// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package stardict

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

// readerAtThatObjects records any ReadAt as a failure: readRange's bound
// checks exist so a corrupt .idx never reaches the chunk loop, and the only
// way to see that from a test is to make inflation itself loud.
type readerAtThatObjects struct {
	t *testing.T
}

func (r readerAtThatObjects) ReadAt([]byte, int64) (int, error) {
	r.t.Error("readRange inflated a chunk for a range that should have been refused")
	return 0, io.ErrUnexpectedEOF
}

// TestReadRangeRejectsRangesPastTheExtent: (offset,size) comes out of the
// .idx, and a size reaching past the chunk table's extent names bytes the
// file cannot contain. Before the extent check, an oversized size sent the
// chunk loop inflating every chunk to the end of the file into one buffer.
func TestReadRangeRejectsRangesPastTheExtent(t *testing.T) {
	raw := makeDictzip(t, []byte("abcdefgh"), 4) // two chunks of four
	d, err := newDzReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	// The loud reader goes in before the refusals: an extent check that
	// somehow let a range through would try to inflate, and be caught here.
	d.ra = readerAtThatObjects{t}
	for _, r := range [][2]int64{{0, 1 << 40}, {6, 1 << 20}, {8, 1}} {
		if _, err := d.readRange(r[0], int(r[1])); err == nil {
			t.Fatalf("range offset %d size %d was accepted", r[0], r[1])
		}
	}
}

// readRange over honest ranges still works, including one that spans chunks
// and one that ends inside the short tail chunk.
func TestReadRangeReadsHonestRanges(t *testing.T) {
	raw := makeDictzip(t, []byte("abcdefgh"), 4)
	d, err := newDzReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		off int64
		n   int
	}{
		{0, 8}, // the whole extent
		{2, 5}, // spans the two chunks
		{4, 4}, // exactly the second chunk
	} {
		got, err := d.readRange(tc.off, tc.n)
		if err != nil {
			t.Fatalf("readRange(%d,%d): %v", tc.off, tc.n, err)
		}
		if want := "abcdefgh"[tc.off : tc.off+int64(tc.n)]; string(got) != want {
			t.Fatalf("readRange(%d,%d) = %q, want %q", tc.off, tc.n, got, want)
		}
	}
	// The tail chunk is short, and a range ending inside it must read.
	raw = makeDictzip(t, []byte("abcdef"), 4) // second chunk holds "ef"
	d, err = newDzReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := d.readRange(4, 2); err != nil || string(got) != "ef" {
		t.Fatalf("tail chunk read = %q err %v", got, err)
	}
}

// TestReadGzBoundedRefusesBombs: a gzip stream that inflates far past the cap
// is refused - the cap read is bounded, so the refusal does not first
// materialize what it refuses.
func TestReadGzBoundedRefusesBombs(t *testing.T) {
	var raw bytes.Buffer
	zw := gzip.NewWriter(&raw)
	zw.Write(make([]byte, 1<<20)) // 1 MiB of zeros against a 64 KiB cap
	zw.Close()
	gr, err := gzip.NewReader(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readGzBounded(gr, 64<<10); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected the index-size refusal, got %v", err)
	}
}
