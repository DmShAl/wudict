// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wuweidict/wudict/internal/store"
)

// A flag given sets its half of the plan; a flag left out keeps what the
// database already has, and a dictionary never prepared gets the default.
func TestPlanFlags(t *testing.T) {
	yes, no := true, false
	headwords := &store.Plan{}
	both := &store.Plan{FullText: true, Contains: true}
	for _, tc := range []struct {
		name string
		pf   planFlags
		have *store.Plan
		want store.Plan
	}{
		{"never prepared, no flags", planFlags{}, nil, store.Plan{FullText: true}},
		{"never prepared, -headwords -contains", planFlags{&no, &yes}, nil, store.Plan{Contains: true}},
		{"no flags keep headwords only", planFlags{}, headwords, store.Plan{}},
		{"no flags keep full text and contains", planFlags{}, both, *both},
		{"-contains adds to what is there", planFlags{contains: &yes}, headwords, store.Plan{Contains: true}},
		{"-contains=false drops only contains", planFlags{contains: &no}, both, store.Plan{FullText: true}},
		{"-headwords drops only full text", planFlags{fullText: &no}, both, store.Plan{Contains: true}},
		{"-headwords=false adds full text", planFlags{fullText: &yes}, headwords, store.Plan{FullText: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pf.plan(tc.have); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// `wudict ingest` with no path prepares the configured DICT_DIR, and a run
// over the whole library with no flags changes no dictionary's plan.
func TestIngestConfiguredLibrary(t *testing.T) {
	home, srcDir, dbDir := t.TempDir(), t.TempDir(), t.TempDir()
	// isolate from the user's own wudict.toml and library: the environment
	// outranks any file config.Load could find
	t.Setenv("HOME", home)
	t.Setenv("CONFIG_PATH", "")
	t.Setenv("DB_DIR", dbDir)
	t.Setenv("WUDICT_DB_DIR", dbDir)
	t.Setenv("DICT_DIR", srcDir)

	src := func(name string) string {
		p := filepath.Join(srcDir, name+".dsl")
		body := "#NAME \"" + name + "\"\n\ncasa\n\tvivienda\n"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	withContains, headwordsOnly := src("WithContains"), src("HeadwordsOnly")
	if err := cmdIngest([]string{"-contains", withContains}); err != nil {
		t.Fatal(err)
	}
	if err := cmdIngest([]string{"-headwords", headwordsOnly}); err != nil {
		t.Fatal(err)
	}
	fresh := src("Fresh")

	textDB := func(p string) string {
		t.Helper()
		dir, ok := store.LookupDir(p)
		if !ok {
			t.Fatalf("%s is not prepared", filepath.Base(p))
		}
		return store.TextDBPath(dir)
	}
	uuid := func(p string) string {
		t.Helper()
		v, err := store.ReadMetaValue(textDB(p), "dict_uuid")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := map[string]string{withContains: uuid(withContains), headwordsOnly: uuid(headwordsOnly)}

	if err := cmdIngest(nil); err != nil {
		t.Fatalf("no path: %v", err)
	}
	for _, tc := range []struct {
		path string
		want store.Plan
	}{
		{withContains, store.Plan{FullText: true, Contains: true}},
		{headwordsOnly, store.Plan{}},
		{fresh, store.Plan{FullText: true}},
	} {
		if got := store.KeptPlan(textDB(tc.path)); got != tc.want {
			t.Errorf("%s: plan %+v, want %+v", filepath.Base(tc.path), got, tc.want)
		}
	}
	for p, u := range before {
		if uuid(p) != u {
			t.Errorf("%s: a current dictionary was rebuilt", filepath.Base(p))
		}
	}

	if err := cmdIngest([]string{"-contains=false"}); err != nil {
		t.Fatal(err)
	}
	if got := store.KeptPlan(textDB(withContains)); got != (store.Plan{FullText: true}) {
		t.Errorf("-contains=false: %+v, want full text kept and contains dropped", got)
	}
	if got := store.KeptPlan(textDB(headwordsOnly)); got != (store.Plan{}) {
		t.Errorf("-contains=false: %+v, want headwords only kept", got)
	}

	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"-o without a file", []string{"-o", filepath.Join(home, "x.db")}, "-o needs"},
		{"-o with a folder", []string{"-o", filepath.Join(home, "x.db"), srcDir}, "-o names"},
		{"-o with two files", []string{"-o", filepath.Join(home, "x.db"), withContains, fresh}, "-o names"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := cmdIngest(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}
