// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package dsl

import "strings"

// titleResult carries the headword variants of one DSL title line plus its
// display form:
//
//	(...)  optional part: one variant with it and one without, for EVERY such
//	       part independently - bracketed in Display
//	{...}  unsorted part: rendered only into Display (markup allowed)
//	{{..}} comment: dropped from both
//	\x     escaped char
type titleResult struct {
	// Keys are the lookup variants, the fully-expanded one first (which is
	// also the form `~` mirrors into the body). Deduplicated; empty only when
	// the line indexes nothing at all.
	Keys    []string
	Display string // HTML display title (unsorted parts rendered)
}

// titlePart is one run of a headword line that either always belongs to a key
// (opt false) or belongs to it only in half the variants (opt true).
type titlePart struct {
	text string
	opt  bool
}

// maxOptionalParts caps the cartesian expansion. n optional parts are 2^n
// headwords, and the 246-character limit on a title leaves room for enough of
// them to turn one line into millions of index rows. Six is far past anything
// a lexicographer writes (`(пре)вращать(ся)` is two) and bounds one line at 64
// keys; past it the line falls back to the two extremes - everything in and
// everything out - which is what the old two-variant parser produced for every
// line, so no dictionary loses ground.
const maxOptionalParts = 6

// transformTitle parses one headword line. The three constructs are
// independent and may be nested in either order, so this is one flat loop
// with a paren flag rather than a paren scanner with its own alphabet:
// `(слов{[']}а{[/']}рной)` puts an unsorted stress mark inside an optional
// part, and a paren loop that did not know about `{` would copy the braces
// verbatim into the lookup key and make the entry unfindable.
func transformTitle(line string) titleResult {
	var display, cur strings.Builder
	var parts []titlePart
	pos := 0
	inParen := false

	// Headword variants stay raw (they are lookup keys); only the
	// display form is HTML-escaped. Bytes are appended raw so multi-byte
	// UTF-8 sequences survive (string(byte) would mangle them).
	escByte := func(b *strings.Builder, c byte) {
		switch c {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(c)
		}
	}
	// flush closes the run being accumulated, recording whether it was inside
	// an optional part. opt is passed rather than read from inParen because the
	// flag has to change at the same instant the run ends.
	flush := func(opt bool) {
		if cur.Len() > 0 {
			parts = append(parts, titlePart{text: cur.String(), opt: opt})
			cur.Reset()
		}
	}
	// add appends one indexable byte to the current run and to the display.
	add := func(c byte) {
		cur.WriteByte(c)
		escByte(&display, c)
	}

	for pos < len(line) {
		c := line[pos]
		pos++
		switch c {
		case '\\':
			if pos >= len(line) {
				add(c)
				break
			}
			add(line[pos])
			pos++
		case '(':
			if inParen { // stray '(': literal, DSL does not nest optional parts
				add(c)
				break
			}
			// The brackets themselves are index syntax, so they never reach a
			// key - but they are content in the display form, which is what
			// Lingvo and GoldenDict show: "abandonar(se)", not "abandonarse".
			// Without them the reader cannot tell which part is optional.
			flush(false)
			inParen = true
			display.WriteByte(c)
		case ')':
			if !inParen {
				add(c)
				break
			}
			flush(true)
			inParen = false
			display.WriteByte(c)
		case '{':
			// `{{...}}` is a comment even in a headword: consume, emit nothing.
			if pos < len(line) && line[pos] == '{' {
				if i := strings.Index(line[pos+1:], "}}"); i >= 0 {
					pos += 1 + i + 2
				} else {
					pos = len(line)
				}
				break
			}
			start := pos
			depth := 1
			for pos < len(line) && depth > 0 {
				b := line[pos]
				pos++
				switch b {
				case '\\':
					if pos < len(line) {
						pos++
					}
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			inner := line[start:] // unterminated `{`: take the rest verbatim
			if depth == 0 {
				inner = line[start : pos-1]
			}
			// Fragment, not body: the space in `{headword } suffix` is the word
			// separator and must be preserved.
			if html, _, err := transformFragment(inner, ""); err == nil {
				display.WriteString(html)
			}
		default:
			add(c)
		}
	}
	flush(inParen) // an unterminated '(' still ends a run

	return titleResult{
		// Keys get their interior whitespace collapsed (in expandOptional), the
		// display form does not. Deleting an unsorted or optional part leaves
		// the spaces that surrounded it behind - `sample {unsorted part} card`
		// keys as "sample  card", which nobody can type - and a key exists only
		// to be matched. Display is the opposite case: its spacing is the
		// author's.
		Keys:    expandOptional(parts),
		Display: strings.TrimSpace(display.String()),
	}
}

// first is Keys[0], the canonical variant, or "" for a line that indexes
// nothing.
func (t titleResult) first() string {
	if len(t.Keys) == 0 {
		return ""
	}
	return t.Keys[0]
}

// expandOptional turns the runs of one title line into its lookup keys: the
// cartesian product over the optional parts, fully-expanded first.
// `(пре)вращать(ся)` is FOUR headwords - вращать, вращаться, превращать,
// превращаться (lingvo-ref "Заголовок статьи") - not the two that keeping only
// "all in" and "all out" yields, and the two it dropped are the two a reader is
// most likely to type.
func expandOptional(parts []titlePart) []string {
	n := 0
	for _, p := range parts {
		if p.opt {
			n++
		}
	}
	build := func(in func(rank int) bool) string {
		var b strings.Builder
		rank := 0
		for _, p := range parts {
			if p.opt {
				keep := in(rank)
				rank++
				if !keep {
					continue
				}
			}
			b.WriteString(p.text)
		}
		return collapseSpace(b.String())
	}
	all := func(int) bool { return true }
	none := func(int) bool { return false }

	var out []string
	seen := map[string]bool{}
	emit := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	switch {
	case n == 0:
		emit(build(all))
	case n > maxOptionalParts:
		emit(build(all))
		emit(build(none))
	default:
		// Counting DOWN keeps the fully-expanded form first: it is the key the
		// rest of the pipeline treats as canonical (the card's `~`, the
		// sub-card back-reference).
		for mask := 1<<uint(n) - 1; mask >= 0; mask-- {
			m := uint(mask)
			emit(build(func(rank int) bool { return m&(1<<uint(rank)) != 0 }))
		}
	}
	return out
}

// collapseSpace trims and squeezes runs of whitespace to a single space.
func collapseSpace(s string) string {
	if !strings.ContainsAny(s, " \t\n\v\f\r ") {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}
