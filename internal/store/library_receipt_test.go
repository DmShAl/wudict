// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if err := os.WriteFile(InfoPath(dir), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, ok := receiptMeta(dir)
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
	if err := os.WriteFile(InfoPath(dir), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	if meta, ok = receiptMeta(dir); !ok || meta["ingest_level"] != string(LevelHeadwords) ||
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
	if err := os.WriteFile(InfoPath(dir), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := receiptMeta(dir); ok {
		t.Fatal("a receipt without contains was accepted")
	}
	if _, ok := receiptMeta(filepath.Join(t.TempDir(), "no-receipt")); ok {
		t.Fatal("a missing receipt was accepted")
	}
}
