// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package slob

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// craft writes the smallest header a slob parser accepts, with storeOffset
// under the caller's control. file_size is set to the file's true length so
// the header's own size check - which covers size and nothing else - passes.
func craft(t *testing.T, storeOffset uint64) string {
	t.Helper()
	var b []byte
	b = append(b, magic...)
	b = append(b, make([]byte, 16)...) // uuid
	b = append(b, 5)                   // tinyText len
	b = append(b, "utf-8"...)
	b = append(b, 0)                        // compression: ""
	b = append(b, 0)                        // tag count
	b = append(b, 0)                        // content-type count
	b = binary.BigEndian.AppendUint32(b, 0) // blob count
	b = binary.BigEndian.AppendUint64(b, storeOffset)
	b = binary.BigEndian.AppendUint64(b, uint64(len(b)+8)) // file_size == truth
	path := filepath.Join(t.TempDir(), "crafted.slob")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCraftedStoreOffsetAllocatesNothing is the regression for the class of
// bug where a file-declared offset becomes an allocation size. 1<<31 asked for
// 2 GiB before the span checks; 1<<62 exceeded makeslice and panicked. Neither
// is a survivable failure - the first is a runtime OOM abort, which no recover
// converts.
func TestCraftedStoreOffsetAllocatesNothing(t *testing.T) {
	for _, off := range []uint64{1 << 31, 1 << 40, 1 << 62, ^uint64(0)} {
		path := craft(t, off)

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		_, err := Open(path)
		runtime.ReadMemStats(&after)

		if err == nil {
			t.Fatalf("storeOffset %#x: opened a 61-byte file as valid", off)
		}
		const cap = 32 << 20
		if grew := after.TotalAlloc - before.TotalAlloc; grew > cap {
			t.Errorf("storeOffset %#x: allocated %d bytes rejecting a 61-byte file (cap %d)",
				off, grew, cap)
		}
	}
}

// TestCraftedStoreOffsetInsideFile covers the other direction: an offset that
// IS inside the file must still be rejected on its contents, not accepted.
func TestCraftedStoreOffsetInsideFile(t *testing.T) {
	if _, err := Open(craft(t, 61)); err == nil {
		t.Fatal("truncated store dir accepted")
	}
}

// TestItemContentTypeAllocatesNothingOnAHugeHeader is the regression for the
// lookup-path twin: itemContentType reads a bin's content-type id count
// straight into make, and a header naming 0xFFFFFFFF ids asked for a 4 GiB
// allocation in a media listing. The span check the lookup path always had
// now stands on this path too.
func TestItemContentTypeAllocatesNothingOnAHugeHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hostile.slob")
	bin := bytes.Repeat([]byte{0xff}, 64) // every count and offset maximal
	if err := os.WriteFile(path, bin, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c := &container{f: f, size: int64(len(bin)), storePos: []uint64{0}}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err = c.itemContentType(0, 0)
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("a bin header declaring 4 GiB of ids was accepted")
	}
	const cap = 32 << 20
	if grew := after.TotalAlloc - before.TotalAlloc; grew > cap {
		t.Errorf("refusing the header allocated %d bytes (cap %d)", grew, cap)
	}
}

// TestReadAllBoundedRefusesBombs exercises the inflate ceiling directly: the
// bound applies to what the codec PRODUCES, and the refusal comes before the
// caller can be handed a buffer it never asked for.
func TestReadAllBoundedRefusesBombs(t *testing.T) {
	var bomb bytes.Buffer
	zw := zlib.NewWriter(&bomb)
	zw.Write(make([]byte, 4096)) // 4 KiB of zeros against a 128-byte cap
	zw.Close()
	zr, err := zlib.NewReader(bytes.NewReader(bomb.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if _, err := readAllBounded(zr, 128); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected the inflate-size refusal, got %v", err)
	}

	var honest bytes.Buffer
	zw = zlib.NewWriter(&honest)
	zw.Write(bytes.Repeat([]byte("x"), 100))
	zw.Close()
	zr, err = zlib.NewReader(bytes.NewReader(honest.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out, err := readAllBounded(zr, 128)
	if err != nil || len(out) != 100 {
		t.Fatalf("honest bin = %d bytes err %v", len(out), err)
	}
}
