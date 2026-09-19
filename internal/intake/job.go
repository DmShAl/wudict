// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/wuweidict/wudict/internal/dict"
)

// One import at a time, everywhere, and the reasons are not only about RAM.
//
// Extraction is the one operation here that writes to the library, and two of
// them running at once would be two goroutines choosing destination folder
// names from the same directory listing - the check and the create cannot be
// one act across processes, so the way to make them one is to have a single
// writer. It also bounds the cost: one open decompressor, one megabyte of
// buffer, one directory tree in flight, on a phone as on a desktop.
//
// The state machine is small and every transition is a user act or its
// completion:
//
//	(none) --Begin----> ready --Confirm--> installing --> done
//	   `---BeginURL--> downloading --^                  `-> error
//
// Cancel returns to (none) from any of them. There is no "sniffing" state: a
// zip's central directory is a few kilobytes of reads, so Begin answers within
// its request and a state nobody can observe would only be a state to test.
// Downloading IS a state, for the opposite reason: it is minutes of work on a
// phone connection, it has a progress number somebody watches, and it cannot
// be held inside the request that asked for it.
const (
	StateDownloading = "downloading" // fetching the archive named by a URL
	StateReady       = "ready"       // sniffed; waiting for the user to choose
	StateInstalling  = "installing"  // extracting the chosen candidates
	StateDone        = "done"        // finished; the list of what was installed
	StateError       = "error"
)

var (
	// ErrBusy is a second import while one is extracting. Deliberately not a
	// queue: the caller is a person watching a progress line, and the honest
	// answer to "can you do this now" is no.
	ErrBusy = errors.New("an import is already running")
	// ErrNoJob is Confirm or Cancel with nothing to confirm or cancel - a page
	// left open across a restart, or two tabs racing one job.
	ErrNoJob = errors.New("no import is waiting")
	// ErrNothingFound is a readable archive holding no dictionary. Not an
	// error about the file: it is the answer.
	ErrNothingFound = errors.New("no dictionaries found in that file")
	// ErrUnsupported is an archive format this build cannot read.
	ErrUnsupported = errors.New("unsupported archive format")
	// ErrNoDestination is an import with no dictionary folder to install into,
	// which is the state a first-run server is in.
	ErrNoDestination = errors.New("no dictionary folder is configured")
	// ErrNotInstallable is a confirmation naming a candidate that was reported
	// incomplete. Refused here as well as at extraction, because by then the
	// staging directory exists.
	ErrNotInstallable = errors.New("that dictionary is missing files it cannot work without")
)

// Options are the choices the user makes on the confirm screen, as a struct
// rather than a row of adjacent bools: Confirm(dest, pick, true, false) is a
// call nobody can read, and both of these are answers to questions about what
// happens to somebody's files.
type Options struct {
	// Keep says the user's own source file survives a SUCCESSFUL import. It is
	// ignored on failure, where the source is ALWAYS kept: deleting somebody's
	// only copy after an import that did not work is the one mistake here that
	// cannot be undone, and it is policy rather than a preference (D129).
	Keep bool
	// Copy installs beside a dictionary that is already in the library instead
	// of replacing it, under the numbered name uniqueDir picks. It is the
	// explicit opt-out from the default, which is that importing a dictionary
	// the library already holds UPDATES it (D134) - because the common reason
	// to import the same bundle twice is that it was reissued, and the common
	// outcome before this existed was two of everything in every result list.
	Copy bool
	// Extras are indexes into Job.Extras: the companion files that were found
	// beside a downloaded one and that the user agreed to fetch. Nothing is
	// downloaded on the strength of having been found (see probe.go), so an
	// empty list means the dictionary is installed with what is already here.
	Extras []int
}

// Source is where one job's bytes came from.
type Source struct {
	// Path is the archive on disk. Always a real file: the archive readers
	// seek, and a zip is read from its tail.
	Path string
	// Name is what the user sees. The base name of Path for a file they
	// pointed at, the download's name for one we fetched.
	Name string
	// Temp marks a copy this package made (Spool, and later a download). It is
	// removed when the job ends regardless of the keep choice, which applies
	// only to a file the user owns.
	Temp bool
}

// Job is the whole of what a caller can observe, and is always a copy: the
// installer writes Done from its own goroutine.
type Job struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// Source is what the bytes ARE, as the user would name them: the file.
	// While a download is still resolving its name it is empty rather than
	// wrong, and a page says "the file" for itself.
	Source string `json:"source"`
	// Host is where a downloaded file is coming from, for the life of the job.
	// The host and not the link: a URL can carry a session token in its query,
	// and that is not a line to put on somebody's screen (D102) - while "which
	// site is this arriving from" is exactly what a person watching a phone
	// download on their own data wants to know. Empty for a local import.
	Host string `json:"host,omitempty"`
	// Candidates is what the archive turned out to hold, in the order the
	// user is shown them; the index into this slice is what Confirm takes.
	Candidates []Candidate `json:"candidates,omitempty"`
	// Installed names the folders now in the library, as the user will see
	// them in a listing - which is not always the candidate's name, since a
	// second import of the same bundle is numbered rather than merged.
	// Extras are companion files that exist beside the downloaded one and
	// have NOT been downloaded: the user is asked first, on the same screen
	// that asks which dictionaries to install.
	Extras    []Extra  `json:"extras,omitempty"`
	Installed []string `json:"installed,omitempty"`
	Error     string   `json:"error,omitempty"`
	// Done and Total are bytes, and are not omitempty: a job that has just
	// started has done 0, and an absent field leaves a page dividing by
	// undefined.
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

// Manager owns the single job. The zero value is usable.
type Manager struct {
	mu  sync.Mutex
	cur *jobState
	// wg counts install goroutines. The server never waits on one - an import
	// is meant to outlive the request that confirmed it - but a test must, or
	// it removes the t.TempDir() out from under a running extraction.
	wg sync.WaitGroup

	// Installed, when set, is called after a job installs at least one
	// dictionary. It is how the library learns to look again; this package
	// deliberately knows nothing about registries.
	Installed func()
}

// jobState is the mutable job: the public copy plus what only the installer
// needs. Fields are read and written under Manager.mu.
type jobState struct {
	pub    Job
	dest   string
	stage  string
	src    Source
	keep   bool
	cancel context.CancelFunc

	// fetch and extras are the download side's half of a confirmation: the
	// policy to fetch a companion under, and the links found for the ones the
	// user is being asked about. Both are empty for an import from a file.
	fetch  Fetcher
	extras []Extra
}

// Begin reads the archive and reports what is in it, without extracting
// anything or writing a byte. Synchronous on purpose: it is a directory read,
// and a caller that has to poll for the contents of a file it just named would
// be poll machinery invented for a millisecond.
//
// It replaces a finished job - that is how a user imports a second archive -
// and refuses to interrupt a running one.
// dest is the dictionary folder the import would land in, and is needed here
// and not only at Confirm because the answer to "is this one already
// installed" belongs on the screen where the user chooses (D134). It may be
// empty - a first-run server has no folder yet - and then no candidate is
// marked and Confirm is what refuses.
func (m *Manager) Begin(dest string, src Source) (Job, error) {
	cands, err := sniffFile(dest, src.Path)
	if err != nil {
		m.discardSource(src)
		return Job{}, err
	}

	m.mu.Lock()
	if m.busyLocked() {
		m.mu.Unlock()
		m.discardSource(src)
		return Job{}, ErrBusy
	}
	defer m.mu.Unlock()
	m.clearLocked()
	name := src.Name
	if name == "" {
		name = filepath.Base(src.Path)
	}
	j := &jobState{
		src: src,
		pub: Job{ID: jobID(), State: StateReady, Source: name, Candidates: cands},
	}
	m.cur = j
	return j.pub.copy(), nil
}

// BeginURL fetches an archive from a URL and sniffs it, and unlike Begin it
// answers before the work is done. It has to: the file is the size of a
// dictionary collection and the link is being followed over a phone
// connection, so the alternative is a request held open for ten minutes
// against every timeout between here and the caller.
//
// The URL is validated before the job is claimed, so a link that was never
// going to be fetched gets a refusal rather than a job to poll and cancel.
func (m *Manager) BeginURL(dest, raw string, f Fetcher) (Job, error) {
	if dest == "" {
		return Job{}, ErrNoDestination
	}
	u, err := f.Check(raw)
	if err != nil {
		return Job{}, err
	}

	m.mu.Lock()
	if m.busyLocked() {
		m.mu.Unlock()
		return Job{}, ErrBusy
	}
	m.clearLocked()
	ctx, cancel := context.WithCancel(context.Background())
	j := &jobState{
		dest:   dest,
		fetch:  f,
		cancel: cancel,
		// The name of the file is not known until the server answers, so the
		// job starts with only the host and fills Source in from the first
		// progress report (see Progress).
		pub: Job{ID: jobID(), State: StateDownloading, Host: u.Hostname()},
	}
	m.cur = j
	// Snapshot under the lock: the worker below starts writing pub fields
	// (Source arrives with the first progress report) as soon as it runs, and
	// the copy would otherwise race it.
	pub := j.pub.copy()
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		m.download(ctx, j, f, raw)
	}()
	return pub, nil
}

// download is the fetching side, on its own goroutine: acquire, then sniff,
// then hand the job to the user exactly as Begin would have.
//
// A failure leaves the partial file where it is. That is the whole point of
// the .part convention - the next attempt continues from it - and it is also
// why nothing here disposes of anything: the download is not an intermediary,
// it is the file the user asked for.
func (m *Manager) download(ctx context.Context, j *jobState, f Fetcher, raw string) {
	src, err := f.Fetch(ctx, j.dest, raw, func(name string, done, total int64) {
		m.mu.Lock()
		j.pub.Source, j.pub.Done, j.pub.Total = name, done, total
		m.mu.Unlock()
	})
	var cands []Candidate
	var extras []Extra
	if err == nil {
		cands, err = sniffFile(j.dest, src.Path)
	}
	if err == nil {
		// Only now, with the main file in hand: the probe is derived from ITS
		// name, and asking before it landed would be asking about a file whose
		// name the server had not yet confirmed.
		if u, cerr := f.Check(raw); cerr == nil {
			extras = probeCompanions(ctx, f, u, filepath.Base(src.Path), filepath.Dir(src.Path))
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur != j {
		return // cancelled, or replaced by another import while this ran
	}
	if err != nil {
		j.pub.State, j.pub.Error = StateError, errorText(err)
		return
	}
	j.src, j.extras = src, extras
	j.pub.State, j.pub.Source, j.pub.Candidates = StateReady, src.Name, cands
	j.pub.Extras = append([]Extra(nil), extras...)
	j.pub.Done, j.pub.Total = 0, 0 // the next number this reports is extraction
}

// sniffFile lists what an archive holds without writing anything, and gives
// "readable, but holds no dictionary" its own answer rather than letting it
// arrive as an empty success.
func sniffFile(dest, path string) ([]Candidate, error) {
	a, err := openArchive(path)
	if err != nil {
		return nil, err
	}
	cands, err := Sniff(a)
	if cerr := a.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, ErrNothingFound
	}
	markExisting(dest, cands)
	return cands, nil
}

// busyLocked reports whether the current job is doing work that a new one
// would interrupt. A ready or finished job is not busy - replacing it is how a
// user imports a second archive.
func (m *Manager) busyLocked() bool {
	return m.cur != nil &&
		(m.cur.pub.State == StateInstalling || m.cur.pub.State == StateDownloading)
}

// Confirm starts extracting the candidates named by pick - indexes into the
// job's Candidates - into dest. It returns as soon as the work is running: an
// extraction outlives its request for the same reason a lemma download does,
// because on Android the page dies whenever the device rotates or the app is
// backgrounded, and an import that died with it would leave half a dictionary
// and no way to learn that it had.
//
// opts carries the two choices the screen offers; see Options.
func (m *Manager) Confirm(dest string, pick []int, opts Options) (Job, error) {
	if dest == "" {
		return Job{}, ErrNoDestination
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.cur
	switch {
	case j == nil:
		return Job{}, ErrNoJob
	case j.pub.State == StateInstalling, j.pub.State == StateDownloading:
		return Job{}, ErrBusy
	case j.pub.State != StateReady:
		return Job{}, ErrNoJob
	}

	extras, fetching, err := pickExtras(j.extras, opts.Extras)
	if err != nil {
		return Job{}, err
	}

	chosen := make([]Candidate, 0, len(pick))
	var total int64
	for _, i := range pick {
		if i < 0 || i >= len(j.pub.Candidates) {
			return Job{}, errors.New("no such dictionary in this archive")
		}
		c := j.pub.Candidates[i]
		if !c.Complete() && !fetching {
			// With a required companion on its way, "incomplete" is a
			// statement about the files that have arrived SO FAR. The check
			// runs again after they land, where it is finally true or not.
			return Job{}, ErrNotInstallable
		}
		chosen = append(chosen, c)
		total += c.Size
	}
	if len(chosen) == 0 {
		return Job{}, errors.New("nothing was selected")
	}

	stage, err := Stage(dest)
	if err != nil {
		return Job{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.dest, j.stage, j.keep, j.cancel = dest, stage, opts.Keep, cancel
	j.pub.State, j.pub.Total, j.pub.Done = StateInstalling, total, 0
	if len(extras) > 0 {
		// The download comes first and is what the user watches; the install
		// total replaces this one when the files are here.
		j.pub.State, j.pub.Total = StateDownloading, extrasTotal(extras)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		m.run(ctx, j, chosen, opts, extras)
	}()
	// Still under m.mu (it unlocks via defer, after the return is evaluated),
	// and the worker writes pub only through m.mu - so this snapshot cannot
	// race, unlike BeginURL's.
	return j.pub.copy(), nil
}

// pickExtras resolves the indexes the user ticked against the companions this
// job actually found, and reports whether any of them is one the dictionary
// cannot work without - which is what makes confirming an incomplete candidate
// a reasonable thing to allow.
func pickExtras(found []Extra, pick []int) ([]Extra, bool, error) {
	out := make([]Extra, 0, len(pick))
	required := false
	seen := map[int]bool{}
	for _, i := range pick {
		if i < 0 || i >= len(found) || seen[i] {
			return nil, false, errors.New("no such companion file")
		}
		seen[i] = true
		out = append(out, found[i])
		required = required || found[i].Need == NeedRequired
	}
	return out, required, nil
}

func extrasTotal(extras []Extra) int64 {
	var n int64
	for _, e := range extras {
		n += e.Size
	}
	return n
}

// run is the confirmed job end to end: fetch what the user agreed to fetch,
// then install. Split from install because the two halves report different
// numbers - bytes off the network, then bytes onto the disk - and a single
// progress line that meant either would mean neither.
func (m *Manager) run(ctx context.Context, j *jobState, chosen []Candidate, opts Options, extras []Extra) {
	if len(extras) > 0 {
		chosen = m.fetchExtras(ctx, j, chosen, extras)
	}
	m.install(ctx, j, chosen, opts)
}

// fetchExtras downloads the companions the user ticked and re-reads the
// dictionary they belong to, so the install works from what is on disk NOW
// rather than from the list the user was shown before the files existed.
//
// A companion that fails to download is not an error: it is a companion the
// dictionary will be installed without, which is exactly the state it was in a
// moment ago. What decides the outcome is the re-sniff - a StarDict set whose
// index never arrived is still incomplete, and extraction refuses it with the
// same message it would have given before.
func (m *Manager) fetchExtras(ctx context.Context, j *jobState, chosen []Candidate, extras []Extra) []Candidate {
	var base int64
	total := extrasTotal(extras)
	for _, e := range extras {
		if ctx.Err() != nil {
			break
		}
		// The name BEFORE the request, because the probe already learned it:
		// a companion can be several gigabytes and its server can take a while
		// to answer, and for that whole window a line built from the last
		// report would name the file that finished rather than the one being
		// waited on. The callback below replaces it with whatever the server
		// turns out to call the file.
		m.mu.Lock()
		j.pub.Source = e.Name
		m.mu.Unlock()
		src, err := j.fetch.Fetch(ctx, j.dest, e.url, func(name string, done, _ int64) {
			m.mu.Lock()
			// The companion's own name, so the line says which of several
			// files is arriving rather than repeating the dictionary's.
			j.pub.Source, j.pub.Done = name, base+done
			m.mu.Unlock()
		})
		if err == nil {
			base += fileSize(src.Path)
		} else {
			base += e.Size
		}
		m.mu.Lock()
		j.pub.Done, j.pub.Total = base, max(total, base)
		m.mu.Unlock()
	}

	fresh, err := sniffFile(j.dest, j.src.Path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		j.pub.Candidates = fresh
		chosen = remap(chosen, fresh)
	}
	var total2 int64
	for _, c := range chosen {
		total2 += c.Size
	}
	j.pub.State, j.pub.Done, j.pub.Total = StateInstalling, 0, total2
	return chosen
}

// remap replaces each chosen candidate with the re-read one of the same name.
// By name and not by index: the sniff ran again over a directory that has
// gained files, and an index into the old list is a promise about an order
// nothing guarantees. A name with no match keeps the old candidate, which then
// fails at extraction the way it would have anyway.
func remap(chosen, fresh []Candidate) []Candidate {
	by := make(map[string]Candidate, len(fresh))
	for _, c := range fresh {
		by[c.Name] = c
	}
	out := make([]Candidate, 0, len(chosen))
	for _, c := range chosen {
		if f, ok := by[c.Name]; ok {
			out = append(out, f)
			continue
		}
		out = append(out, c)
	}
	return out
}

// fileSize is the size of a file that is known to exist, 0 when it does not -
// progress arithmetic, where a missing file means nothing was added.
func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// install is the whole of the writing side, on one goroutine.
//
// Each candidate is extracted into its own numbered directory under the stage
// and then RENAMED into place, which is why staging is inside the destination:
// the rename is atomic, so the library never contains a dictionary that is
// still arriving, and a failure leaves nothing to tidy but the stage.
func (m *Manager) install(ctx context.Context, j *jobState, chosen []Candidate, opts Options) {
	a, err := openArchive(j.src.Path)
	if err == nil {
		defer a.Close()
	}
	var installed []string
	var consumed []string
	pa, plain := a.(*plainArchive)
	for i, c := range chosen {
		if err != nil {
			break
		}
		sub := filepath.Join(j.stage, strconv.Itoa(i))
		if err = os.MkdirAll(sub, 0o755); err != nil {
			break
		}
		if err = Extract(ctx, a, c, sub, func(n int64) {
			m.mu.Lock()
			j.pub.Done += n
			m.mu.Unlock()
		}); err != nil {
			break
		}
		var final string
		if final, err = placeDict(j.dest, j.stage, i, c, sub, opts.Copy); err != nil {
			break
		}
		installed = append(installed, filepath.Base(final))
		if plain {
			// A loose import takes the .mdd as well as the .mdx, and
			// "delete the source afterwards" that left the .mdd behind would
			// be leaving half of what it took.
			consumed = append(consumed, pa.realPaths(c.Files)...)
		}
	}
	// The stage goes whatever happened, including on cancel: everything still
	// inside it is half a dictionary by definition, since a whole one has
	// already been renamed out.
	_ = os.RemoveAll(j.stage)

	m.mu.Lock()
	j.pub.Installed = installed
	if err != nil {
		j.pub.State, j.pub.Error = StateError, errorText(err)
	} else {
		j.pub.State = StateDone
	}
	src, keep := j.src, j.keep
	m.mu.Unlock()

	// Dispose only after the outcome is recorded, and only ever the source we
	// were given: a spooled or downloaded copy always, a file the user owns
	// only when they said so and only when the import worked.
	if src.Temp || (err == nil && !keep) {
		removeSource(src)
		if !src.Temp {
			for _, p := range consumed {
				if p != src.Path {
					_ = os.Remove(p)
				}
			}
		}
	}
	// The staging ROOT goes last and only if it is empty, which is the order
	// that matters: a spooled upload lives in it, so removing the root before
	// disposing of the source would always find it occupied and leave the
	// user's dictionary folder with a directory in it forever. Non-forced, so
	// a second wudict staging its own import there keeps it.
	_ = os.Remove(StageRoot(j.dest))
	if len(installed) > 0 && m.Installed != nil {
		m.Installed()
	}
}

// placeDict moves one extracted dictionary out of the stage and into the
// library, and is where "the library already has this one" is finally decided.
//
// The question is asked AGAIN here, and not read from the flag the sniffer
// recorded, because minutes can pass between the two: the user was shown a
// screen, an archive was extracted, and in between the folder may have been
// created, removed or replaced by something else entirely. The flag is what
// the user was told; this is what is true at the moment of writing.
//
// Replacing is a swap and not a delete-then-move. The old folder is renamed
// ASIDE into the stage first, so at no point is the library without the
// dictionary: if the second rename fails, the first is undone and the user
// still has what they had. Only once the new folder is in place is the old one
// removed - by the stage teardown, which runs whatever happened.
//
// A directory rename with files open inside it is fine on POSIX and can fail
// on Windows, where the error surfaces as the import's error rather than being
// papered over: a half-swapped library is not something to recover silently.
func placeDict(dest, stage string, i int, c Candidate, sub string, copyBeside bool) (string, error) {
	name := ""
	if !copyBeside {
		name, _, _ = existingDict(dest, c)
	}
	if name == "" {
		final, err := uniqueDir(dest, c.Name)
		if err != nil {
			return "", err
		}
		if err := os.Rename(sub, final); err != nil {
			return "", err
		}
		return final, nil
	}

	final := filepath.Join(dest, name)
	away := filepath.Join(stage, "replaced-"+strconv.Itoa(i))
	if err := os.Rename(final, away); err != nil {
		return "", err
	}
	if err := os.Rename(sub, final); err != nil {
		// Put it back. The restore is best-effort by necessity - there is no
		// third place to stand if it also fails - but the failure that gets
		// reported is the one that caused this, not the one cleaning up.
		_ = os.Rename(away, final)
		return "", err
	}
	return final, nil
}

// Status is the current job, or the zero Job when there is none. Never an
// error: "nothing is happening" is an answer a poller needs to be able to act
// on without distinguishing it from a failure to ask.
func (m *Manager) Status() Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return Job{}
	}
	return m.cur.pub.copy()
}

// Cancel stops whatever is in flight and forgets it. It returns at once rather
// than waiting for the extraction to notice: the goroutine owns its own
// staging directory and removes it on the way out, so there is nothing left
// for a waiting caller to be waiting for.
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearLocked()
}

// clearLocked forgets the current job, stopping it first if it is running.
func (m *Manager) clearLocked() {
	j := m.cur
	m.cur = nil
	if j == nil {
		return
	}
	if j.cancel != nil {
		j.cancel()
		return // the installer disposes of its own stage and source
	}
	if j.src.Temp {
		removeSource(j.src)
	}
}

// Wait blocks until no install goroutine is running. For tests only - the
// server must never wait on one.
func (m *Manager) Wait() { m.wg.Wait() }

// discardSource removes a spooled copy for a job that never started, so a
// malformed upload does not leave its bytes in the staging area until the next
// sweep.
func (m *Manager) discardSource(src Source) {
	if src.Temp {
		removeSource(src)
	}
}

// jobID is an opaque handle, not a counter: it appears in a URL the page
// polls, and a guessable one would let a second page adopt a job it never
// started.
func jobID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "job"
	}
	return hex.EncodeToString(b[:])
}

func (j Job) copy() Job {
	c := j
	c.Candidates = append([]Candidate(nil), j.Candidates...)
	c.Extras = append([]Extra(nil), j.Extras...)
	c.Installed = append([]string(nil), j.Installed...)
	return c
}

// openArchive is the one seam in this package. A test needs an archive whose
// DIRECTORY LIES - a real writer records what it actually wrote, and every
// check in writeEntry exists because the directory is an attacker-chosen
// claim - and the manager opens its own archives by design, since a job
// outlives the request that confirmed it.
var openArchive = OpenArchive

// OpenArchive dispatches on the name. By extension and not by content sniffing
// because the name is what the user pointed at: a ".zip" that is really a 7z
// is a mislabelled file, and opening it anyway would mean this decides what
// the user's files are.
func OpenArchive(path string) (Archive, error) {
	switch archiveKind(path) {
	case "zip":
		return OpenZip(path)
	case "7z":
		return OpenSevenZip(path)
	}
	// Not an archive. A loose dictionary file is still an import - it is how a
	// site that publishes "oxford.mdx" and "oxford.mdd" as two links hands one
	// over - and it enters the same pipeline through a one-directory archive
	// of the file and its siblings (plain.go).
	if canBegin(filepath.Base(path)) {
		return OpenPlain(path)
	}
	return nil, ErrUnsupported
}

// canBegin reports whether a loose NAME can begin an import. A main file
// always can, and so can a companion that names its dictionary - somebody who
// shares "oxford.mdd" has shared the dictionary and means it.
//
// A stylesheet or a script cannot, although both are companions of an .mdx
// (CompanionSuffixes): ".css" and ".js" are the two most common file names on
// the web, and a link to one identifies a dictionary to nobody. They are files
// this package CARRIES, found from the .mdx side, never a link it acts on.
func canBegin(name string) bool {
	switch dict.ClassifyName(name) {
	case dict.KindMain:
		return true
	case dict.KindCompanion:
		return !dict.IsAssetName(name)
	}
	return false
}

// Installable reports whether a NAME is something this package can import:
// an archive it can open, or a loose dictionary file. The downloader asks
// before it takes a body, so the answer has to come from the name alone.
func Installable(name string) bool {
	return SupportedArchive(name) || canBegin(filepath.Base(name))
}

// SupportedArchive reports whether a NAME - not a file, which may not exist
// yet - is an archive this build can open. The downloader asks before it
// streams a body, so the answer has to come from the name alone.
func SupportedArchive(name string) bool { return archiveKind(name) != "" }

// archiveKind is the single table both questions read, so a format added to
// one is never missing from the other.
func archiveKind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip":
		return "zip"
	case ".7z":
		return "7z"
	}
	return ""
}

// errorText is what the user is shown. context.Canceled arrives here whenever
// a cancel lands mid-copy, and "context canceled" is machinery leaking into a
// sentence a person reads (D102).
func errorText(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return err.Error()
}
