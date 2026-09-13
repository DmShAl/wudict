// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wuweidict/wudict/internal/config"
	"github.com/wuweidict/wudict/internal/intake"
	"github.com/wuweidict/wudict/internal/logx"
)

// Importing an archive over HTTP. internal/intake does the work; this file is
// the three requests a page makes of it, and one answer shape it polls.
//
// The split matters on Android, where this endpoint is the ONLY way a
// dictionary can arrive: the shell hands the server a path or a stream and has
// no unarchiver, no knowledge of formats, and no business acquiring one. The
// desktop page reaches the same three routes with a dropped file, so there is
// one import path on every platform rather than a shell feature the desktop
// build quietly lacks.
//
// None of these may answer cross-origin (D69): every one of them writes to the
// user's library, and the first names a path on their disk.

// maxIntakeUpload bounds a streamed archive. It is not a memory bound - the
// body is spooled to disk as it arrives and never held - but a bound on what a
// dropped connection can leave behind before the sweep notices, and a number
// that makes a mis-aimed upload fail while there is still room on the device.
// Dictionary bundles of several gigabytes are real, so it is generous.
const maxIntakeUpload = 16 << 30

// importer returns the manager, wiring its one hook on first use. Not done in
// New because a Server built directly in a test is a supported shape, and a
// nil hook would be the difference between the two.
func (s *Server) importer() *intake.Manager {
	s.intakeOnce.Do(func() {
		s.intake.Installed = func() {
			if err := s.reg.Rescan(); err != nil {
				logx.Warn("rescan after import: %v", err)
				return
			}
			// Pre-open what just arrived, so the first search after an import
			// is not the one that pays for opening it.
			s.reg.Warm()
		}
	})
	return &s.intake
}

// intakeStatus is the job plus the two things the page cannot infer from it:
// where an import would land, and whether it has to ASK about the source file
// or the answer is already settled (D102 - the setting is shown as a question
// only when it is one).
type intakeStatus struct {
	intake.Job
	Dir  string `json:"dir,omitempty"`
	Keep string `json:"keep"` // ask | keep | delete
}

func (s *Server) intakeStatus(j intake.Job) intakeStatus {
	return intakeStatus{Job: j, Dir: s.importDir(), Keep: s.keepPolicy()}
}

// importDir is where dictionaries are installed: the first configured folder,
// which is the one the setup page presents as the dictionary folder. Empty
// when none is configured, which is the state a first-run server is in and the
// reason Confirm can fail with nothing to refuse.
func (s *Server) importDir() string {
	if dirs := s.reg.Dirs(); len(dirs) > 0 {
		return dirs[0]
	}
	return ""
}

func (s *Server) keepPolicy() string {
	switch s.ImportKeep {
	case config.ImportKeepYes, config.ImportKeepDelete:
		return s.ImportKeep
	}
	return config.ImportKeepAsk
}

// keepSource resolves what becomes of the user's own file. A configured
// answer wins over the request: the setting exists so the question stops being
// asked, and a page that kept asking would make it a suggestion.
func (s *Server) keepSource(want *bool) bool {
	switch s.keepPolicy() {
	case config.ImportKeepYes:
		return true
	case config.ImportKeepDelete:
		return false
	}
	if want != nil {
		return *want
	}
	// Unanswered under "ask" keeps it. The two mistakes are not symmetric:
	// a kept archive is clutter, a deleted one may be the only copy.
	return true
}

// handleIntake starts a job or confirms one. Which of the two is not a guess:
// ?confirm is the second act and carries the user's choices, everything else
// is an acquisition.
func (s *Server) handleIntake(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("confirm") {
		s.intakeConfirm(w, r)
		return
	}
	dest := s.importDir()
	if dest == "" {
		intakeErr(w, intake.ErrNoDestination)
		return
	}

	// A URL is fetched by this server rather than by the caller, because the
	// caller is often a phone whose page dies on rotation and whose shell has
	// no networking of its own (D130). It answers at once and the download
	// runs behind the same poll the extraction uses.
	if u := strings.TrimSpace(q.Get("url")); u != "" {
		j, err := s.importer().BeginURL(dest, u, s.fetcher())
		if err != nil {
			intakeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, s.intakeStatus(j))
		return
	}

	// A path is a file the caller already has on disk and wants read in
	// place - the Android shell handing over a download, or a desktop page
	// naming a file. Nothing is copied, so a 2 GB archive costs nothing to
	// begin.
	if p := strings.TrimSpace(q.Get("path")); p != "" {
		src, err := localSource(p)
		if err != nil {
			intakeErr(w, err)
			return
		}
		s.intakeBegin(w, src)
		return
	}

	// Otherwise the body IS the archive. Spooled into the staging area under
	// the destination - not TMPDIR, which on Android is a small cache
	// partition and on every platform is a different filesystem, making the
	// eventual install a second full copy.
	name := filepath.Base(strings.TrimSpace(q.Get("name")))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "archive.zip"
	}
	src, err := intake.Spool(dest, name, http.MaxBytesReader(w, r.Body, maxIntakeUpload))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpErr(w, http.StatusRequestEntityTooLarge, "that file is too large to import")
			return
		}
		httpErr(w, http.StatusInternalServerError, "could not receive the file: %v", err)
		return
	}
	s.intakeBegin(w, src)
}

// localSource describes a file named by path, refusing anything that is not a
// plain readable file. A directory or a device node reaching an archive reader
// is not an import somebody meant.
func localSource(p string) (intake.Source, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return intake.Source{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return intake.Source{}, errors.New("there is no file at that path")
	}
	if !fi.Mode().IsRegular() {
		return intake.Source{}, errors.New("that is not a file")
	}
	return intake.Source{Path: abs, Name: filepath.Base(abs)}, nil
}

func (s *Server) intakeBegin(w http.ResponseWriter, src intake.Source) {
	// The destination is passed at Begin as well as at Confirm because the
	// sniff result says which candidates the library already holds, and that
	// belongs on the screen where the user chooses (D134). An unconfigured
	// server passes "" and simply marks nothing.
	j, err := s.importer().Begin(s.importDir(), src)
	if err != nil {
		intakeErr(w, err)
		return
	}
	writeJSON(w, s.intakeStatus(j))
}

// intakeConfirmReq is what the user chose: which dictionaries, and whether
// their archive survives. Indexes rather than names, because two dictionaries
// in one archive may be named the same in different folders.
type intakeConfirmReq struct {
	Pick []int `json:"pick"`
	Keep *bool `json:"keep"`
	// Copy installs beside a dictionary the library already holds instead of
	// updating it. Absent means the default, which is to update.
	Copy bool `json:"copy"`
	// Extras are indexes into the job's extras: companion files found beside a
	// downloaded one, which are fetched only when the user says so.
	Extras []int `json:"extras"`
}

func (s *Server) intakeConfirm(w http.ResponseWriter, r *http.Request) {
	var req intakeConfirmReq
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			httpErr(w, http.StatusBadRequest, "bad request: %v", err)
			return
		}
	}
	if len(req.Pick) == 0 {
		// The query form, for a caller with no JSON to hand: ?pick=0,2
		req.Pick = parsePick(r.URL.Query().Get("pick"))
	}
	if req.Keep == nil {
		if v := r.URL.Query().Get("keep"); v != "" {
			b := v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "no")
			req.Keep = &b
		}
	}
	if !req.Copy {
		v := r.URL.Query().Get("copy")
		req.Copy = v != "" && v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "no")
	}
	dest := s.importDir()
	if len(req.Extras) == 0 {
		req.Extras = parsePick(r.URL.Query().Get("extras"))
	}
	j, err := s.importer().Confirm(dest, req.Pick, intake.Options{
		Keep:   s.keepSource(req.Keep),
		Copy:   req.Copy,
		Extras: req.Extras,
	})
	if err != nil {
		intakeErr(w, err)
		return
	}
	// Accepted, not OK: the extraction is running and outlives this request,
	// and the page learns how it went by polling - the same shape as a lemma
	// install, for the same reason (the Android page dies on rotation).
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.intakeStatus(j))
}

func parsePick(v string) []int {
	var out []int
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		n := 0
		ok := true
		for _, c := range f {
			if c < '0' || c > '9' {
				ok = false
				break
			}
			n = n*10 + int(c-'0')
			if n > 1<<20 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, n)
		}
	}
	return out
}

// fetcher is the download policy this server was configured with.
func (s *Server) fetcher() intake.Fetcher {
	return intake.Fetcher{Hosts: s.ImportURLHosts, Insecure: s.ImportInsecure}
}

// handleIntakeStatus is the poll. It never fails: "nothing is happening" is an
// answer the page acts on, and it must not have to tell that apart from a
// request that did not arrive.
func (s *Server) handleIntakeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.intakeStatus(s.importer().Status()))
}

// handleIntakeCancel abandons whatever is in flight. Also how the page says
// "not these after all" to a sniffed archive it never confirmed.
func (s *Server) handleIntakeCancel(w http.ResponseWriter, r *http.Request) {
	s.importer().Cancel()
	writeJSON(w, s.intakeStatus(intake.Job{}))
}

// intakeErr maps the package's sentinels onto status codes. Each is a
// different thing for a page to do - retry later, reload, tell the user the
// file is not one we read - and a single 400 would collapse them into one.
func intakeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, intake.ErrBusy):
		code = http.StatusConflict
	case errors.Is(err, intake.ErrNoJob):
		code = http.StatusConflict
	case errors.Is(err, intake.ErrNoDestination):
		code = http.StatusConflict
	case errors.Is(err, intake.ErrNotInstallable):
		code = http.StatusConflict
	case errors.Is(err, intake.ErrHostNotAllowed), errors.Is(err, intake.ErrInsecureURL):
		// Forbidden rather than bad request: the link is well formed and the
		// refusal is this server's policy, which is a setting the user owns
		// and the difference the page has to explain.
		code = http.StatusForbidden
	case errors.Is(err, intake.ErrUnsupported), errors.Is(err, intake.ErrNotArchive):
		code = http.StatusUnsupportedMediaType
	case errors.Is(err, intake.ErrNothingFound), errors.Is(err, intake.ErrTooManyEntries):
		code = http.StatusUnprocessableEntity
	}
	httpErr(w, code, "%s", err)
}
