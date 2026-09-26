// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wuweidict/wudict/internal/dict"
)

// setMeta overwrites (or, with v == "", deletes) one meta key in place, the
// way an older build would have left it.
func setMeta(t *testing.T, dbPath, k, v string) {
	t.Helper()
	db, err := sql.Open(driverName, dsnIngest(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v == "" {
		_, err = db.Exec("DELETE FROM meta WHERE key = ?", k)
	} else {
		_, err = db.Exec("INSERT OR REPLACE INTO meta(key, value) VALUES(?, ?)", k, v)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func ingestSrc(t *testing.T, src, dbPath string, plan Plan) {
	t.Helper()
	r := &fakeReader{
		meta:    dict.Meta{Name: "S", Format: "mdx", Path: src},
		entries: []dict.Entry{h("alpha", "<p>one two</p>"), h("beta", "<p>three</p>")},
	}
	if _, err := IngestPlan(r, dbPath, plan, nil); err != nil {
		t.Fatal(err)
	}
}

// A freshly built database is current; each stamp an older build would have
// left behind is reported under its own reason, and a missing stamp reads as
// version 1 - so a library prepared before the stamps existed is current.
func TestStaleReasons(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	src := writeSrc(t, filepath.Join(t.TempDir(), "s.mdx"), "source bytes")
	dbPath := filepath.Join(t.TempDir(), TextDBName)
	ingestSrc(t, src, dbPath, Plan{})

	if got := Stale(dbPath, src); len(got) != 0 {
		t.Fatalf("fresh build: Stale = %v, want none", got)
	}
	for _, k := range []string{"ingest_version", "reader_version"} {
		setMeta(t, dbPath, k, "")
	}
	if got := Stale(dbPath, src); len(got) != 0 {
		t.Fatalf("unstamped (pre-versioning) build: Stale = %v, want none", got)
	}
	setMeta(t, dbPath, "ingest_version", "0")
	if got := Stale(dbPath, src); !reflect.DeepEqual(got, []Reason{ReasonIngest}) {
		t.Errorf("older ingest: Stale = %v", got)
	}
	// a NEWER build's library is not improved by this one rebuilding it
	setMeta(t, dbPath, "ingest_version", "99")
	if got := Stale(dbPath, src); len(got) != 0 {
		t.Errorf("newer ingest: Stale = %v, want none", got)
	}

	writeSrc(t, src, "different source bytes")
	future := time.Now().Add(3 * time.Second)
	_ = os.Chtimes(src, future, future)
	if got := Stale(dbPath, src); !reflect.DeepEqual(got, []Reason{ReasonSource}) {
		t.Errorf("edited source: Stale = %v", got)
	}
	// without the source only the version reasons can be judged
	if got := Stale(dbPath, ""); len(got) != 0 {
		t.Errorf("no source: Stale = %v", got)
	}

	if err := os.WriteFile(dbPath, []byte("not a database at all, just bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Stale(dbPath, src); !reflect.DeepEqual(got, []Reason{ReasonSchema}) {
		t.Errorf("garbage file: Stale = %v", got)
	}
	if got := Stale(filepath.Join(t.TempDir(), "absent.db"), src); !reflect.DeepEqual(got, []Reason{ReasonSchema}) {
		t.Errorf("absent file: Stale = %v", got)
	}
}

// A reader version is judged only for a format that registered one.
func TestOutdatedReaderVersion(t *testing.T) {
	dict.RegisterReaderVersion("stale-test-fmt", 3)
	cases := []struct {
		meta map[string]string
		want []Reason
	}{
		{map[string]string{"format": "stale-test-fmt", "reader_version": "3"}, nil},
		{map[string]string{"format": "stale-test-fmt", "reader_version": "2"}, []Reason{ReasonReader}},
		{map[string]string{"format": "stale-test-fmt"}, []Reason{ReasonReader}}, // unstamped = 1
		{map[string]string{"format": "unregistered-fmt", "reader_version": "1"}, nil},
	}
	for _, c := range cases {
		if got := Outdated(c.meta); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Outdated(%v) = %v, want %v", c.meta, got, c.want)
		}
	}
}

// A rebuild from the same source keeps the dictionary's identity, so the
// media.db beside it stays paired; a rebuild from a changed source does not.
func TestRebuildKeepsUUIDAndMedia(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	src := writeSrc(t, filepath.Join(t.TempDir(), "s.mdx"), "source bytes")
	dir := t.TempDir()
	textDB := filepath.Join(dir, TextDBName)
	ingestSrc(t, src, textDB, Plan{FullText: true, Contains: true})
	uuid, _ := ReadMetaValue(textDB, "dict_uuid")
	if err := IngestMedia(&mediaSrc{}, []string{"a.png"}, filepath.Join(dir, MediaDBName), uuid, nil); err != nil {
		t.Fatal(err)
	}
	if !MediaPaired(textDB) {
		t.Fatal("freshly packed media not paired")
	}
	if got := KeptPlan(textDB); got != (Plan{FullText: true, Contains: true}) {
		t.Errorf("KeptPlan = %+v", got)
	}

	ingestSrc(t, src, textDB, KeptPlan(textDB))
	if u, _ := ReadMetaValue(textDB, "dict_uuid"); u != uuid {
		t.Errorf("rebuild from the same source changed dict_uuid %s → %s", uuid, u)
	}
	if !MediaPaired(textDB) || len(Stale(textDB, src)) != 0 {
		t.Errorf("same-source rebuild: paired=%v stale=%v", MediaPaired(textDB), Stale(textDB, src))
	}

	writeSrc(t, src, "different source bytes")
	future := time.Now().Add(3 * time.Second)
	_ = os.Chtimes(src, future, future)
	ingestSrc(t, src, textDB, KeptPlan(textDB))
	if u, _ := ReadMetaValue(textDB, "dict_uuid"); u == uuid {
		t.Error("rebuild from a changed source kept the old identity")
	}
	if MediaPaired(textDB) {
		t.Error("media packed from the old source still counts as paired")
	}
	if got := Stale(textDB, src); !reflect.DeepEqual(got, []Reason{ReasonMedia}) {
		t.Errorf("after changed-source rebuild: Stale = %v, want [media]", got)
	}
}

// PreparedFor refuses a database this build cannot open, so the caller
// rebuilds it rather than falling back to the source's format for good.
func TestPreparedForRejectsUnusable(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	src := writeSrc(t, filepath.Join(t.TempDir(), "d.mdx"), "content")
	dir := prepare(t, src, "D")
	if _, ok := PreparedFor(src); !ok {
		t.Fatal("fresh preparation not found")
	}
	db, err := sql.Open(driverName, dsnIngest(TextDBPath(dir)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, ok := PreparedFor(src); ok {
		t.Error("another schema counted as prepared")
	}
	if err := os.WriteFile(TextDBPath(dir), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := PreparedFor(src); ok {
		t.Error("an unreadable text.db counted as prepared")
	}
	if got := KeptPlan(TextDBPath(dir)); got != (Plan{}) {
		t.Errorf("KeptPlan of an unreadable db = %+v, want the default", got)
	}
	// the folder is still this source's: a rebuild lands in it
	if tgt, err := PrepareTarget(src); err != nil || tgt != TextDBPath(dir) {
		t.Errorf("PrepareTarget = %q, %v; want %q", tgt, err, TextDBPath(dir))
	}
}

// The fingerprint is the logical content: the same source built twice hashes
// the same, whatever uuid, timestamp or location it was given; different
// content or a different plan hashes differently.
func TestFingerprint(t *testing.T) {
	build := func(src string, plan Plan, entries []dict.Entry) string {
		t.Helper()
		dbPath := filepath.Join(t.TempDir(), TextDBName)
		r := &fakeReader{meta: dict.Meta{Name: "F", Format: "mdx", Path: src}, entries: entries}
		if _, err := IngestPlan(r, dbPath, plan, nil); err != nil {
			t.Fatal(err)
		}
		fp, err := Fingerprint(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		return fp
	}
	base := []dict.Entry{
		h("alpha", "<p>one <b>two</b></p>"),
		{Headwords: []string{"beta", "bet"}, Body: "<p>three</p>", Kind: dict.BodyHTML},
		{Headwords: []string{"gamma"}, LinkTo: "alpha"},
	}
	full := Plan{FullText: true, Contains: true}
	a := build("/one/place/x.mdx", full, base)
	if b := build("/another/place/x.mdx", full, base); a != b {
		t.Error("same content, different location/uuid/time: fingerprints differ")
	}
	changed := append([]dict.Entry(nil), base...)
	changed[0] = h("alpha", "<p>one <b>TWO</b></p>")
	if a == build("/one/place/x.mdx", full, changed) {
		t.Error("a changed article left the fingerprint unchanged")
	}
	if a == build("/one/place/x.mdx", Plan{FullText: true}, base) {
		t.Error("dropping the trigram index left the fingerprint unchanged")
	}
	if a == build("/one/place/x.mdx", Plan{Contains: true}, base) {
		t.Error("dropping full text left the fingerprint unchanged")
	}
}

// Folders lists every library folder holding a text.db - an unreadable one
// included, since that is the one most in need of a rebuild - with the source
// its claim names; a folder without a text.db and a stray file are not listed.
func TestFolders(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	if got, err := Folders(); err != nil || len(got) != 0 {
		t.Fatalf("empty library: %v, %v", got, err)
	}
	srcDir := t.TempDir()
	good := writeSrc(t, filepath.Join(srcDir, "a.mdx"), "a")
	broken := writeSrc(t, filepath.Join(srcDir, "b.mdx"), "b")
	goodDir := prepare(t, good, "A")
	brokenDir := prepare(t, broken, "B")
	if err := os.WriteFile(TextDBPath(brokenDir), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(DefaultDBDir(), "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(DefaultDBDir(), "stray.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Folders()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{goodDir: good, brokenDir: broken}
	if len(got) != len(want) {
		t.Fatalf("Folders = %+v, want %d folders", got, len(want))
	}
	for _, f := range got {
		src, ok := want[f.Dir]
		if !ok {
			t.Errorf("unexpected folder %q", f.Dir)
			continue
		}
		if !sameSource(f.Source, src) {
			t.Errorf("%s: Source = %q, want %q", filepath.Base(f.Dir), f.Source, src)
		}
	}
	if len(Stale(TextDBPath(brokenDir), broken)) == 0 {
		t.Error("an unreadable text.db must be stale")
	}
}

// TextStale is Stale without the media reason: an unpaired media.db is the
// pack's business, not a reason to rebuild the text.
func TestTextStaleIgnoresMedia(t *testing.T) {
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	src := writeSrc(t, filepath.Join(t.TempDir(), "s.mdx"), "source bytes")
	dir := t.TempDir()
	textDB := filepath.Join(dir, TextDBName)
	ingestSrc(t, src, textDB, Plan{})
	if err := IngestMedia(&mediaSrc{}, []string{"a.png"}, filepath.Join(dir, MediaDBName), "another-uuid", nil); err != nil {
		t.Fatal(err)
	}
	if got := Stale(textDB, src); !reflect.DeepEqual(got, []Reason{ReasonMedia}) {
		t.Fatalf("Stale = %v, want [media]", got)
	}
	if got := TextStale(textDB, src); len(got) != 0 {
		t.Errorf("TextStale = %v, want none", got)
	}
}

// One source file under two spellings is one source: a path through a
// symlinked folder (what discovery resolves, and a typed path does not) must
// find the folder already prepared, not claim a second one.
func TestSameSourceThroughSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	src := filepath.Join(target, "a.dsl")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(target, "b.dsl")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		a, b string
		want bool
	}{
		{"same spelling", src, src, true},
		{"through the link", src, filepath.Join(link, "a.dsl"), true},
		{"another file", src, other, false},
		{"another file through the link", src, filepath.Join(link, "b.dsl"), false},
		{"a missing file", src, filepath.Join(link, "gone.dsl"), false},
		{"empty", src, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameSource(tc.a, tc.b); got != tc.want {
				t.Errorf("sameSource(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
