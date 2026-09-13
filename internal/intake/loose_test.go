// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// looseSite serves a folder of loose dictionary files at /dicts, with real
// bodies so a download produces a file. It is the shape of the sites these
// formats come from: one dictionary, several links.
func looseSite(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/dicts/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, filepath.Base(r.URL.Path), zeroTime, strings.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The bug that started this: a link to a bare .mdx was refused as "not an
// archive". It is a dictionary, and it arrives with the companion the site
// published beside it - offered, and installed because the user said so.
func TestURLImportOfALooseFile(t *testing.T) {
	srv := looseSite(t, map[string]string{
		"Oxford.mdx": "main file",
		"Oxford.mdd": "media file",
	})
	dest := t.TempDir()
	m := &Manager{}
	if _, err := m.BeginURL(dest, srv.URL+"/dicts/Oxford.mdx", loopback()); err != nil {
		t.Fatalf("BeginURL: %v", err)
	}
	m.Wait()

	st := m.Status()
	if st.State != StateReady {
		t.Fatalf("state = %q (%s)", st.State, st.Error)
	}
	if len(st.Candidates) != 1 || st.Candidates[0].Name != "Oxford" || !st.Candidates[0].Complete() {
		t.Fatalf("candidates = %+v", st.Candidates)
	}
	if len(st.Extras) != 1 || st.Extras[0].Name != "Oxford.mdd" || st.Extras[0].Need != NeedMedia {
		t.Fatalf("extras = %+v, want the .mdd beside it", st.Extras)
	}

	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true, Extras: []int{0}}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	m.Wait()
	if st := m.Status(); st.State != StateDone {
		t.Fatalf("state = %q (%s)", st.State, st.Error)
	}
	for _, want := range []string{"Oxford.mdx", "Oxford.mdd"} {
		if _, err := os.Stat(filepath.Join(dest, "Oxford", want)); err != nil {
			t.Errorf("%s not installed: %v", want, err)
		}
	}
}

// Offered is not taken. A companion the user left unticked is not downloaded,
// which is the whole reason the probe reports instead of fetching.
func TestExtrasAreOnlyFetchedWhenTicked(t *testing.T) {
	srv := looseSite(t, map[string]string{
		"Oxford.mdx": "main file",
		"Oxford.mdd": "media file",
	})
	dest := t.TempDir()
	m := &Manager{}
	if _, err := m.BeginURL(dest, srv.URL+"/dicts/Oxford.mdx", loopback()); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	m.Wait()
	if st := m.Status(); st.State != StateDone {
		t.Fatalf("state = %q (%s)", st.State, st.Error)
	}
	if _, err := os.Stat(filepath.Join(dest, "Oxford", "Oxford.mdx")); err != nil {
		t.Fatalf("not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(DownloadDir(dest), "Oxford.mdd")); err == nil {
		t.Error("a companion nobody ticked was downloaded anyway")
	}
	if _, err := os.Stat(filepath.Join(dest, "Oxford", "Oxford.mdd")); err == nil {
		t.Error("a companion nobody ticked was installed anyway")
	}
}

// A StarDict .ifo alone is incomplete, and stays refused - until the index it
// names is on the list of things about to be downloaded. The completeness
// check then runs again on what actually landed, which is what keeps the
// permission from becoming a way to install a broken dictionary.
func TestIncompleteCandidateNeedsItsRequiredExtras(t *testing.T) {
	srv := looseSite(t, map[string]string{
		"abc.ifo":  "StarDict's dict ifo file\n",
		"abc.idx":  "index",
		"abc.dict": "articles",
	})
	dest := t.TempDir()
	m := &Manager{}
	if _, err := m.BeginURL(dest, srv.URL+"/dicts/abc.ifo", loopback()); err != nil {
		t.Fatal(err)
	}
	m.Wait()

	st := m.Status()
	if len(st.Candidates) != 1 || st.Candidates[0].Complete() {
		t.Fatalf("a lone .ifo must arrive incomplete: %+v", st.Candidates)
	}
	want := map[string]bool{"abc.idx": true, "abc.dict": true}
	pick := []int{}
	for i, e := range st.Extras {
		if want[e.Name] {
			if e.Need != NeedRequired {
				t.Errorf("%s: need = %q, want %q", e.Name, e.Need, NeedRequired)
			}
			pick = append(pick, i)
		}
	}
	if len(pick) != 2 {
		t.Fatalf("extras = %+v, want the index and the articles", st.Extras)
	}

	// Without them it is refused, and the refusal is the honest one.
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); !errors.Is(err, ErrNotInstallable) {
		t.Fatalf("err = %v, want ErrNotInstallable", err)
	}

	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true, Extras: pick}); err != nil {
		t.Fatalf("Confirm with the required extras: %v", err)
	}
	m.Wait()
	if st := m.Status(); st.State != StateDone {
		t.Fatalf("state = %q (%s)", st.State, st.Error)
	}
	for _, f := range []string{"abc.ifo", "abc.idx", "abc.dict"} {
		if _, err := os.Stat(filepath.Join(dest, "abc", f)); err != nil {
			t.Errorf("%s not installed: %v", f, err)
		}
	}
}

// An index into a list nobody sent is not a companion.
func TestConfirmRefusesAnUnknownExtra(t *testing.T) {
	dest := t.TempDir()
	m := &Manager{}
	src := buildZip(t, member{"Oxford.mdx", "main"})
	mustBegin(t, m, dest, src, false)
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true, Extras: []int{0}}); err == nil {
		t.Fatal("an out-of-range companion index was accepted")
	}
	// And the job survives the refusal: nothing was started, so it is still
	// the sniffed job waiting for a real answer.
	if st := m.Status(); st.State != StateReady {
		t.Fatalf("state = %q, want the job still ready", st.State)
	}
}

// A loose file the user pointed at on disk, with "delete the source
// afterwards": what is deleted is what was consumed. Leaving the .mdd behind
// after taking the .mdx would be removing half of what was imported.
func TestLoosePathImportDisposesEveryFileItTook(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"Oxford.mdx":  "main",
		"Oxford.mdd":  "media",
		"Collins.mdx": "a different dictionary",
	})
	dest := t.TempDir()
	m := &Manager{}
	mustBegin(t, m, dest, filepath.Join(dir, "Oxford.mdx"), false)
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: false}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	m.Wait()
	if st := m.Status(); st.State != StateDone {
		t.Fatalf("state = %q (%s)", st.State, st.Error)
	}
	for _, gone := range []string{"Oxford.mdx", "Oxford.mdd"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s was consumed but not disposed of", gone)
		}
	}
	// And only those: the other dictionary in the folder was never part of
	// this import.
	if _, err := os.Stat(filepath.Join(dir, "Collins.mdx")); err != nil {
		t.Errorf("an unrelated file was deleted: %v", err)
	}
}

// Keeping the source keeps all of it, for the same reason.
func TestLoosePathImportKeepsEveryFileItTook(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"Oxford.mdx": "main",
		"Oxford.mdd": "media",
	})
	dest := t.TempDir()
	m := &Manager{}
	mustBegin(t, m, dest, filepath.Join(dir, "Oxford.mdx"), false)
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	for _, kept := range []string{"Oxford.mdx", "Oxford.mdd"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s: %v", kept, err)
		}
	}
}

// A spooled upload keeps the name it was given, because for a loose file the
// name IS the dictionary's name: a temp name would install a dictionary called
// "incoming-3141592". The per-upload directory is also what keeps one upload's
// sibling scan out of another's.
func TestSpoolKeepsTheFileName(t *testing.T) {
	dest := t.TempDir()
	src, err := Spool(dest, "Oxford.mdx", strings.NewReader("main"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(src.Path) != "Oxford.mdx" {
		t.Fatalf("spooled as %q, want the name it was given", filepath.Base(src.Path))
	}
	if !src.Temp {
		t.Error("a spooled upload is ours to remove")
	}
	if !strings.HasPrefix(filepath.Base(filepath.Dir(src.Path)), spoolPrefix) {
		t.Fatalf("spooled into %q, want a per-upload directory", filepath.Dir(src.Path))
	}
	// Two uploads of the same name do not collide, and neither sees the other.
	second, err := Spool(dest, "Oxford.mdx", strings.NewReader("main again"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(second.Path) == filepath.Dir(src.Path) {
		t.Fatal("two uploads shared a directory")
	}

	// And disposing of one takes its directory with it.
	removeSource(src)
	if _, err := os.Stat(filepath.Dir(src.Path)); err == nil {
		t.Error("the spool directory outlived the upload")
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Errorf("the other upload was removed too: %v", err)
	}
}

// An uploaded loose file installs under its own name, end to end.
func TestSpooledLooseUploadInstalls(t *testing.T) {
	dest := t.TempDir()
	src, err := Spool(dest, "Oxford.mdx", strings.NewReader("main"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	j, err := m.Begin(dest, src)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if len(j.Candidates) != 1 || j.Candidates[0].Name != "Oxford" {
		t.Fatalf("candidates = %+v", j.Candidates)
	}
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if st := m.Status(); st.State != StateDone {
		t.Fatalf("state = %q (%s)", st.State, st.Error)
	}
	if _, err := os.Stat(filepath.Join(dest, "Oxford", "Oxford.mdx")); err != nil {
		t.Fatalf("not installed: %v", err)
	}
	// Temp, so it goes whatever the keep setting says: it was never the
	// user's file, it was a copy this server made.
	if _, err := os.Stat(filepath.Dir(src.Path)); err == nil {
		t.Error("the spool directory was left behind")
	}
}

// The name is not enough on its own. These formats have no registered MIME
// type and arrive as octet-stream, so the name is what identifies them - but a
// server that declares HTML is serving the login page that link redirected to,
// wearing the file's name, and no amount of ".mdx" makes that a dictionary.
func TestMarkupIsRefusedEvenUnderADictionaryName(t *testing.T) {
	for _, ct := range []string{"text/html", "text/html; charset=utf-8", "application/xhtml+xml"} {
		t.Run(ct, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", ct)
				_, _ = w.Write([]byte("<html>sign in to download</html>"))
			}))
			defer srv.Close()
			m := &Manager{}
			if _, err := m.BeginURL(t.TempDir(), srv.URL+"/dicts/Oxford.mdx", loopback()); err != nil {
				t.Fatal(err)
			}
			m.Wait()
			if st := m.Status(); st.State != StateError {
				t.Fatalf("state = %q, want the job to have failed", st.State)
			}
		})
	}
}

// text/plain is deliberately NOT refused: a DSL dictionary IS text, and the
// servers that send it as text/plain are sending the file.
func TestTextPlainIsADictionaryWhenItIsNamedLikeOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-16")
		_, _ = w.Write([]byte("#NAME\t\"Oxford\"\n"))
	}))
	defer srv.Close()

	dest := t.TempDir()
	m := &Manager{}
	if _, err := m.BeginURL(dest, srv.URL+"/dicts/Oxford.dsl", loopback()); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	st := m.Status()
	if st.State != StateReady {
		t.Fatalf("state = %q (%s), want a DSL dictionary to be readable", st.State, st.Error)
	}
	if len(st.Candidates) != 1 || st.Candidates[0].Name != "Oxford" {
		t.Fatalf("candidates = %+v", st.Candidates)
	}
}
