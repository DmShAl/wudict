// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package go_mdict

import (
	"bytes"
	"compress/zlib"
	"strings"
	"testing"
)

// TestZlibDecompressBoundsOutput: the decompressed size a caller checks
// against is itself read out of the file, so the inflate itself has to be the
// bounded step - a small block that claims (or inflates to) gigabytes is a
// refusal, not a zlib bomb.
func TestZlibDecompressBoundsOutput(t *testing.T) {
	var bomb bytes.Buffer
	zw := zlib.NewWriter(&bomb)
	zw.Write(make([]byte, 4<<20)) // 4 MiB of zeros against a 1 MiB cap
	zw.Close()
	if _, err := zlibDecompress(bomb.Bytes(), 0, int64(bomb.Len()), 1<<20); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected the inflate-size refusal, got %v", err)
	}

	var honest bytes.Buffer
	zw = zlib.NewWriter(&honest)
	zw.Write([]byte("hello"))
	zw.Close()
	out, err := zlibDecompress(honest.Bytes(), 0, int64(honest.Len()), 1<<20)
	if err != nil || string(out) != "hello" {
		t.Fatalf("honest block = %q err %v", out, err)
	}

	// A range naming bytes outside the buffer is an error, not a slice panic.
	if _, err := zlibDecompress(honest.Bytes(), 0, 1<<40, 1<<20); err == nil {
		t.Fatal("an out-of-buffer range was accepted")
	}
	if _, err := zlibDecompress(honest.Bytes(), -1, 2, 1<<20); err == nil {
		t.Fatal("a negative offset was accepted")
	}
}
