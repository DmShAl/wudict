// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package facet

import "testing"

// get returns the value id a dictionary got in one facet, or "" for none.
func get(gs []Group, facet string) string {
	for _, g := range gs {
		if g.F == facet {
			return g.V
		}
	}
	return ""
}

func all(gs []Group, facet string) []string {
	var out []string
	for _, g := range gs {
		if g.F == facet {
			out = append(out, g.V)
		}
	}
	return out
}

func TestDeriveLanguage(t *testing.T) {
	tests := []struct {
		name  string
		in    Input
		want  string
		wantP string
	}{
		{"declared wins", Input{Declared: "SpanishModernSort", Name: "Oxford Russian", Path: "/d/fr-collins.mdx"}, "es", ""},
		{"file stem", Input{Name: "Apresyan", Path: "/d/en-ru-apresyan.mdx"}, "en", "en-ru"},
		{"title last", Input{Name: "Dahl's Russian Dictionary", Path: "/d/dahl.dsl"}, "ru", ""},
		{"nothing is nothing", Input{Name: "Kolokviumo", Path: "/d/kolokviumo.mdx"}, "", ""},
		{"never english by default", Input{Name: "Unlabelled", Path: "/d/unlabelled.mdx"}, "", ""},
		{"monolingual pair in title", Input{Name: "Hagen's paradigm (Ru-Ru)", Path: "/d/hagen.dsl"}, "ru", "ru"},
		{"monolingual pair in stem", Input{Name: "DRAE", Path: "/d/es-es-drae.mdx"}, "es", "es"},
		{"declared pair", Input{Declared: "English", Contents: "Russian", Name: "Anon", Path: "/d/anon.dsl"}, "en", "en-ru"},
		{"declared monolingual pair", Input{Declared: "English", Contents: "English", Name: "Anon", Path: "/d/anon.dsl"}, "en", "en"},
		{"full names in title", Input{Name: "Larousse Compact English-Spanish", Path: "/d/larousse.mdx"}, "en", "en-es"},
		{"reverse direction is the same pair", Input{Name: "Oxford Russian-English", Path: "/d/ox.mdx"}, "", "en-ru"},
		{"folder pair", Input{Name: "Apresyan", Path: "/d/En-Ru/apresyan.dsl", Roots: []string{"/d"}}, "", "en-ru"},
		{"hyphen that is not a pair", Input{Name: "Anglo-Saxon Wordhoard", Path: "/d/as.mdx"}, "", ""},
		{"one hint is not a pair", Input{Name: "Oxford English Dictionary", Path: "/d/oed.mdx"}, "en", ""},
		{"han pair", Input{Name: "《牛津高阶英汉双解词典OALD》", Path: "/d/oald.mdx"}, "", "en-zh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := Derive(tc.in)
			// lang is Resolve's business and has its own tests; "" here means
			// the case is about the pair and does not pin the language.
			if got := get(gs, "lang"); tc.want != "" && got != tc.want {
				t.Errorf("lang = %q, want %q", got, tc.want)
			}
			if got := get(gs, "pair"); got != tc.wantP {
				t.Errorf("pair = %q, want %q", got, tc.wantP)
			}
		})
	}
}

// One label per pair whichever way round the dictionary is written, so the two
// directions count as one group in the picker.
func TestPairLabel(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"English-Russian", "English ↔ Russian"},
		{"Russian-English", "English ↔ Russian"},
		{"German-English", "English ↔ German"},
		{"es-es", "Monolingual Spanish"},
		{"英英词典", "Monolingual English"},
	} {
		v, l := languagePair(Input{Name: tc.name})
		if v == "" || l != tc.want {
			t.Errorf("%q: label %q (id %q), want %q", tc.name, l, v, tc.want)
		}
	}
}

func TestHanPair(t *testing.T) {
	for _, tc := range []struct{ s, a, b string }{
		{"牛津高阶英汉双解词典", "en", "zh"},
		{"汉英大词典", "zh", "en"},
		{"新英和中辞典", "en", "ja"},
		{"研究社和英大辞典", "ja", "en"},
		{"俄汉详解大词典", "", ""}, // 详解 is not the word a pair is followed by
		{"俄汉大词典", "ru", "zh"},
		{"英语和汉语词典", "", ""}, // 和 is "and" here
		{"西汉词典", "", ""},    // Western Han, not Spanish
		{"现代汉语词典", "", ""},
		{"", "", ""},
	} {
		if a, b := hanPair(tc.s); a != tc.a || b != tc.b {
			t.Errorf("hanPair(%q) = %q,%q; want %q,%q", tc.s, a, b, tc.a, tc.b)
		}
	}
}

func TestDeriveKind(t *testing.T) {
	tests := []struct {
		title string
		want  []string
	}{
		{"Encyclopaedia Britannica", []string{"encyclopedia"}},
		{"Wikipedia (English)", []string{"encyclopedia"}},
		{"Roget's Thesaurus of Synonyms", []string{"thesaurus"}},
		{"Oxford Dictionary of Idioms", []string{"idioms"}},
		{"Dictionary of American Slang", []string{"slang"}},
		{"Online Etymology Dictionary", []string{"etymology"}},
		{"Black's Law Dictionary", []string{"legal"}},
		{"Stedman's Medical Dictionary", []string{"medical"}},
		{"Dictionary of Abbreviations and Acronyms", []string{"abbrev"}},
		{"Longman Dictionary of Contemporary English", nil},
		{"Malawi Gazetteer", nil}, // "law" must not fire inside a word
		{"The Lawyer's Companion", nil},
		{"Concise Oxford English Dictionary", nil},
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			got := all(Derive(Input{Name: tc.title, Path: "/d/x.mdx"}), "kind")
			if len(got) != len(tc.want) {
				t.Fatalf("kind = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("kind = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestDerivePublisher(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Oxford Advanced Learner's Dictionary", "oxford"},
		{"OALD 9", "oxford"},
		{"CALD 4", "cambridge"},
		{"Collins COBUILD Advanced", "collins"},
		{"LDOCE 6", "longman"},
		{"Merriam-Webster's Collegiate Dictionary", "webster"},
		{"Webster's Revised Unabridged Dictionary (1913)", "webster"},
		{"The American Heritage Dictionary", "ahd"},
		{"Duden - Das große Wörterbuch", "duden"},
		{"Le Robert Micro", "robert"},
		{"Random House Webster's Unabridged", "webster"}, // first table hit wins
		{"Multitran", ""},
		{"Oxfordshire Place Names", ""}, // whole words only
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			if got := get(Derive(Input{Name: tc.title, Path: "/d/x.mdx"}), "pub"); got != tc.want {
				t.Errorf("pub = %q, want %q", got, tc.want)
			}
		})
	}
}

// A group's labels must never be empty: the picker renders them verbatim, and
// a blank option is a dead row a user can select.
func TestLabelsPresent(t *testing.T) {
	for _, in := range []Input{
		{Name: "Oxford Russian-English Encyclopedia", Path: "/d/oxford.mdx"},
		{Name: "DRAE", Path: "/d/es-es-drae.mdx"},
	} {
		for _, g := range Derive(in) {
			if g.F == "" || g.FL == "" || g.V == "" || g.VL == "" || g.FO == 0 {
				t.Errorf("%q: incomplete group %+v", in.Name, g)
			}
		}
	}
}

// Facet order is what the picker lists the groups in; it must be ascending
// however many groups a dictionary happens to hold.
func TestFacetOrderAscending(t *testing.T) {
	gs := Derive(Input{Name: "Oxford Russian-English Encyclopedia", Path: "/d/oxford.mdx"})
	if len(gs) != 4 {
		t.Fatalf("want all four facets, got %+v", gs)
	}
	for i := 1; i < len(gs); i++ {
		if gs[i].FO < gs[i-1].FO {
			t.Fatalf("facet order not ascending: %+v", gs)
		}
	}
}

func TestArticleLang(t *testing.T) {
	roots := []string{"/d"}
	tests := []struct {
		name string
		in   Input
		want string
	}{
		{"declared contents wins", Input{Contents: "Russian", Name: "English-French", Path: "/d/en-de.dsl"}, "ru"},
		{"title pair", Input{Name: "Oxford Russian-English", Path: "/d/en-fr.mdx"}, "en"},
		{"file pair, 2-letter", Input{Name: "Collins", Path: "/d/en-fr-collins.mdx"}, "fr"},
		{"file pair, 3-letter", Input{Name: "Elhuyar", Path: "/d/eng-eus.mdx"}, "eu"},
		{"file pair, underscore", Input{Name: "Apresyan", Path: "/d/eng_rus_apresyan.dsl.dz"}, "ru"},
		{"folder pair", Input{Name: "Apresyan", Path: "/d/En-Ru/apresyan.dsl"}, "ru"},
		{"nearest folder pair", Input{Name: "X", Path: "/d/en-fr/de-es/x.mdx"}, "es"},
		{"folder pair not above root", Input{Name: "X", Path: "/en-ru/d/x.mdx", Roots: []string{"/en-ru/d"}}, ""},
		{"outside roots: own folder only", Input{Name: "X", Path: "/en-ru/other/x.mdx"}, ""},
		{"monolingual: headword language", Input{Name: "Oxford English Dictionary", Path: "/d/oed.mdx"}, "en"},
		{"monolingual: declared headword", Input{Declared: "Spanish", Name: "DRAE", Path: "/d/drae.mdx"}, "es"},
		{"monolingual pair", Input{Name: "DRAE", Path: "/d/es-es-drae.mdx"}, "es"},
		{"hyphen that is not a pair", Input{Name: "Anglo-Saxon Wordhoard", Path: "/d/as.mdx"}, ""},
		{"nothing is nothing", Input{Name: "Kolokviumo", Path: "/d/kolokviumo.mdx"}, ""},
		{"han pair", Input{Name: "牛津高阶英汉双解词典", Path: "/d/oald.mdx"}, "zh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.Roots == nil {
				tc.in.Roots = roots
			}
			if got := ArticleLang(tc.in); got != tc.want {
				t.Errorf("ArticleLang = %q, want %q", got, tc.want)
			}
		})
	}
}
