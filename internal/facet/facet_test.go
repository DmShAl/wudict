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
		wantD string
	}{
		{"declared wins", Input{Declared: "SpanishModernSort", Name: "Oxford Russian", Path: "/d/fr-collins.mdx"}, "es", ""},
		{"file stem", Input{Name: "Apresyan", Path: "/d/en-ru-apresyan.mdx"}, "en", "bi"},
		{"title last", Input{Name: "Dahl's Russian Dictionary", Path: "/d/dahl.dsl"}, "ru", ""},
		{"nothing is nothing", Input{Name: "Kolokviumo", Path: "/d/kolokviumo.mdx"}, "", ""},
		{"never english by default", Input{Name: "Unlabelled", Path: "/d/unlabelled.mdx"}, "", ""},
		{"monolingual pair in title", Input{Name: "Hagen's paradigm (Ru-Ru)", Path: "/d/hagen.dsl"}, "ru", "mono"},
		{"monolingual pair in stem", Input{Name: "DRAE", Path: "/d/es-es-drae.mdx"}, "es", "mono"},
		{"declared pair", Input{Declared: "English", Contents: "Russian", Name: "Anon", Path: "/d/anon.dsl"}, "en", "bi"},
		{"declared monolingual pair", Input{Declared: "English", Contents: "English", Name: "Anon", Path: "/d/anon.dsl"}, "en", "mono"},
		{"full names in title", Input{Name: "Larousse Compact English-Spanish", Path: "/d/larousse.mdx"}, "en", "bi"},
		{"hyphen that is not a pair", Input{Name: "Anglo-Saxon Wordhoard", Path: "/d/as.mdx"}, "", ""},
		{"one hint is not a direction", Input{Name: "Oxford English Dictionary", Path: "/d/oed.mdx"}, "en", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := Derive(tc.in)
			if got := get(gs, "lang"); got != tc.want {
				t.Errorf("lang = %q, want %q", got, tc.want)
			}
			if got := get(gs, "dir"); got != tc.wantD {
				t.Errorf("dir = %q, want %q", got, tc.wantD)
			}
		})
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
		{"Malawi Gazetteer", nil},  // "law" must not fire inside a word
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
