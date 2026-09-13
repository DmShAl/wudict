// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package intake turns arbitrary bytes into candidate dictionaries.
//
// It is the stage in front of discovery, and it exists because of how these
// formats are actually distributed: as a .zip or .7z holding one or several
// dictionaries, each of which is a main file plus the companions it cannot
// work without. On a desktop that is a double-click and a drag; on a phone
// there is no comfortable way to unpack an archive into an app's own folder at
// all, which is the wall this removes.
//
// The division of labour with the rest of the tree:
//
//   - internal/dict discovers dictionaries that are ALREADY in place, and owns
//     the knowledge of which files make up one. This package borrows that
//     knowledge through dict.ClassifyName and dict.CompanionStem rather than
//     restating it, so an archive is grouped by exactly the rule a folder is.
//   - internal/store prepares a discovered dictionary into the library. This
//     package stops one step short of it: intake ends by putting files where a
//     rescan will find them, and preparation is then the ordinary path, under
//     the ordinary worker limits.
//
// Every input here is hostile by assumption. An archive is a list of names and
// declared sizes, all attacker-chosen: the names can climb out of the
// destination, the sizes can lie in either direction, and the entry count can
// be large enough to be the attack by itself. Nothing in this package trusts a
// declared value it has not also enforced while writing.
package intake

import (
	"encoding/json"
	"io"
	"strings"
)

// Entry is one member of an archive as its directory describes it - a name and
// two numbers, none of which has been verified against the bytes yet.
type Entry struct {
	// Name is the archive path, slash-separated, as this package normalised
	// it. It is the key Open takes.
	Name string
	// Dir is Name's directory part, "" at the archive root. Grouping is per
	// directory: two dictionaries sharing a stem in one archive would collide
	// on their companions' names, exactly as they would in one folder.
	Dir string
	// Base is Name's last element.
	Base string
	// Size is the DECLARED uncompressed size. Treated as a hint for the
	// sniffer's arithmetic and never as a promise: extraction enforces it.
	Size int64
	// Compressed is the stored size, for the ratio check that catches an
	// archive whose declared size is honest and enormous.
	Compressed int64
}

// Archive is a container that can be listed without being decompressed. Both
// implementations read only the directory: a zip's central directory, a 7z's
// header. Sniffing an 8 GB archive therefore costs a few kilobytes of reads
// and no decompressor state at all.
type Archive interface {
	// Entries lists the files, directories excluded.
	Entries() []Entry
	// Open streams one entry by its Name. The caller must Close it before
	// opening the next: a 7z stream is sequential, and holding two open is
	// what multiplies a 64 MiB LZMA2 window.
	Open(name string) (io.ReadCloser, error)
	Close() error
}

// Candidate is one dictionary found inside an archive: what would be installed
// if the user says yes.
type Candidate struct {
	// Name is what the user will see and what the destination folder is named
	// after - the main file's stem, which is what every companion is named
	// after too.
	Name string `json:"name"`
	// Format is the main file's extension (".mdx", ".ifo"), as dict spells it.
	Format string `json:"format"`
	// Main is the archive entry name of the main file. Internal: the user
	// chooses a dictionary, never a path inside an archive (D102).
	Main string `json:"-"`
	// Files are every entry to extract, main file first. Paths are relative to
	// the archive root; extraction re-roots them on the candidate's directory,
	// so a StarDict "res/" subtree keeps its shape and everything else
	// flattens to one folder.
	//
	// Internal in this form, and published as base names by MarshalJSON: what
	// the user needs is WHICH FILES, not where they sat inside a bundle
	// (D102, D137).
	Files []string `json:"-"`
	// Size is the total declared uncompressed size of Files.
	Size int64 `json:"size"`
	// Missing names the companions this format cannot work without and this
	// archive does not have (".idx", ".dict"). Non-empty means the candidate
	// is offered for display and refused for installation: a StarDict .ifo is
	// a header naming an index it does not contain, and installing one alone
	// produces a dictionary that opens and answers nothing.
	Missing []string `json:"missing,omitempty"`

	// Existing is the library folder that ALREADY holds this dictionary, ""
	// when none does. It exists because the alternative was silence: importing
	// the same bundle twice used to produce a second numbered folder, which
	// means doubled search results and a second full preparation pass, and the
	// user was never told (D134).
	Existing string `json:"existing,omitempty"`
	// Unchanged says that folder holds the same files at the same sizes the
	// archive declares - so installing would replace a dictionary with itself.
	// Size, not a checksum: that is the test store.SourceChanged already makes
	// before it re-indexes anything, and hashing a two-gigabyte bundle on a
	// phone to answer a question a stat answers is not a trade worth making.
	Unchanged bool `json:"unchanged,omitempty"`
	// Stale says that folder holds no dictionary at all - a folder a removal
	// left behind, or one the user made that happens to share the name. The
	// install takes it over exactly as it would a real one, so it is still
	// reported; what changes is the sentence, because telling somebody a
	// dictionary they have just removed is "installed, will be updated" is
	// telling them something they can see is false (D137).
	Stale bool `json:"stale,omitempty"`

	// sizes are Files' declared sizes, in the same order. Unexported because
	// it is arithmetic, not something a user chooses between (D102).
	sizes []int64
}

// Complete reports whether the candidate can actually be installed.
func (c Candidate) Complete() bool { return len(c.Missing) == 0 }

// Names is what this dictionary is, said in file names: the files an install
// would write, main file first, as the user would recognise them in a download
// folder (D137).
//
// Two things are deliberately dropped on the way out of Files. The archive
// directory, because the user chose a dictionary and not a location inside a
// bundle; and the contents of a subtree, collapsed to the subtree itself
// ("res/"), because a StarDict resource folder is thousands of images and
// naming them is not a list anybody reads - nor one worth sending to a phone.
func (c Candidate) Names() []string {
	base := candidateDir(c)
	out := make([]string, 0, len(c.Files))
	seen := make(map[string]bool, len(c.Files))
	for _, f := range c.Files {
		rel := strings.TrimPrefix(strings.TrimPrefix(f, base), "/")
		if rel == "" {
			continue
		}
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			rel = rel[:i+1] // the subtree, named once
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		out = append(out, rel)
	}
	return out
}

// MarshalJSON publishes the names alongside the rest. Derived on the way out
// rather than stored beside Files, so there is one list of what an install
// takes and no second one to fall out of step with it.
func (c Candidate) MarshalJSON() ([]byte, error) {
	type wire Candidate // no method set: this would otherwise recurse
	return json.Marshal(struct {
		wire
		Files []string `json:"files,omitempty"`
	}{wire(c), c.Names()})
}

// The limits. Every one of them is a bound on work done on behalf of an
// unverified input, so each is enforced at the point the work happens rather
// than checked once at the door.
const (
	// maxEntries bounds the directory itself. 100k matches the Android SAF
	// importer's enumeration cap: far above any real dictionary bundle, far
	// below the point where a crafted directory is the attack.
	maxEntries = 100_000

	// maxDepth bounds path nesting, likewise matching the SAF importer. A
	// legitimate bundle is two or three deep.
	maxDepth = 32

	// maxCandidateBytes bounds one dictionary's extraction. Large, because
	// real dictionaries are: a full Wikipedia .zim runs to tens of gigabytes.
	// The number exists so that a declared size which is absurd rather than
	// merely large is refused before any disk is touched.
	maxCandidateBytes = 64 << 30

	// maxRatio bounds compression ratio per entry. Deflate tops out near
	// 1032:1 on a stream of one repeated byte, so anything past this was
	// built to be a bomb; dictionary text compresses to single figures.
	maxRatio = 2000

	// copyBufBytes is the streaming window, matching the Android importer's.
	// Extraction is O(1) in RAM by construction: one buffer, one open entry.
	copyBufBytes = 1 << 20
)
