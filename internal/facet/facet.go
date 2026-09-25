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
	"slices"
	"strings"
	"unicode"

	"github.com/wuweidict/wudict/internal/lang"
)

// Group is one (facet, value) membership, carrying its own labels so the
// client holds no taxonomy at all: it renders what it is given and never has
// to know that "lang" means language or that "en-ru" is a pair of them
// (D102 - none of these internal ids reach a user's eye).
//
// FO is the facet's rank in the picker. It travels per row because a row is
// the only thing this API sends: the facets that exist are not known until the
// last dictionary has resolved, so there is no header line to put an order in.
type Group struct {
	F  string `json:"f"`  // facet id: lang | pair | kind | pub
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
	Contents string   // what it declares for the ARTICLE language (DSL, BGL)
}

const (
	foLang = 1
	foPair = 2
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
	if v, l := languagePair(in); v != "" {
		out = append(out, Group{F: "pair", FL: "Language pair", FO: foPair, V: v, VL: l})
	}
	out = append(out, match(kinds, "kind", "Content", foKind, in.Name)...)
	out = append(out, match(publishers, "pub", "Publisher", foPub, in.Name)...)
	return out
}

// languagePair names the two languages a dictionary joins, from an EXPLICIT
// pair only: two declared fields, or two language tokens joined the way a
// title, a file name or a folder name joins them ("English-Russian", "es-es",
// "En-Ru/", "英汉"). A single language hint says nothing about this - "Oxford
// English Dictionary" and "Oxford English-Russian" both resolve to English -
// so a dictionary with one hint and no pair joins no group here.
//
// It replaced a mono/bi "Type" facet (D149). "Bilingual" gathered every pair
// into one choice nobody searches with; a pair is the unit a reader actually
// picks by - the dictionaries that explain English in Russian. The pair is
// UNDIRECTED: en-ru and ru-en are one value. Directed, each half is often a
// single dictionary and the single-member rule would drop both; together they
// are the set a reader of that pair wants, and a query only matches the
// headwords of the side it is written in anyway.
func languagePair(in Input) (string, string) {
	a, b := lang.FromDeclared(in.Declared), lang.FromDeclared(in.Contents)
	if a == "" || b == "" {
		a, b = pair(in.Name)
	}
	if a == "" || b == "" {
		a, b = pair(stem(in.Path))
	}
	if a == "" || b == "" {
		a, b = folderPair(in.Path, in.Roots)
	}
	switch {
	case a == "" || b == "":
		return "", ""
	case a == b:
		// es-es, spa-spa, "English-English", 英英: a pair that names the same
		// language twice is the standard way a monolingual dictionary is
		// labelled, and it is stated, not inferred.
		return a, "Monolingual " + lang.Name(a)
	}
	if a > b {
		a, b = b, a // the id is order-free: en-ru and ru-en are one group
	}
	na, nb := lang.Name(a), lang.Name(b)
	if strings.ToLower(nb) < strings.ToLower(na) {
		na, nb = nb, na
	}
	return a + "-" + b, na + " ↔ " + nb
}

// ArticleLang is the language the dictionary's ARTICLE BODIES are written in -
// the second half of a pair - as an ISO 639-1 code, or "" when nothing says.
// It is the default voice for reading a selection aloud; the client refines it
// per selection by script, choosing between this and the headword language,
// because a bilingual article quotes both.
//
// Evidence, first hit wins: the declared contents language; the second code
// of a pair in the title ("Oxford Russian-English"), then in the file name
// ("eng-eus", "en_fr"), then in an enclosing folder name up to the configured
// root. A dictionary with no pair anywhere is taken to be monolingual, so its
// headword language answers. Unlike a group label this value is never shown -
// it picks a voice, and the reader overrides a wrong one with a tap - which is
// why the monolingual assumption is acceptable here and not in Derive.
func ArticleLang(in Input) string {
	if c := lang.FromDeclared(in.Contents); c != "" {
		return c
	}
	if _, b := pair(in.Name); b != "" {
		return b
	}
	if _, b := pair(stem(in.Path)); b != "" {
		return b
	}
	if _, b := folderPair(in.Path, in.Roots); b != "" {
		return b
	}
	return lang.Resolve(in.Declared, in.Path, in.Roots, in.Name)
}

// folderPair is the pair the nearest enclosing folder is named as
// ("En-Ru/apresyan.dsl"). The walk stops at the configured root it is
// under, root included, exactly as the headword-language folder walk does; a
// path under no root has only its own folder read. Strings only - no disk.
func folderPair(path string, roots []string) (string, string) {
	if path == "" {
		return "", ""
	}
	dir := filepath.Clean(filepath.Dir(path))
	inside := false
	for _, r := range roots {
		if r == "" {
			continue
		}
		r = filepath.Clean(r)
		if dir == r || strings.HasPrefix(dir, r+string(filepath.Separator)) {
			inside = true
			break
		}
	}
	for {
		if a, b := pair(filepath.Base(dir)); b != "" {
			return a, b
		}
		if !inside || slices.ContainsFunc(roots, func(r string) bool {
			return r != "" && filepath.Clean(r) == dir
		}) {
			return "", ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
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
	return hanPair(s)
}

// hanPair reads the pair a Chinese or Japanese title writes with no separator
// at all: one character per language, "英汉" (English-Chinese), "汉英", "俄汉",
// "英和" (English-Japanese), "英英" (monolingual). Two characters that both
// name a language are not yet a pair - "语和汉语" would read as one - so the
// pair must be followed by the word the title is naming: 词典/辞典/字典/辞書,
// optionally sized (大/中/小), or 双解 ("bilingual explanations"). That is how
// "牛津高阶英汉双解词典" and "新英和中辞典" are written, and it is what keeps a
// sentence out.
//
// Only characters that are unambiguous in that position are in the table. 西
// (Spanish) is not: 西汉 is also the Western Han dynasty. 和 names Japanese
// only against 英, the one pairing it is used in; elsewhere it is "and".
var hanPairRe = regexp.MustCompile(`([英汉漢中俄日法德韩韓意葡拉和])([英汉漢中俄日法德韩韓意葡拉和])(?:[大中小]?(?:词典|詞典|辞典|辭典|字典|辞書|辭書)|双解|雙解)`)

var hanLang = map[string]string{
	"英": "en", "汉": "zh", "漢": "zh", "中": "zh", "俄": "ru", "日": "ja", "法": "fr",
	"德": "de", "韩": "ko", "韓": "ko", "意": "it", "葡": "pt", "拉": "la", "和": "ja",
}

func hanPair(s string) (string, string) {
	for _, m := range hanPairRe.FindAllStringSubmatch(s, -1) {
		if (m[1] == "和" || m[2] == "和") && m[1]+m[2] != "英和" && m[1]+m[2] != "和英" {
			continue
		}
		return hanLang[m[1]], hanLang[m[2]]
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
