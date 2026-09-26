// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package goldentest pins what a format's Reader, fed through IngestPlan,
// writes into a prepared text.db. A prepared library is frozen output of the
// code that built it; a behaviour change reaches it only through a rebuild,
// and only the version stamps (store/stale.go) say one is due. This test is
// what makes forgetting the stamp impossible: the content hash moved, so a
// version must move with it, and the failure names which one.
package goldentest

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/wuweidict/wudict/internal/artmark"
	"github.com/wuweidict/wudict/internal/dict"
	"github.com/wuweidict/wudict/internal/store"
)

// Golden is the recorded result: the versions in force when Hash was taken,
// and the store.Fingerprint of the fixture prepared with every index on.
type Golden struct {
	Versions string
	Hash     string
}

// Versions renders the stamps that together decide a format's prepared
// content, in a fixed order.
func Versions(format string) string {
	return fmt.Sprintf("reader=%d ingest=%d markup=%d fold=%d",
		dict.ReaderVersion(format), store.IngestVersion, artmark.Version, dict.FoldVersion)
}

// Check prepares src (full text and contains both on, so every index is
// hashed) into a temporary text.db and compares its fingerprint and the
// current versions with want. The fixture must be built in code, byte for
// byte the same on every run.
func Check(t *testing.T, format, src string, want Golden) {
	t.Helper()
	t.Setenv("WUDICT_DB_DIR", t.TempDir())
	r, err := dict.OpenReader(src)
	if err != nil {
		t.Fatalf("OpenReader(%s): %v", src, err)
	}
	dbPath := filepath.Join(t.TempDir(), store.TextDBName)
	_, err = store.IngestPlan(r, dbPath, store.Plan{FullText: true, Contains: true}, nil)
	r.Close()
	if err != nil {
		t.Fatalf("IngestPlan: %v", err)
	}
	hash, err := store.Fingerprint(dbPath)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	got := Golden{Versions: Versions(format), Hash: hash}
	if got == want {
		return
	}
	update := fmt.Sprintf("goldentest.Golden{Versions: %q, Hash: %q}", got.Versions, got.Hash)
	switch {
	case got.Hash != want.Hash && got.Versions == want.Versions:
		t.Fatalf("%s: prepared content changed but no version was bumped.\n"+
			"An existing library would keep the old content and never be told to rebuild.\n"+
			"Bump the version of the layer you changed - %s.ReaderVersion (the Reader), "+
			"store.IngestVersion (IngestPlan), artmark.Version (article markup) or "+
			"dict.FoldVersion (folding) - then rerun and set the golden to the value it prints.\n"+
			"hash %s → %s", format, format, want.Hash, got.Hash)
	case got.Hash != want.Hash:
		t.Fatalf("%s: prepared content changed with versions %q → %q; update the golden:\n\t%s",
			format, want.Versions, got.Versions, update)
	default:
		t.Fatalf("%s: a version moved (%q → %q) but the content did not; update the golden:\n\t%s\n"+
			"(if nothing of %s's output was meant to change, the bump is unnecessary and "+
			"will ask every library to rebuild for nothing)", format, want.Versions, got.Versions, update, format)
	}
}
