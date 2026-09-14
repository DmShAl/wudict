// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package dsl

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"

	"github.com/wuweidict/wudict/internal/lang"
)

// Not every Lingvo export is Unicode. The compiler wrote UTF-16LE from Lingvo
// 8 on, but a great many .dsl files in circulation - GoldenDict-era Multitran
// rebuilds above all - are saved in ANSI, meaning a single-byte Windows code
// page, with no BOM and nothing in the body of the file to say which one.
// Lingvo's answer is the #SOURCE_CODE_PAGE directive, introduced in 8.0
// alongside Unicode support and, by the spec, "used only when the dictionary
// file is saved in ANSI" - which is exactly when this file is consulted, since
// the compiler itself ignores the directive on a UTF-16 source.
//
// Getting this wrong is not a cosmetic failure. The old fallback here was
// "assume UTF-16LE", and a single-byte file read as UTF-16 produces no U+000A
// at all - every LF is paired with the byte next to it into some other rune -
// so the scanner sees the whole dictionary as ONE line and dies with
// "bufio.Scanner: token too long" on a large file, or silently yields zero
// entries on a small one.

// codePages are the #SOURCE_CODE_PAGE values, from the DSL reference manual's
// chapter «Кодовые страницы ANSI в Windows» (dsl-language/index.html), which
// prints the MSDN code-page table with a "Наименование в Lingvo" column added.
// Seven of its ten ANSI pages have a documented Lingvo name; the manual marks
// the other three "?", its compilers having been unable to establish one.
//
// Those three are given the obvious name anyway. A reader has nothing to lose
// by accepting a spelling the compiler might have rejected - the alternative
// is not "reject it too", it is "fall back to Windows-1252 and mojibake the
// file" - and a dictionary that declares Hebrew and is not Unicode has exactly
// one thing it can be.
//
// Lingvo itself requires the exact documented casing and errors on "english";
// matching case-insensitively here is deliberate for the same reason.
var codePages = map[string]encoding.Encoding{
	// documented Lingvo names
	"easterneuropean": charmap.Windows1250,
	"cyrillic":        charmap.Windows1251,
	"latin":           charmap.Windows1252,
	"greek":           charmap.Windows1253,
	"turkish":         charmap.Windows1254,
	"arabic":          charmap.Windows1256,
	"baltic":          charmap.Windows1257,

	// the manual's three "?" rows, by MSDN's name for the page
	"thai":       charmap.Windows874,
	"hebrew":     charmap.Windows1255,
	"vietnamese": charmap.Windows1258,

	// Not an ANSI page and in neither of the manual's tables, so Lingvo
	// cannot have compiled it - but Shift-JIS is what a Japanese Windows
	// wrote, and a file that says so is readable rather than not.
	"japanese": japanese.ShiftJIS,
}

// headerField pulls one #KEY value out of the raw, still-undecoded head of the
// file. Legal only for the fields this package needs BEFORE it knows the
// encoding, and only because all of them are ASCII keys with ASCII values: in
// any single-byte code page those bytes are their own text, so the match is
// exact rather than a guess.
//
// The shape is the spec's: "#" at column 0, then one or more spaces or tabs,
// then the value in quotes. Unquoted values are accepted because files in the
// wild carry them, and a directive is one line, so the value ends at the line.
func headerField(sample []byte, key string) string {
	re := regexp.MustCompile(`(?im)^#` + key + `[ \t]+"?([^"\r\n]*)`)
	m := re.FindSubmatch(sample)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// eightBitEncoding picks the single-byte code page for a file that is neither
// UTF-16 nor valid UTF-8.
//
// #SOURCE_CODE_PAGE is believed when present. It is optional, though, and most
// tools omitted it, so the two directives that are NOT optional decide next:
// #INDEX_LANGUAGE and #CONTENTS_LANGUAGE must both be there or the dictionary
// would not have compiled, and the manual prints the language-to-code-page
// table for precisely this purpose ("with this table one can determine which
// code page a language belongs to - information for use with the
// #SOURCE_CODE_PAGE directive"). A header that says the headwords are Russian
// cannot be Windows-1252.
//
// Windows-1252 is the last resort, being both the Latin page and the one under
// which a mis-guess does the least damage: it maps every byte to some
// character, so nothing is lost, only mis-spelled.
func eightBitEncoding(sample []byte) encoding.Encoding {
	if cp := headerField(sample, "SOURCE_CODE_PAGE"); cp != "" {
		if enc, ok := codePages[strings.ToLower(cp)]; ok {
			return enc
		}
	}
	// Headwords first: they are what the index is built from, and in a
	// single-code-page file - which the spec requires an ANSI dictionary to
	// be - both directives agree anyway.
	for _, key := range []string{"INDEX_LANGUAGE", "CONTENTS_LANGUAGE"} {
		if enc := codePageForLang(lang.FromDeclared(headerField(sample, key))); enc != nil {
			return enc
		}
	}
	return charmap.Windows1252
}

// codePageForLang maps a language onto the Windows code page its text was
// written in before Unicode, after the manual's «Коды локалей Microsoft»
// table. nil means "no verdict" and the caller moves on.
//
// Everything the table gives 1252 is absent: that is the fallback already, and
// listing it would only make a future reader think the two came from different
// places. So are the three languages the table gives TWO pages - Serbian
// (1250 Latin / 1251 Cyrillic), Azeri and Uzbek (1251 / 1254) - because a
// declared "Serbian" carries no script (internal/lang keeps sr-Latn and
// sr-Cyrl apart only when it is given an LCID, which a directive never is) and
// #SOURCE_CODE_PAGE exists for exactly this case. Guessing there would be a
// coin toss dressed as a fact.
func codePageForLang(code string) encoding.Encoding {
	switch code {
	case "sq", "hr", "cs", "hu", "pl", "ro", "sk", "sl":
		return charmap.Windows1250
	case "be", "bg", "kk", "ky", "mk", "mn", "ru", "tt", "uk":
		return charmap.Windows1251
	case "el":
		return charmap.Windows1253
	case "tr":
		return charmap.Windows1254
	case "he":
		return charmap.Windows1255
	case "ar", "fa", "ur":
		return charmap.Windows1256
	case "et", "lv", "lt":
		return charmap.Windows1257
	case "vi":
		return charmap.Windows1258
	// Neither language is in the manual's locale table, so these two are
	// Windows' answer rather than Lingvo's - the same reasoning as the "?"
	// rows in codePages, and reachable only for a file that is already known
	// not to be Unicode.
	case "th":
		return charmap.Windows874
	case "ja":
		return japanese.ShiftJIS
	}
	return nil
}

// decodesToLines is the one check that makes an encoding guess falsifiable.
//
// Every DSL file is line-structured - directives one per line, headwords at
// column 0, bodies indented - so a decoding that produces no line break in the
// first 64 KB is wrong, whatever the byte statistics said. This is what
// separates "the sniff was unsure" from the failure mode that reaches the user
// as "token too long": it is checked BEFORE the scanner is built, so a bad
// guess is rejected while there is still another candidate to try, instead of
// surfacing 100 MB later as a buffer error.
func decodesToLines(enc encoding.Encoding, sample []byte) bool {
	out, err := enc.NewDecoder().Bytes(sample)
	// A truncated trailing rune is expected (sample cuts mid-character) and
	// says nothing about the encoding; what was decoded before it still does.
	if err != nil && len(out) == 0 {
		return false
	}
	return bytes.ContainsRune(out, '\n')
}
