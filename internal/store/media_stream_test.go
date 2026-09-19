// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"bytes"
	"io"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wuweidict/wudict/internal/dict"
)

// A packed resource over the streaming threshold must come back as a
// ReadSeeker that answers from the blob, chunk by chunk - not as five
// megabytes materialized in memory so that a Range request could serve a
// hundred KB of them.
func TestLargePackedResourceStreams(t *testing.T) {
	dir := t.TempDir()
	textDB := filepath.Join(dir, "text.db")
	r := &fakeReader{
		meta:    dict.Meta{Name: "M", Format: "mdx", Path: "/x.mdx"},
		entries: []dict.Entry{h("w", "body")},
	}
	if err := Ingest(r, textDB, nil); err != nil {
		t.Fatal(err)
	}
	uuid, _ := ReadMetaValue(textDB, "dict_uuid")

	big := make([]byte, blobStreamMin+1<<20)
	for i := range big {
		big[i] = byte(i*7 + i>>9) // deterministic, and not one repeated byte
	}
	small := []byte("tiny")
	mediaDB := filepath.Join(dir, "media.db")
	if err := IngestMedia(&assetDict{
		"big.bin":   string(big),
		"small.bin": string(small),
	}, []string{"big.bin", "small.bin"}, mediaDB, uuid, nil); err != nil {
		t.Fatal(err)
	}
	m, err := OpenMedia(mediaDB)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	rc, _, err := m.Resource("big.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		t.Fatal("a streamed resource lost its Seek - Range requests would break")
	}

	// Reading it whole arrives at the same bytes the source packed.
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("whole read = %d bytes, want %d identical", len(got), len(big))
	}

	// A seek into the middle - what a Range request does - reads the right
	// window and allocates a couple of chunks, not the blob.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := rs.Seek(3<<20, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	window := make([]byte, 1<<10)
	if _, err := io.ReadFull(rs, window); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if !bytes.Equal(window, big[3<<20:3<<20+1<<10]) {
		t.Fatal("mid-blob window does not match the packed bytes")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
		t.Errorf("serving a 1 KB window at offset 3 MiB allocated %d bytes", grew)
	}

	// SeekEnd anchors where a range from the tail anchors.
	if _, err := rs.Seek(-8, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail := make([]byte, 8)
	if _, err := io.ReadFull(rs, tail); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tail, big[len(big)-8:]) {
		t.Fatal("tail window does not match the packed bytes")
	}

	// Below the threshold nothing changes: the small resource is still a
	// whole-row read and still seekable.
	rc2, _, err := m.Resource("small.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.Close()
	got2, err := io.ReadAll(rc2)
	if err != nil || !bytes.Equal(got2, small) {
		t.Fatalf("small resource = %q err %v", got2, err)
	}
}
