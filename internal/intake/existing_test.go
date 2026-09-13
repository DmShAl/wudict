// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"os"
	"path/filepath"
	"testing"
)

// The marking must re-root paths EXACTLY as Extract does, or a bundle packed
// as "Dicts/English/Oxford.mdx" would be compared against a folder nested the
// same way, find nothing, and quietly install a second copy of a dictionary
// that is already there.
func TestExistingMatchesTheLayoutExtractWouldProduce(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t,
		member{"Dicts/English/Oxford.mdx", "main"},
		member{"Dicts/English/Oxford.mdd", "media"},
	)
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
}

// A StarDict res/ subtree keeps its shape on disk, so the comparison has to
// look for it there and not at the top of the folder.
func TestExistingFollowsAStarDictResourceSubtree(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t,
		member{"sd/x.ifo", "header"},
		member{"sd/x.idx", "index"},
		member{"sd/x.dict", "bodies"},
		member{"sd/res/a.png", "image"},
	)
	m := &Manager{}
	mustBegin(t, m, dest, src, false)
	if _, err := m.Confirm(dest, []int{0}, Options{Keep: true}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if _, err := os.Stat(filepath.Join(dest, "x", "res", "a.png")); err != nil {
		t.Fatalf("resource not where the comparison will look: %v", err)
	}

	j := mustBegin(t, m, dest, src, false)
	if c := j.Candidates[0]; c.Existing != "x" || !c.Unchanged {
		t.Fatalf("existing=%q unchanged=%v, want x/true", c.Existing, c.Unchanged)
	}

	// One file removed by hand is enough to make it "not the same", which is
	// the outcome that matters: a user repairing a folder must be offered the
	// replace, not told there is nothing to do.
	if err := os.Remove(filepath.Join(dest, "x", "res", "a.png")); err != nil {
		t.Fatal(err)
	}
	j = mustBegin(t, m, dest, src, false)
	if c := j.Candidates[0]; c.Existing != "x" || c.Unchanged {
		t.Fatalf("existing=%q unchanged=%v, want x/false", c.Existing, c.Unchanged)
	}
}

// No dictionary folder yet - a first-run server - is not a reason to guess.
func TestExistingSaysNothingWithoutADestination(t *testing.T) {
	cands := []Candidate{{Name: "Oxford", Main: "Oxford.mdx", Files: []string{"Oxford.mdx"}, sizes: []int64{4}}}
	markExisting("", cands)
	if cands[0].Existing != "" || cands[0].Unchanged {
		t.Fatalf("marked against nothing: %+v", cands[0])
	}
	// and a folder that simply is not there
	markExisting(t.TempDir(), cands)
	if cands[0].Existing != "" || cands[0].Unchanged {
		t.Fatalf("marked against an empty library: %+v", cands[0])
	}
}

// The folder a removal left behind is NAMED - the install takes it over, so
// saying nothing would replace it silently - but it is not an installation.
// Removing a dictionary unlinks its files; the folder that held them could
// survive, and reading its mere existence as "installed" told the user their
// next import would UPDATE a dictionary they had just watched disappear
// (D137).
func TestEmptyFolderIsReportedAsStaleNotInstalled(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", "main"})
	if err := os.MkdirAll(filepath.Join(dest, "Oxford"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := &Manager{}
	j := mustBegin(t, m, dest, src, false)
	c := j.Candidates[0]
	if c.Existing != "Oxford" || !c.Stale || c.Unchanged {
		t.Fatalf("existing=%q stale=%v unchanged=%v, want Oxford/true/false",
			c.Existing, c.Stale, c.Unchanged)
	}
}

// A folder holding files that are not a dictionary is the same case: what
// makes a folder an installation is a dictionary in it, and a leftover note or
// a stray .css is not one.
func TestFolderWithoutAMainFileIsStale(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", "main"})
	dir := filepath.Join(dest, "Oxford")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Oxford.css"), []byte("p{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{}
	j := mustBegin(t, m, dest, src, false)
	if c := j.Candidates[0]; c.Existing != "Oxford" || !c.Stale {
		t.Fatalf("existing=%q stale=%v, want Oxford/true", c.Existing, c.Stale)
	}
}

// The collision D134 exists for is unaffected: a folder of that name holding
// SOMEBODY ELSE'S dictionary is still reported, because the install would
// otherwise land beside it under a numbered name with the user never told.
func TestFolderHoldingAnotherDictionaryStillCounts(t *testing.T) {
	dest := t.TempDir()
	src := buildZip(t, member{"Oxford.mdx", "main"})
	dir := filepath.Join(dest, "Oxford")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Something.mdx"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{}
	j := mustBegin(t, m, dest, src, false)
	c := j.Candidates[0]
	if c.Existing != "Oxford" || c.Unchanged || c.Stale {
		t.Fatalf("existing=%q unchanged=%v stale=%v, want Oxford/false/false",
			c.Existing, c.Unchanged, c.Stale)
	}
}
