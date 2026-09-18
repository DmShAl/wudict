// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/wuweidict/wudict/internal/dict"
)

// browseStore ingests a headword-only fixture: browsing reads the index and
// nothing else, so no fixture here needs an article, a trigram table or FTS.
func browseStore(t *testing.T, words []string) *Store {
	t.Helper()
	entries := make([]dict.Entry, 0, len(words))
	for _, w := range words {
		entries = append(entries, h(w, "<p>x</p>"))
	}
	r := &fakeReader{
		meta:    dict.Meta{Name: "Browse Fixture", Format: "mdx", Path: "/nonexistent/browse.mdx"},
		entries: entries,
	}
	dbPath := filepath.Join(t.TempDir(), "browse.text.db")
	if _, err := IngestPlan(r, dbPath, Plan{}, nil); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestBrowseAlphabet pins the three things the strip is easy to get wrong:
// non-adjacent runs of one letter merge into one chip (NOCASE folds ASCII and
// nothing else, so "Ärger" and "äpfel" are separated by every C-with-cedilla
// in the dictionary), a whole non-alphabetic script collapses to one chip, and
// Korean gets its nineteen leading jamo rather than one chip or eleven
// thousand.
func TestBrowseAlphabet(t *testing.T) {
	s := browseStore(t, []string{
		"1000", "apple", "azalea", "Zebra", // ASCII, case-folded
		"Ärger", "Ça va", "äpfel", // one A chip, split around C
		"—dash",    // not a letter
		"ア",        // katakana
		"中国", "漢字", // han, one chip
		"각", "나라", // hangul, two leads
	})
	got, err := s.Alphabet()
	if err != nil {
		t.Fatal(err)
	}
	want := []dict.Letter{
		{Letter: "0-9", Offset: 0, Count: 1},
		{Letter: "A", Offset: 1, Count: 4}, // apple azalea + Ärger + äpfel
		{Letter: "Z", Offset: 3, Count: 1},
		{Letter: "C", Offset: 5, Count: 1},
		{Letter: "#", Offset: 7, Count: 1},
		{Letter: "ア", Offset: 8, Count: 1},
		{Letter: "漢", Offset: 9, Count: 2},
		{Letter: "ㄱ", Offset: 11, Count: 1},
		{Letter: "ㄴ", Offset: 12, Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("alphabet = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chip %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The server totals the dictionary by summing the strip, so the strip has
	// to account for every row exactly once.
	sum := 0
	for _, l := range got {
		sum += l.Count
	}
	if sum != 13 {
		t.Errorf("chip counts sum to %d, want 13", sum)
	}
	// Every chip offset is a browse offset: it addresses the sequence Page
	// pages through, or the jump lands somewhere else than the tab promises.
	for _, l := range got {
		w, err := s.Page(l.Offset, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(w) != 1 {
			t.Fatalf("chip %q offset %d addresses nothing", l.Letter, l.Offset)
		}
		if at, err := s.Locate(w[0]); err != nil || at != l.Offset {
			t.Errorf("Locate(%q) = %d, %v; want %d", w[0], at, err, l.Offset)
		}
	}
}

// TestBrowseChipCut covers the reason a Chinese dictionary is browsable at all:
// 漢 alone is every headword in the book behind one tab, so a run longer than
// the span is cut into pieces labelled by the headword each opens on.
func TestBrowseChipCut(t *testing.T) {
	const han = 7000
	words := make([]string, 0, han+10)
	for i := 0; i < 10; i++ {
		words = append(words, fmt.Sprintf("aaa%03d", i))
	}
	for i := 0; i < han; i++ {
		words = append(words, string(rune(0x4E00+i)))
	}
	s := browseStore(t, words)
	got, err := s.Alphabet()
	if err != nil {
		t.Fatal(err)
	}
	// 7010 headwords: below the budget, so the span is the ceiling, and the
	// Han run becomes 3000 + 3000 + 1000.
	wantOffsets := []int{0, 10, 3010, 6010}
	wantCounts := []int{10, 3000, 3000, 1000}
	if len(got) != 4 {
		t.Fatalf("alphabet = %+v, want 4 chips", got)
	}
	for i, l := range got {
		if l.Offset != wantOffsets[i] || l.Count != wantCounts[i] {
			t.Errorf("chip %d = %+v, want offset %d count %d", i, l, wantOffsets[i], wantCounts[i])
		}
		if l.Count > chipCeiling {
			t.Errorf("chip %+v is longer than the span", l)
		}
	}
	if got[0].Letter != "A" || got[1].Letter != "漢" {
		t.Errorf("first chips = %q %q, want A 漢", got[0].Letter, got[1].Letter)
	}
	// A cut piece is labelled with the word it opens on, which is the whole
	// promise the tab makes.
	for _, l := range got[2:] {
		w, err := s.Page(l.Offset, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(w) != 1 || w[0] != l.Letter {
			t.Errorf("chip %q opens on %v", l.Letter, w)
		}
	}
}
