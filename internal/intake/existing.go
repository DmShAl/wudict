// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wuweidict/wudict/internal/dict"
)

// Is this dictionary already in the library? (D134)
//
// The question is asked once, at sniff time, and its answer travels with the
// candidate so the user is told BEFORE choosing rather than shown a numbered
// folder afterwards. What the question is not: an audit of the library, a
// checksum, or a merge. It compares the folder an install would create against
// the folder that is there, by the one test the rest of this program already
// uses for "has this file changed" - size, with nothing hashed
// (store.SourceChanged, internal/store/library.go).
//
// Both possible mistakes are cheap by construction. A false "existing" offers
// a replacement of a folder that turns out to hold something else, which the
// user sees named and can untick; a false "not existing" installs beside it,
// which is exactly what happened before this existed.

// markExisting annotates candidates with the library folder that already holds
// them. dest may be empty - a server with no dictionary folder configured -
// and then there is nothing to compare against and nothing to say.
func markExisting(dest string, cands []Candidate) {
	if dest == "" {
		return
	}
	for i := range cands {
		cands[i].Existing, cands[i].Unchanged, cands[i].Stale = existingDict(dest, cands[i])
	}
}

// existingDict reports the folder in dest that already holds this candidate,
// and whether its contents match what the archive declares.
//
// The folder is the one uniqueDir would have picked first, which is the only
// honest place to look: a dictionary installed from this archive before is in
// safeDirName(c.Name), and a dictionary installed from somewhere else that
// happens to own that name is a collision the user must be told about anyway,
// since the install would otherwise land beside it under a numbered name.
// The folder's EXISTENCE is the collision and is always reported; whether it
// holds a dictionary is a separate question, and conflating the two is what
// produced the complaint this was rewritten for. Removing a dictionary unlinks
// its files and used to leave its folder behind, so the next import of the
// same bundle announced "installed, will be updated" about a dictionary the
// user had just watched disappear (D137).
//
// Reporting nothing in that case would have been the worse fix: the install
// takes that folder name either way, so the user would lose whatever is in it
// with no warning at all. The folder is named, and what it holds decides which
// sentence is true about it.
func existingDict(dest string, c Candidate) (name string, unchanged, stale bool) {
	name = safeDirName(c.Name)
	dir := filepath.Join(dest, name)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", false, false
	}
	if !holdsDictionary(dir) {
		// A folder left behind by a removal, or the user's own folder that
		// happens to share the name. Both are taken over by the install and
		// neither is an installed dictionary.
		return name, false, true
	}
	return name, sameFiles(dir, c), false
}

// holdsDictionary reports whether the folder contains a dictionary at all -
// any main file, not necessarily this candidate's.
//
// Any, because a folder holding somebody ELSE'S dictionary under this name is
// still a dictionary about to be replaced, and the user is shown the name
// either way. Only the folder with nothing a dictionary is made of is
// something other than an installation.
func holdsDictionary(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && dict.ClassifyName(e.Name()) == dict.KindMain {
			return true
		}
	}
	return false
}

// sameFiles compares the candidate's declared files against what is in dir.
// Every file must be there at the declared size; anything else in the folder is
// not this function's business, because the user's own additions - a note, a
// copy of the licence - are not evidence that the dictionary differs.
//
// Sizes come from the archive directory and are therefore attacker-chosen,
// which does not matter here: the worst a lie achieves is a wrong answer to
// "is this the same", and the user is shown the folder name either way. Nothing
// is opened and nothing is read.
func sameFiles(dir string, c Candidate) bool {
	if len(c.Files) == 0 || len(c.sizes) != len(c.Files) {
		return false
	}
	base := candidateDir(c)
	for i, name := range c.Files {
		rel := strings.TrimPrefix(strings.TrimPrefix(name, base), "/")
		if rel == "" {
			return false
		}
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != c.sizes[i] {
			return false
		}
	}
	return true
}
