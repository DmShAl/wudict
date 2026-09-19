// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package bgl

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// A block header nibble below 4 hands the reader a length field of n+1 bytes;
// four of them name up to 4 GiB. Before maxBlockBytes that length went
// straight into make - an OOM abort on a hostile or truncated file, which no
// recover converts.
func TestReadBlockStreamRefusesHugeLength(t *testing.T) {
	var b []byte
	b = append(b, 0x31) // type 1, nibble 3: a 4-byte length follows
	b = binary.BigEndian.AppendUint32(b, 1<<31-1)
	_, _, ok, err := readBlockStream(bufio.NewReader(bytes.NewReader(b)))
	if ok {
		t.Fatal("a 2 GiB block length was accepted")
	}
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected the block-size refusal, got %v", err)
	}
}

// The honest shapes around the refusal: a small block reads back whole, and
// the stream ends quietly when the bytes do.
func TestReadBlockStreamReadsSmallBlocks(t *testing.T) {
	var b []byte
	b = append(b, 0x31) // type 1, nibble 3: a 4-byte length follows
	b = binary.BigEndian.AppendUint32(b, 3)
	b = append(b, "abc"...)
	br := bufio.NewReader(bytes.NewReader(b))
	typ, data, ok, err := readBlockStream(br)
	if err != nil || !ok || typ != 1 || string(data) != "abc" {
		t.Fatalf("small block: got type %d data %q ok %v err %v", typ, data, ok, err)
	}
	_, _, ok, err = readBlockStream(br)
	if ok || err != nil {
		t.Fatalf("end of stream: got ok %v err %v, want quiet false", ok, err)
	}
}

// splitEntryType11 frames its fields with length checks that a 32-bit int can
// wrap past (a u40 word length goes negative); this can only prove the
// 64-bit paths, which those guards must not disturb.
func TestSplitEntryType11HonestEntriesFrame(t *testing.T) {
	var b []byte
	b = append(b, 0, 0, 0, 0, 3)            // u40 word length
	b = append(b, "dog"...)                 // the word
	b = binary.BigEndian.AppendUint32(b, 2) // two alternates: one real, terminator
	b = binary.BigEndian.AppendUint32(b, 3) // the alternate's length
	b = append(b, "cat"...)
	b = binary.BigEndian.AppendUint32(b, 0) // the zero length that ends the run
	b = binary.BigEndian.AppendUint32(b, 6) // definition length
	b = append(b, "woffs!"...)
	e, ok := splitEntryType11(b)
	if !ok || string(e.word) != "dog" || string(e.defi) != "woffs!" ||
		len(e.alts) != 1 || string(e.alts[0]) != "cat" {
		t.Fatalf("honest entry framed as %#v ok=%v", e, ok)
	}
	// A definition length running past the data is a refusal, not a slice.
	// The length field sits just before the definition bytes it names.
	for i := len(b) - 10; i < len(b)-6; i++ {
		b[i] = 0xff
	}
	if _, ok := splitEntryType11(b); ok {
		t.Fatal("an over-long definition length was accepted")
	}
}
