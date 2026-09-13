// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mustBegin sniffs an archive into a fresh manager and asserts it is ready.
func mustBegin(t *testing.T, m *Manager, dest, path string, temp bool) Job {
	t.Helper()
	j, err := m.Begin(dest, Source{Path: path, Temp: temp})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if j.State != StateReady {
		t.Fatalf("state = %q, want %q", j.State, StateReady)
	}
	return j
}

func TestJobInstallsIntoItsOwnFolder(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t,
		member{"Oxford.mdx", "main"},
		member{"Oxford.mdd", "media"},
	)
	var rescanned int
	m := &Manager{Installed: func() { rescanned++ }}
	j := mustBegin(t, m, dest, src, false)
	if len(j.Candidates) != 1 || j.Candidates[0].Name != "Oxford" {
		t.Fatalf("candidates = %+v", j.Candidates)
	}
	// The entry names inside the archive are mechanics, not something a page
	// is given to show (D102).
	if j.Candidates[0].Main == "" {
		t.Fatal("Main is needed internally")
	}

	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	m.Wait()

	st := m.Status()
	if st.State != StateDone || len(st.Installed) != 1 || st.Installed[0] != "Oxford" {
		t.Fatalf("status = %+v", st)
	}
	if st.Done != st.Total || st.Total == 0 {
		t.Errorf("progress = %d/%d", st.Done, st.Total)
	}
	for _, want := range []string{"Oxford.mdx", "Oxford.mdd"} {
		if _, err := os.Stat(filepath.Join(dest, "Oxford", want)); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	if rescanned != 1 {
		t.Errorf("Installed hook called %d times, want 1", rescanned)
	}
	// keep=true: the archive the user pointed at is still theirs.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source was removed despite keep: %v", err)
	}
	// Nothing of the staging area survives a success, not even its root.
	if _, err := os.Stat(StageRoot(dest)); !os.IsNotExist(err) {
		t.Errorf("staging root left behind: %v", err)
	}
}

// The second import of the same bundle UPDATES the dictionary rather than
// leaving two of it in the library (D134). The test writes a different body
// the second time, so "replaced" is a claim about the bytes and not only
// about the number of folders.
func TestJobSecondImportReplaces(t *testing.T) {
	dest := t.TempDir()
	m := &Manager{}
	for i, text := range []string{"first", "second-and-longer"} {
		src := buildZip(t, member{"Oxford.mdx", text})
		j := mustBegin(t, m, dest, src, false)
		if i == 1 {
			// and the user was TOLD before choosing
			if j.Candidates[0].Existing != "Oxford" {
				t.Fatalf("Existing = %q, want Oxford", j.Candidates[0].Existing)
			}
			if j.Candidates[0].Unchanged {
				t.Error("a different body must not read as unchanged")
			}
		}
		if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
			t.Fatalf("Confirm %d: %v", i, err)
		}
		m.Wait()
		if st := m.Status(); st.State != StateDone {
			t.Fatalf("run %d: %+v", i, st)
		}
	}
	ents, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "Oxford" {
		t.Fatalf("library = %v, want one Oxford", dirNames(ents))
	}
	b, err := os.ReadFile(filepath.Join(dest, "Oxford", "Oxford.mdx"))
	if err != nil || string(b) != "second-and-longer" {
		t.Fatalf("body = %q, %v", b, err)
	}
}

// Re-importing the SAME bytes is reported as unchanged, and still installs
// cleanly - the user may well be repairing a folder they broke.
func TestJobSecondImportOfIdenticalBytesIsUnchanged(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", "main"}, member{"Oxford.mdd", "media"})
	m := &Manager{}
	mustBegin(t, m, dest, src, false)
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()

	j := mustBegin(t, m, dest, src, false)
	c := j.Candidates[0]
	if c.Existing != "Oxford" || !c.Unchanged {
		t.Fatalf("existing=%q unchanged=%v, want Oxford/true", c.Existing, c.Unchanged)
	}
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if st := m.Status(); st.State != StateDone || len(st.Installed) != 1 || st.Installed[0] != "Oxford" {
		t.Fatalf("status = %+v", st)
	}
}

// Copy is the opt-out, and the only way to end up with two.
func TestJobCopyInstallsBeside(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", "main"})
	m := &Manager{}
	for i := 0; i < 2; i++ {
		mustBegin(t, m, dest, src, false)
		if _, err := m.Confirm(dest, []int{0}, Options{Keep: true, Copy: true}); err != nil {
			t.Fatalf("Confirm %d: %v", i, err)
		}
		m.Wait()
	}
	for _, want := range []string{"Oxford", "Oxford (2)"} {
		if _, err := os.Stat(filepath.Join(dest, want, "Oxford.mdx")); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
}

// A folder that merely shares the name is still reported - the user must be
// told what is about to be replaced - but it is not "unchanged".
func TestJobExistingFolderWithOtherContents(t *testing.T) {
	dest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest, "Oxford"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "Oxford", "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	j := mustBegin(t, m, dest, buildZip(t, member{"Oxford.mdx", "main"}), false)
	if j.Candidates[0].Existing != "Oxford" || j.Candidates[0].Unchanged {
		t.Fatalf("candidate = %+v", j.Candidates[0])
	}
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	// Replaced whole, which is what "replace" means: the old folder went with
	// the stage, and nothing of it was merged into the new one.
	if _, err := os.Stat(filepath.Join(dest, "Oxford", "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("old contents survived the replace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Oxford", "Oxford.mdx")); err != nil {
		t.Errorf("new contents missing: %v", err)
	}
	if _, err := os.Stat(StageRoot(dest)); !os.IsNotExist(err) {
		t.Errorf("staging root left behind: %v", err)
	}
}

// dirNames is for a failure message that says what the library actually holds.
func dirNames(ents []os.DirEntry) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// keep=false is the user saying "I only wanted the dictionaries"; a spooled
// copy goes regardless, because it was never theirs.
func TestJobDisposesSource(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", "main"})
	m := &Manager{}
	mustBegin(t, m, dest, src, false)
	if _, err := m.Confirm(dest, []int{0}, Options{}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source kept despite delete: %v", err)
	}
}

func TestSpooledSourceIsAlwaysRemoved(t *testing.T) {
	dest := t.TempDir()
	body, err := os.Open(buildZip(t, member{"Oxford.mdx", "main"}))
	if err != nil {
		t.Fatal(err)
	}
	src, err := Spool(dest, "bundle.zip", body)
	body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(src.Path, StageRoot(dest)) {
		t.Fatalf("spooled to %q, want it under the staging root", src.Path)
	}
	m := &Manager{}
	if _, err := m.Begin(dest, src); err != nil {
		t.Fatal(err)
	}
	// keep=true, and it still goes: keep is about the user's file.
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if _, err := os.Stat(src.Path); !os.IsNotExist(err) {
		t.Errorf("spooled copy kept: %v", err)
	}
}

// An incomplete candidate is refused before a staging directory exists.
func TestJobRefusesIncompleteCandidate(t *testing.T) {
	dest := t.TempDir()
	m := &Manager{}
	mustBegin(t, m, dest, buildZip(t, member{"star.ifo", "header"}), false)
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); !errors.Is(err, ErrNotInstallable) {
		t.Fatalf("err = %v, want ErrNotInstallable", err)
	}
	if _, err := os.Stat(StageRoot(dest)); !os.IsNotExist(err) {
		t.Errorf("staged anyway: %v", err)
	}
	if st := m.Status(); st.State != StateReady {
		t.Errorf("state = %q, want the job still waiting", st.State)
	}
}

func TestJobRejects(t *testing.T) {
	dest := t.TempDir()
	m := &Manager{}
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); !errors.Is(err, ErrNoJob) {
		t.Errorf("Confirm with no job = %v, want ErrNoJob", err)
	}
	if st := m.Status(); st.State != "" {
		t.Errorf("idle status = %+v", st)
	}

	// an archive holding nothing we recognise
	junk := buildZip(t, member{"readme.txt", "hello"})
	if _, err := m.Begin(dest, Source{Path: junk}); !errors.Is(err, ErrNothingFound) {
		t.Errorf("Begin(junk) = %v, want ErrNothingFound", err)
	}
	// a format this build cannot read
	if _, err := m.Begin(dest, Source{Path: filepath.Join(t.TempDir(), "x.rar")}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Begin(.rar) = %v, want ErrUnsupported", err)
	}
	// not an archive at all
	bad := filepath.Join(t.TempDir(), "x.zip")
	if err := os.WriteFile(bad, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Begin(dest, Source{Path: bad}); err == nil {
		t.Error("Begin on a non-archive must fail")
	}

	ok := buildZip(t, member{"Oxford.mdx", "main"})
	mustBegin(t, m, dest, ok, false)
	if _, err := m.Confirm(dest, nil, Options{Keep: true}); err == nil {
		t.Error("Confirm with an empty selection must fail")
	}
	if _, err := m.Confirm(dest, []int{7}, Options{Keep: true}); err == nil {
		t.Error("Confirm with an out-of-range index must fail")
	}
	if _, err := m.Confirm("", []int{0}, Options{Keep: true}); !errors.Is(err, ErrNoDestination) {
		t.Error("Confirm with no destination must fail")
	}
}

// Cancelling a job that has only been sniffed forgets it and takes the spooled
// copy with it.
func TestCancelReadyJob(t *testing.T) {
	dest := t.TempDir()
	body, err := os.Open(buildZip(t, member{"Oxford.mdx", "main"}))
	if err != nil {
		t.Fatal(err)
	}
	src, err := Spool(dest, "bundle.zip", body)
	body.Close()
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	if _, err := m.Begin(dest, src); err != nil {
		t.Fatal(err)
	}
	m.Cancel()
	if st := m.Status(); st.State != "" {
		t.Errorf("state after cancel = %q, want idle", st.State)
	}
	if _, err := os.Stat(src.Path); !os.IsNotExist(err) {
		t.Errorf("cancelled job left its copy behind: %v", err)
	}
}

// A failed import installs nothing and keeps the user's file, whatever they
// chose - the one rule here that is not a setting. The failure is provoked the
// only way a real archive cannot be made to fail on demand: a directory that
// lies about a member's size.
func TestFailedImportKeepsSourceAndCleansUp(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", strings.Repeat("x", 4096)})

	// The directory claims one byte; the member holds four thousand.
	liar := &fakeArchive{
		entries: []Entry{{Name: "Oxford.mdx", Base: "Oxford.mdx", Size: 1, Compressed: 1}},
		bodies:  map[string]string{"Oxford.mdx": strings.Repeat("x", 4096)},
	}
	restore := openArchive
	openArchive = func(string) (Archive, error) { return liar, nil }
	t.Cleanup(func() { openArchive = restore })

	m := &Manager{}
	mustBegin(t, m, dest, src, false)
	// keep=false: the user asked for the source to go, and it stays anyway.
	if _, err := m.Confirm(dest, []int{0}, Options{}); err != nil {
		t.Fatal(err)
	}
	m.Wait()

	st := m.Status()
	if st.State != StateError || st.Error == "" {
		t.Fatalf("status = %+v, want an error", st)
	}
	if len(st.Installed) != 0 {
		t.Errorf("installed %v after a failure", st.Installed)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("a failed import must keep the source: %v", err)
	}
	if ents, err := os.ReadDir(StageRoot(dest)); err == nil && len(ents) > 0 {
		t.Errorf("staging left behind after a failure: %v", ents)
	}
	if ents, _ := os.ReadDir(dest); len(ents) > 1 {
		t.Errorf("a failed import left something in the library: %v", ents)
	}
}

func TestSweepRemovesStaleStaging(t *testing.T) {
	dest := t.TempDir()
	stale, err := Stage(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "half.mdx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := Stage(dest)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleAfter)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	Sweep([]string{dest, filepath.Join(dest, "nowhere")})

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale staging survived: %v", err)
	}
	// Another wudict may be extracting right now; a recent directory is not
	// ours to remove.
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("live staging was swept: %v", err)
	}
}

func TestSafeDirName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Oxford", "Oxford"},
		{"../../etc", "_.._etc"},
		{"a/b", "a_b"},
		{`a\b`, "a_b"},
		{"C:stuff", "C_stuff"},
		{".hidden", "hidden"},
		{"trailing.", "trailing"},
		{"trailing ", "trailing"},
		{"", "dictionary"},
		{"...", "dictionary"},
		{"CON", "_CON"},
		{"con.mdx", "_con.mdx"},
		{"a\nb", "ab"},
		{"日本語辞典", "日本語辞典"},
	}
	for _, c := range cases {
		if got := safeDirName(c.in); got != c.want {
			t.Errorf("safeDirName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := safeDirName(strings.Repeat("é", 300)); len(got) > 200 {
		t.Errorf("long name not truncated: %d bytes", len(got))
	}
}
