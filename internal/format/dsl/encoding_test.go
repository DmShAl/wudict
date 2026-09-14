// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package dsl

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/wuweidict/wudict/internal/lang"
)

// The sniff peeks a fixed 4096 bytes, which cuts mid-rune whenever the byte at
// that boundary is not a rune start - roughly half the time for Cyrillic, which
// is exactly the audience for BOM-less DSL. Validating the raw peek then said
// "not UTF-8" and the whole dictionary was decoded as UTF-16LE: mojibake, no
// error, no way for the user to tell why.
func TestDetectEncodingRuneSplitSample(t *testing.T) {
	const peek = 1 << 12

	// CJK reaches the UTF-16LE fallback the long way round, by failing the UTF-8
	// test: its bytes carry almost no NULs, so the NUL probe abstains. Cyrillic
	// is the opposite and the reason the probe exists - it is byte-for-byte
	// valid UTF-8 (every byte of a U+04xx pair is below 0x80 or the constant
	// 0x04), so nothing downstream of the UTF-8 test can ever see it.
	enc16 := func(e encoding.Encoding, s string) []byte {
		b, err := e.NewEncoder().Bytes([]byte(s))
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		return b
	}
	utf16le := func() []byte {
		return enc16(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), "日本語の辞書")
	}

	cases := []struct {
		name string
		body []byte
		want string
	}{
		{
			name: "ascii",
			body: []byte(strings.Repeat("word\ttranslation\n", 600)),
			want: "UTF-8",
		},
		{
			// "я" is two bytes; an odd-length ASCII prefix puts the peek
			// boundary between them.
			name: "cyrillic split across the peek boundary",
			body: append([]byte(strings.Repeat("a", peek-1)), []byte(strings.Repeat("я", 100))...),
			want: "UTF-8",
		},
		{
			// Four-byte rune, boundary inside it.
			name: "astral rune split across the peek boundary",
			body: append([]byte(strings.Repeat("a", peek-2)), []byte(strings.Repeat("𝔘", 100))...),
			want: "UTF-8",
		},
		{
			name: "bom-less utf-16le stays utf-16le",
			body: bytes.Repeat(utf16le(), 300),
			want: "UTF-16LE",
		},
		{
			// The regression the probe ordering exists for: without it this
			// passes utf8.Valid and the whole dictionary becomes mojibake.
			name: "bom-less utf-16le cyrillic",
			body: bytes.Repeat(enc16(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), "словарь\tdictionary\n"), 200),
			want: "UTF-16LE",
		},
		{
			name: "bom-less utf-16be cyrillic",
			body: bytes.Repeat(enc16(unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), "словарь\tdictionary\n"), 200),
			want: "UTF-16BE",
		},
		{
			// The other side of the ordering: real UTF-8 Cyrillic has no NUL
			// bytes at all, so the probe abstains and UTF-8 still wins.
			name: "bom-less utf-8 cyrillic",
			body: []byte(strings.Repeat("словарь\tdictionary\n", 300)),
			want: "UTF-8",
		},
		{
			name: "bom wins over the sample",
			body: append([]byte{0xFF, 0xFE}, utf16le()...),
			want: "UTF-16LE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			br := bufio.NewReaderSize(bytes.NewReader(tc.body), peek*2)
			enc, err := detectEncoding(br)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			decoded, err := enc.NewDecoder().Bytes(tc.body[:min(len(tc.body), peek)])
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := "UTF-16LE"
			switch {
			case enc == unicode.UTF8 || enc == unicode.UTF8BOM:
				got = "UTF-8"
			case enc == unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM):
				got = "UTF-16BE"
			}
			if got != tc.want {
				t.Fatalf("detected %s, want %s (decoded head %q)", got, tc.want, decoded[:min(len(decoded), 40)])
			}
		})
	}
}

// A single-byte Lingvo export must be recognised as such. Before code pages
// were understood, a BOM-less Windows-1251 file fell through both Unicode
// probes to the UTF-16LE fallback, where no LF byte survives decoding: the
// scanner then saw the entire dictionary as one token and a large one failed
// with "bufio.Scanner: token too long" - the error users reported against the
// GoldenDict-era Multitran rebuilds - while a small one silently read as zero
// entries.
func TestDetectEncodingSingleByte(t *testing.T) {
	body := func(header string) []byte {
		var b strings.Builder
		b.WriteString(header)
		for i := 0; i < 400; i++ {
			b.WriteString("слово\r\n\t[m1][trn]word[/trn][/m]\r\n\r\n")
		}
		out, err := charmap.Windows1251.NewEncoder().String(b.String())
		if err != nil {
			t.Fatal(err)
		}
		return []byte(out)
	}

	for _, tc := range []struct {
		name, header string
		want         encoding.Encoding
	}{
		{"declared code page", "#SOURCE_CODE_PAGE \"Cyrillic\"\r\n", charmap.Windows1251},
		{"implied by language", "#INDEX_LANGUAGE \"Russian\"\r\n", charmap.Windows1251},
		{"tab separator", "#SOURCE_CODE_PAGE\t\"Cyrillic\"\r\n", charmap.Windows1251},
		// Lingvo's compiler wants the documented casing; a reader gains
		// nothing by being as strict.
		{"case insensitive", "#SOURCE_CODE_PAGE \"CYRILLIC\"\r\n", charmap.Windows1251},
		// The rest of the manual's «Наименование в Lingvo» column.
		{"easterneuropean", "#SOURCE_CODE_PAGE \"EasternEuropean\"\r\n", charmap.Windows1250},
		{"latin", "#SOURCE_CODE_PAGE \"Latin\"\r\n", charmap.Windows1252},
		{"greek", "#SOURCE_CODE_PAGE \"Greek\"\r\n", charmap.Windows1253},
		{"turkish", "#SOURCE_CODE_PAGE \"Turkish\"\r\n", charmap.Windows1254},
		{"arabic", "#SOURCE_CODE_PAGE \"Arabic\"\r\n", charmap.Windows1256},
		{"baltic", "#SOURCE_CODE_PAGE \"Baltic\"\r\n", charmap.Windows1257},
		// The three pages the manual leaves as "?", named after MSDN.
		{"thai", "#SOURCE_CODE_PAGE \"Thai\"\r\n", charmap.Windows874},
		{"hebrew", "#SOURCE_CODE_PAGE \"Hebrew\"\r\n", charmap.Windows1255},
		{"vietnamese", "#SOURCE_CODE_PAGE \"Vietnamese\"\r\n", charmap.Windows1258},
		// An unknown name is not a verdict: the languages decide instead.
		{"unknown name falls through", "#SOURCE_CODE_PAGE \"Klingon\"\r\n#INDEX_LANGUAGE \"Ukrainian\"\r\n", charmap.Windows1251},
		// Contents language when the index language yields nothing.
		{"contents language", "#INDEX_LANGUAGE \"English\"\r\n#CONTENTS_LANGUAGE \"Greek\"\r\n", charmap.Windows1253},
		// Two pages in the manual's locale table, so the language alone is
		// not an answer - #SOURCE_CODE_PAGE exists for this.
		{"ambiguous language", "#INDEX_LANGUAGE \"Serbian\"\r\n", charmap.Windows1252},
		// Nothing declared and nothing to infer from: Windows-1252 is the
		// Lingvo default, and the bytes at least survive round-trip.
		{"nothing declared", "#NAME \"x\"\r\n", charmap.Windows1252},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := body(tc.header)
			got, err := detectEncoding(bufio.NewReaderSize(bytes.NewReader(raw), 1<<20))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("detectEncoding = %v, want %v", got, tc.want)
			}
			// The point of the fix: whatever code page is chosen, the stream
			// must still break into lines.
			if !decodesToLines(got, raw) {
				t.Fatal("chosen encoding yields no line breaks")
			}
		})
	}
}

// End to end: the reader parses a code-page file, and the header it reads back
// is the one that was written - which is only true if the sniff and the decode
// agree.
func TestReaderSingleByteFile(t *testing.T) {
	var b strings.Builder
	b.WriteString("#NAME \"Ru-En\"\r\n#INDEX_LANGUAGE \"Russian\"\r\n#SOURCE_CODE_PAGE \"Cyrillic\"\r\n")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "слово%d\r\n\t[m1][trn]word%d[/trn][/m]\r\n\r\n", i, i)
	}
	cp, err := charmap.Windows1251.NewEncoder().String(b.String())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cp.dsl")
	if err := os.WriteFile(path, []byte(cp), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if m := r.Meta(); m.Name != "Ru-En" || m.IndexLang != "ru" {
		t.Fatalf("meta = %+v", m)
	}
	n := 0
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("entry %d: %v", n, err)
		}
		if want := fmt.Sprintf("слово%d", n); e.Headwords[0] != want {
			t.Fatalf("headword %d = %q, want %q", n, e.Headwords[0], want)
		}
		n++
	}
	if n != 400 {
		t.Fatalf("entries = %d, want 400", n)
	}
}

// The manual's «Коды локалей Microsoft» table, spot-checked through the same
// path the header takes: declared name -> lang code -> code page.
func TestCodePageForLang(t *testing.T) {
	for _, tc := range []struct {
		declared string
		want     encoding.Encoding
	}{
		{"Russian", charmap.Windows1251},
		{"Belarusian", charmap.Windows1251},
		{"Kazakh", charmap.Windows1251},
		{"Bulgarian", charmap.Windows1251},
		{"Polish", charmap.Windows1250},
		{"Czech", charmap.Windows1250},
		{"Croatian", charmap.Windows1250},
		{"Greek", charmap.Windows1253},
		{"Turkish", charmap.Windows1254},
		{"Hebrew", charmap.Windows1255},
		{"Arabic", charmap.Windows1256},
		{"Lithuanian", charmap.Windows1257},
		{"Estonian", charmap.Windows1257},
		{"Vietnamese", charmap.Windows1258},
		// 1252 languages carry no entry: the fallback already covers them.
		{"English", nil},
		{"German", nil},
		// Two code pages in the table and no script in the declared name.
		{"Serbian", nil},
		{"Uzbek", nil},
	} {
		t.Run(tc.declared, func(t *testing.T) {
			code := lang.FromDeclared(tc.declared)
			if code == "" {
				t.Fatalf("%q is not a declared language name", tc.declared)
			}
			if got := codePageForLang(code); got != tc.want {
				t.Fatalf("codePageForLang(%q/%q) = %v, want %v", tc.declared, code, got, tc.want)
			}
		})
	}
}
