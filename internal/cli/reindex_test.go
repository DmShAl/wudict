// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wuweidict/wudict/internal/dict"
	"github.com/wuweidict/wudict/internal/store"
)

type oneEntryReader struct {
	meta dict.Meta
	done bool
}

func (r *oneEntryReader) Meta() dict.Meta { return r.meta }
func (r *oneEntryReader) Close() error    { return nil }
func (r *oneEntryReader) Next() (dict.Entry, error) {
	if r.done {
		return dict.Entry{}, io.EOF
	}
	r.done = true
	return dict.Entry{Headwords: []string{"a"}, Body: "<p>x</p>", Kind: dict.BodyHTML}, nil
}

// prepareFolder claims a library folder for a stand-in source and builds its
// text.db, returning the folder.
func prepareFolder(t *testing.T, src, name string) string {
	t.Helper()
	if err := os.WriteFile(src, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.ClaimDir(src)
	if err != nil {
		t.Fatal(err)
	}
	r := &oneEntryReader{meta: dict.Meta{Name: name, Format: "mdx", Path: src}}
	if _, err := store.IngestPlan(r, store.TextDBPath(dir), store.Plan{}, nil); err != nil {
		t.Fatal(err)
	}
	return dir
}

// reindex names a dictionary by everything `wudict rm` accepts, and reaches a
// folder whose text.db this build cannot read by its folder name or path -
// the case rm's library listing cannot see and a rebuild exists for.
func TestReindexTargets(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	srcDir := t.TempDir()
	goodSrc := filepath.Join(srcDir, "good.mdx")
	brokenSrc := filepath.Join(srcDir, "broken.mdx")
	good := prepareFolder(t, goodSrc, "Good")
	broken := prepareFolder(t, brokenSrc, "Broken")
	if err := os.WriteFile(store.TextDBPath(broken), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	unprepared := filepath.Join(srcDir, "plain.mdx")
	if err := os.WriteFile(unprepared, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		args    []string
		want    []string // folders, in order
		wantErr string
	}{
		{"no arguments is every folder", nil, []string{broken, good}, ""},
		{"by dictionary name", []string{"Good"}, []string{good}, ""},
		{"by folder name", []string{filepath.Base(good)}, []string{good}, ""},
		{"by source path", []string{goodSrc}, []string{good}, ""},
		{"by folder path", []string{good}, []string{good}, ""},
		{"unreadable, by folder name", []string{filepath.Base(broken)}, []string{broken}, ""},
		{"unreadable, by text.db path", []string{store.TextDBPath(broken)}, []string{broken}, ""},
		{"unreadable, by source path", []string{brokenSrc}, []string{broken}, ""},
		{"named twice, rebuilt once", []string{"Good", goodSrc}, []string{good}, ""},
		{"a source never prepared", []string{unprepared}, nil, "not prepared"},
		{"no such dictionary", []string{"nope"}, nil, "no dictionary named"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reindexTargets(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var dirs []string
			for _, f := range got {
				dirs = append(dirs, f.Dir)
			}
			if strings.Join(dirs, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %v, want %v", dirs, tc.want)
			}
		})
	}
}

// A rebuild keeps the plan; -all rebuilds a current dictionary too.
func TestReindexOneKeepsPlan(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	src := filepath.Join(t.TempDir(), "d.mdx")
	dir := prepareFolder(t, src, "D")
	textDB := store.TextDBPath(dir)
	r := &oneEntryReader{meta: dict.Meta{Name: "D", Format: "mdx", Path: src}}
	want := store.Plan{FullText: true, Contains: true}
	if _, err := store.IngestPlan(r, textDB, want, nil); err != nil {
		t.Fatal(err)
	}
	uuid, _ := store.ReadMetaValue(textDB, "dict_uuid")
	// .mdx needs a real file to read; with force and an unreadable stand-in the
	// rebuild must fail and leave the prepared data as it was
	if err := reindexOne(store.Folder{Dir: dir, Source: src}, true); err == nil {
		t.Fatal("rebuilding from an unreadable source succeeded")
	}
	if got := store.KeptPlan(textDB); got != want {
		t.Errorf("after a failed rebuild: plan %+v, want %+v", got, want)
	}
	if u, _ := store.ReadMetaValue(textDB, "dict_uuid"); u != uuid {
		t.Error("a failed rebuild replaced the database")
	}
	// current and not forced: nothing is opened, nothing fails
	if err := reindexOne(store.Folder{Dir: dir, Source: src}, false); err != nil {
		t.Errorf("a current dictionary without -all: %v", err)
	}
}
