// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeReceipt lays down a folder's info.txt and a text.db older than it -
// the state every ingest leaves behind - and returns the text.db path.
func writeReceipt(t *testing.T, dir, info string) string {
	t.Helper()
	textDB := TextDBPath(dir)
	if err := os.WriteFile(textDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(textDB, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(InfoPath(dir), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	return textDB
}

// TestReceiptMetaReadsACompleteReceipt: the library listing answers from the
// receipt alone - no SQLite - for any folder WriteInfo has regenerated.
func TestReceiptMetaReadsACompleteReceipt(t *testing.T) {
	dir := t.TempDir()
	info := "# wudict - prepared dictionary\n" +
		"name = AHD5\n" +
		"format = mdx\n" +
		"entries = 61991\n" +
		"contains = 1\n" +
		"index = full text (exact · prefix · contains · full-text)\n" +
		"source = D:\\dicts\\ahd5.mdx\n" +
		"imported = 2026-01-02T03:04:05Z\n"
	meta, ok := receiptMeta(dir, writeReceipt(t, dir, info))
	if !ok {
		t.Fatal("a complete receipt was rejected")
	}
	want := map[string]string{
		"name":         "AHD5",
		"format":       "mdx",
		"has_trigram":  "1",
		"ingest_level": string(LevelText),
		"source_path":  `D:\dicts\ahd5.mdx`,
		"created":      "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if meta[k] != v {
			t.Errorf("receipt %s = %q, want %q", k, meta[k], v)
		}
	}

	// The headwords level reads back as exactly the enum, so the listing's
	// FullText test keeps working off the receipt.
	dir = t.TempDir()
	info = "name = Old\nformat = slob\nentries = 12\ncontains = 0\n" +
		"index = headwords only (exact · prefix · contains)\n" +
		"source = /dicts/old.slob\nimported = 2026-02-03T04:05:06Z\n"
	if meta, ok = receiptMeta(dir, writeReceipt(t, dir, info)); !ok || meta["ingest_level"] != string(LevelHeadwords) ||
		meta["has_trigram"] != "0" {
		t.Fatalf("headwords receipt = %v ok %v", meta, ok)
	}
}

// TestReceiptMetaFallsBackOnMissingFields: a receipt from before `contains`
// was written - and no receipt at all - both report false, which is the
// caller's cue to read the meta table the slow way.
func TestReceiptMetaFallsBackOnMissingFields(t *testing.T) {
	dir := t.TempDir()
	info := "name = Old\nformat = slob\n" +
		"index = headwords only (exact · prefix · contains)\n" +
		"source = /dicts/old.slob\nimported = 2026-02-03T04:05:06Z\n"
	if _, ok := receiptMeta(dir, writeReceipt(t, dir, info)); ok {
		t.Fatal("a receipt without contains was accepted")
	}
	dir = t.TempDir()
	textDB := writeReceipt(t, dir, "")
	if err := os.Remove(InfoPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, ok := receiptMeta(dir, textDB); ok {
		t.Fatal("a missing receipt was accepted")
	}
}

// TestReceiptMetaRefusesWhatReadMetaWould: the receipt must never answer for a
// state the meta table would not. A text.db rewritten after its receipt (the
// best-effort WriteInfo failed) is read the slow way, and a folder whose text.db
// is gone is not listed at all - the receipt alone would enrol a database that
// cannot be opened.
func TestReceiptMetaRefusesWhatReadMetaWould(t *testing.T) {
	info := "name = X\nformat = mdx\nentries = 1\ncontains = 0\n" +
		"index = headwords only (exact · prefix · contains)\n" +
		"source = /d/x.mdx\nimported = 2026-02-03T04:05:06Z\n"

	dir := t.TempDir()
	textDB := writeReceipt(t, dir, info)
	now := time.Now().Add(time.Hour)
	if err := os.Chtimes(textDB, now, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := receiptMeta(dir, textDB); ok {
		t.Fatal("a receipt older than its text.db was trusted")
	}

	dir = t.TempDir()
	textDB = writeReceipt(t, dir, info)
	if err := os.Remove(textDB); err != nil {
		t.Fatal(err)
	}
	if _, ok := receiptMeta(dir, textDB); ok {
		t.Fatal("a receipt without its text.db was trusted")
	}
	if _, ok := receiptMeta(filepath.Join(t.TempDir(), "absent"), textDB); ok {
		t.Fatal("an absent folder was trusted")
	}
}
