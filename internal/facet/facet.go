// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package facet derives the handful of GROUPS a dictionary belongs to, from
// what is already known about it: its declared language, its file and folder
// names, and its own title. Nothing here opens a dictionary, reads an index or
// touches the disk - Derive is a pure function over strings the caller already
// has, which is why it can run inside the /api/dicts fan-out at no cost.
//
// The groups exist for one purpose: to let the dictionary picker offer "every
// English dictionary" or "the encyclopedias" as a single choice, instead of a
// flat list of a hundred names. They are a CONVENIENCE built out of evidence,
// not a classification the app believes in. Two rules follow from that, and
// both are load-bearing:
//
//  1. Derive, never persist. Nothing here is written to state.json or to a
//     library folder, so a better rule tomorrow reclassifies everything on the
//     next load and no user ever has to undo a stale verdict.
//
//  2. Never invent a value from absence. A dictionary that says nothing about
//     its language joins no language group; it does not become English. The
//     search path may assume English for lemmatization because that guess is
//     invisible and validated against a real headword index (see
//     lang.Resolve), but a group label is SHOWN, and one visibly misfiled
//     dictionary discredits every correctly filed one.
//
// A group is a (facet, value) pair. A dictionary may hold several - an Oxford
// English-Russian encyclopedia is in four - and the UI is free to show none of
// them: a value with a single member, or a facet with a single value, sorts
// nothing and is dropped by the client that renders the list.
package facet

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/wuweidict/wudict/internal/lang"
)

// Group is one (facet, value) membership, carrying its own labels so the
// client holds no taxonomy at all: it renders what it is given and never has
// to know that "lang" means language or that "mono" is a kind of dictionary
// (D102 - none of these internal ids reach a user's eye).
//
// FO is the facet's rank in the picker. It travels per row because a row is
// the only thing this API sends: the facets that exist are not known until the
// last dictionary has resolved, so there is no header line to put an order in.
type Group struct {
	F  string `json:"f"`  // facet id: lang | dir | kind | pub
	FL string `json:"fl"` // facet label, as the picker heads the group
	FO int    `json:"fo"` // facet rank, ascending
	V  string `json:"v"`  // value id, unique within the facet
	VL string `json:"vl"` // value label, as the picker names the choice
}

// Input is everything Derive is allowed to look at.
//
// Path must already be the path language conventions are written on - for a
// prepared dictionary that is the library FOLDER, not the text.db inside it
// (the server's langPath does that reduction).
type Input struct {
	Name     string   // the dictionary's own title
	Path     string   // source path, or library folder
	Roots    []string // configured dictionary directories, to bound the folder walk
	Declared string   // what the format declares for the HEADWORD language
	Contents string   // what it declares for the ARTICLE language (DSL only)
}

const (
	foLang = 1
	foDir  = 2
	foKind = 3
	foPub  = 4
)

// Derive returns the groups a dictionary belongs to, in facet order. nil when
// nothing could be established, which is a normal answer.
func Derive(in Input) []Group {
	var out []Group
	if code := lang.Resolve(in.Declared, in.Path, in.Roots, in.Name); code != "" {
		out = append(out, Group{F: "lang", FL: "Language", FO: foLang, V: code, VL: lang.Name(code)})
	}
	if v, l := direction(in); v != "" {
		out = append(out, Group{F: "dir", FL: "Type", FO: foDir, V: v, VL: l})
	}
	out = append(out, match(kinds, "kind", "Content", foKind, in.Name)...)
	out = append(out, match(publishers, "pub", "Publisher", foPub, in.Name)...)
	return out
}

// direction says whether a dictionary covers one language or two, and only
// ever from an EXPLICIT pair: two declared fields, or two language tokens
// joined the way a dictionary title joins them ("English-Russian", "es-es",
// "Ru–En"). A single language hint says nothing about this - "Oxford English
// Dictionary" and "Oxford English-Russian" both resolve to English - so a
// dictionary with one hint and no pair joins no group here.
func direction(in Input) (string, string) {
	a, b := lang.FromDeclared(in.Declared), lang.FromDeclared(in.Contents)
	if a == "" || b == "" {
		a, b = pair(in.Name)
	}
	if a == "" || b == "" {
		a, b = pair(stem(in.Path))
	}
	switch {
	case a == "" || b == "":
		return "", ""
	case a == b:
		// es-es, spa-spa, "English-English": a pair that names the same
		// language twice is the standard way a monolingual dictionary is
		// labelled, and it is stated, not inferred.
		return "mono", "Monolingual"
	default:
		return "bi", "Bilingual"
	}
}

// pairRe finds two letter tokens joined by one of the separators a language
// pair is written with. The tokens are deliberately unanchored to the whole
// string - a title puts the pair anywhere ("Oxford Russian-English Dictionary",
// "Collins (En-Es) 2nd ed.") - and both sides must resolve to a language, which
// is what keeps "Anglo-Saxon", "Latin-American" and "e-book" out.
var pairRe = regexp.MustCompile(`(?i)(\p{L}{2,})[ \t]*[-\x{2013}\x{2014}>_/][ \t]*(\p{L}{2,})`)

func pair(s string) (string, string) {
	for _, m := range pairRe.FindAllStringSubmatch(s, -1) {
		a, b := lang.Normalize(m[1]), lang.Normalize(m[2])
		if a != "" && b != "" {
			return a, b
		}
	}
	return "", ""
}

// stem is the file name without its extension, which is the only part of a
// path a pair is ever written in ("en-es-apresyan.mdx"). The LAST dot, so
// "dict.dsl.dz" loses one extension rather than its name.
func stem(p string) string {
	base := filepath.Base(p)
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	return base
}

// rule maps one word or phrase, matched as whole words in a dictionary's
// title, to the group it puts the dictionary in. Several rules may share an
// id; the first one that fires wins and the rest are skipped.
type rule struct{ pat, id, label string }

// kinds: what the dictionary IS, where its own title says so plainly. The
// vocabulary is closed on purpose - every entry here is a word publishers put
// in titles to mean exactly this, and a word that is merely common ("new",
// "concise", "student") groups nothing.
var kinds = []rule{
	{"encyclopedia", "encyclopedia", "Encyclopedias"},
	{"encyclopaedia", "encyclopedia", "Encyclopedias"},
	{"encyclopedic", "encyclopedia", "Encyclopedias"},
	{"wikipedia", "encyclopedia", "Encyclopedias"},
	{"thesaurus", "thesaurus", "Thesauri"},
	{"synonyms", "thesaurus", "Thesauri"},
	{"antonyms", "thesaurus", "Thesauri"},
	{"idioms", "idioms", "Idioms & phrases"},
	{"idiomatic", "idioms", "Idioms & phrases"},
	{"phrasebook", "idioms", "Idioms & phrases"},
	{"proverbs", "idioms", "Idioms & phrases"},
	{"slang", "slang", "Slang"},
	{"etymology", "etymology", "Etymology"},
	{"etymological", "etymology", "Etymology"},
	{"abbreviations", "abbrev", "Abbreviations"},
	{"acronyms", "abbrev", "Abbreviations"},
	{"grammar", "grammar", "Grammar"},
	{"medical", "medical", "Medicine"},
	{"medicine", "medical", "Medicine"},
	{"legal", "legal", "Law"},
	{"law", "legal", "Law"},
}

// publishers: the houses whose names are on enough dictionaries for a group to
// be worth offering, plus the abbreviations that are as well known as the
// names. Only unambiguous tokens - "MW" and "ODE" are left out because they
// are also an ordinary abbreviation and an ordinary English word, and a group
// is a claim shown to the user, not a guess.
var publishers = []rule{
	{"oxford", "oxford", "Oxford"},
	{"oald", "oxford", "Oxford"},
	{"oed", "oxford", "Oxford"},
	{"soed", "oxford", "Oxford"},
	{"cambridge", "cambridge", "Cambridge"},
	{"cald", "cambridge", "Cambridge"},
	{"cide", "cambridge", "Cambridge"},
	{"collins", "collins", "Collins"},
	{"cobuild", "collins", "Collins"},
	{"chambers", "chambers", "Chambers"},
	{"longman", "longman", "Longman"},
	{"ldoce", "longman", "Longman"},
	{"macmillan", "macmillan", "Macmillan"},
	{"merriam webster", "webster", "Webster"},
	{"merriam", "webster", "Webster"},
	{"webster", "webster", "Webster"},
	{"american heritage", "ahd", "American Heritage"},
	{"ahd", "ahd", "American Heritage"},
	{"britannica", "britannica", "Britannica"},
	{"random house", "randomhouse", "Random House"},
	{"duden", "duden", "Duden"},
	{"langenscheidt", "langenscheidt", "Langenscheidt"},
	{"larousse", "larousse", "Larousse"},
	{"le robert", "robert", "Le Robert"},
}

// match runs one table over a title, at most one group per value id.
func match(rules []rule, facet, label string, fo int, name string) []Group {
	n := norm(name)
	if n == "  " || n == " " {
		return nil
	}
	var out []Group
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.id] || !strings.Contains(n, " "+r.pat+" ") {
			continue
		}
		seen[r.id] = true
		out = append(out, Group{F: facet, FL: label, FO: fo, V: r.id, VL: r.label})
	}
	return out
}

// norm lower-cases a title and reduces every run of non-alphanumerics to a
// single space, padded at both ends, so a table pattern can be matched as
// whole words with one Contains: "Webster's New Int'l (1913)" becomes
// " webster s new int l 1913 ", where " webster " hits and " law " cannot fire
// inside "lawyer" or "Malawi".
func norm(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(' ')
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	if !space {
		b.WriteByte(' ')
	}
	return b.String()
}
