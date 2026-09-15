// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package dsl

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"

	"github.com/wuweidict/wudict/internal/dict"
	"github.com/wuweidict/wudict/internal/lang"
	"github.com/wuweidict/wudict/internal/logx"
)

// Reader is the sequential ingest scan over a .dsl / .dsl.dz file.
// Entry blocks: headword line(s) at column 0, indented body lines;
// "@ sub-headword" lines split off sub-entries (linked from the parent).
type Reader struct {
	f       *os.File
	scanner *bufio.Scanner
	meta    dict.Meta
	header  map[string]string
	path    string

	// The abbreviation companion is parsed on the first entry, not in
	// NewReader: Open builds a Reader for its name alone on every open of an
	// already-prepared dictionary, and that path must not pay for a file it
	// will never transform with.
	abbrevOnce sync.Once
	abbrev     *abbrevMap
	// plainBody is set for the scan of a companion itself: its articles are
	// the tooltip text, so they must not be prefixed with their own headword,
	// and it has no companion of its own to absorb.
	plainBody bool

	buffered  []string     // lookahead lines
	orphans   int          // body blocks skipped for having no headword
	pending   []dict.Entry // sub-entries queued behind the main entry
	scanCount int
	eof       bool

	// #INCLUDE continuation. The directive names further text files whose
	// entries belong to THIS dictionary (lingvo-ref "Директива #INCLUDE"): the
	// compiler concatenates them into one .lsd, so the reader concatenates them
	// into one scan. Queued in file order, each opened only when the one before
	// it runs out, and each decoded on its own - an include is a separate file
	// and may well be a separate encoding.
	includes  []string        // resolved paths still to read
	opened    map[string]bool // absolute paths already read: include cycles
	extra     []*os.File      // include handles, closed with the reader
	inComment bool            // a {{...}} comment zone is open across lines
	incPath   string          // file currently being read, "" while in the main one
	err       error           // first scan error, from whichever file raised it
}

func NewReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f, header: map[string]string{}, path: path, opened: map[string]bool{}}
	r.opened[absPath(path)] = true // a file that #INCLUDEs itself, once
	if err := r.init(path); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// newAbbrevReader opens a DSL as an abbreviation glossary rather than as a
// dictionary. Same parser, two suppressions - see Reader.plainBody.
func newAbbrevReader(path string) (*Reader, error) {
	r, err := NewReader(path)
	if err != nil {
		return nil, err
	}
	r.plainBody = true
	return r, nil
}

// abbrevs resolves this dictionary's abbreviation companion, once.
func (r *Reader) abbrevs() *abbrevMap {
	if r.plainBody {
		return nil
	}
	r.abbrevOnce.Do(func() { r.abbrev = loadAbbrev(r.path) })
	return r.abbrev
}

// ExtraMeta records what was absorbed, so a later run can tell a text.db built
// with this companion from one built without it, or with an older copy of it.
// Written only when a companion was actually found: a dictionary that has none
// records nothing, and therefore never looks stale for the lack of it.
func (r *Reader) ExtraMeta() map[string]string {
	a := r.abbrevs()
	if a == nil {
		return nil
	}
	return map[string]string{
		"abbrev_path":  a.path,
		"abbrev_size":  strconv.FormatInt(a.size, 10),
		"abbrev_mtime": a.mtime.Format(time.RFC3339),
		"abbrev_count": strconv.Itoa(a.count),
	}
}

// decodedScanner turns an already-open Lingvo file into a line scanner: gunzip
// when gzipped, then whatever charset detectEncoding sniffs. Shared by the
// dictionary reader and by the .ann annotation loader (ann.go), so a Lingvo
// file - which is UTF-16LE far more often than not - is decoded in exactly one
// place. path is used for the error text only.
//
// Compression is SNIFFED, never derived from the name. Both mistakes are in
// the wild: a gzipped glossary saved as plain ".dsl", and a ".dsl.dz" somebody
// already decompressed in place. Two magic bytes settle it, and the file is
// rewound whatever they say, so every caller may hand over a fresh handle.
func decodedScanner(f *os.File, path string) (*bufio.Scanner, error) {
	var magic [2]byte
	n, _ := io.ReadFull(f, magic[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("dsl %s: %w", path, err)
	}
	var src io.Reader = f
	if n == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("dsl %s: %w", path, err)
		}
		src = gz
	}
	br := bufio.NewReaderSize(src, 1<<20)
	enc, err := detectEncoding(br)
	if err != nil {
		return nil, fmt.Errorf("dsl %s: %w", path, err)
	}
	sc := bufio.NewScanner(transform.NewReader(br, enc.NewDecoder()))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	return sc, nil
}

func (r *Reader) init(path string) error {
	sc, err := decodedScanner(r.f, path)
	if err != nil {
		return err
	}
	r.scanner = sc

	// header: leading #KEY "value" lines; first non-# line starts entries
	for r.scanner.Scan() {
		line := strings.TrimPrefix(strings.TrimRight(r.scanner.Text(), "\r"), "\uFEFF")
		// A `{{...}}` zone is legal here too - a licence or authoring note
		// between the directives and the first card is where dictionaries
		// usually put one - and it may span lines. Without this the line that
		// ends the header loop is the bare "{{", which then becomes the first
		// headword and drags the note and the real first card in with it.
		// r.inComment carries an unclosed zone straight into nextLine.
		line, r.inComment = stripLineComments(line, r.inComment)
		if blankLine(line) {
			continue
		}
		if strings.HasPrefix(line, "#") {
			k, v := parseDirective(line)
			if k == "INCLUDE" {
				r.queueInclude(path, v)
				continue
			}
			r.header[k] = v
			continue
		}
		r.buffered = append(r.buffered, line)
		break
	}

	name := r.header["NAME"]
	if name == "" {
		// #FULL_NAME is what Lingvo 6.0/7.0 wrote instead, undocumented and
		// then withdrawn in 8.0 (lingvo-ref "Общее описание", директивы). A
		// dictionary of that vintage has a name; reading only #NAME threw it
		// away and fell through to the file name.
		name = r.header["FULL_NAME"]
	}
	if name == "" {
		base := filepath.Base(path)
		name = strings.TrimSuffix(strings.TrimSuffix(base, ".dz"), ".dsl")
	}
	from, to := r.header["INDEX_LANGUAGE"], r.header["CONTENTS_LANGUAGE"]
	if from == "" {
		// The same vintage: in 6.0/7.0 a main text file's #LANGUAGE named the
		// language of the HEADWORDS, which is exactly #INDEX_LANGUAGE's job.
		// (In a .ann it means something else entirely - ann.go - and a .ann is
		// never read through here.)
		from = r.header["LANGUAGE"]
	}
	desc := ""
	if from != "" || to != "" {
		desc = from + " → " + to
	}
	r.meta = dict.Meta{
		Name:        name,
		Format:      "dsl",
		Path:        path,
		Description: desc,
		// #INDEX_LANGUAGE names the language of the HEADWORDS, which is
		// exactly what a lemmatizer needs and is the one thing a path
		// convention can get backwards. Taken from the header field, never
		// re-parsed out of desc. Lingvo writes collation names here
		// ("SpanishModernSort"), which internal/lang absorbs.
		IndexLang: lang.FromDeclared(from),
		// #CONTENTS_LANGUAGE is the other end of the pair, and until the About
		// panel existed it survived only as the middle of the desc string
		// above. Recorded as a code so a consumer never has to parse " → ".
		ContentsLang: lang.FromDeclared(to),
	}
	return nil
}

// parseDirective splits one `#KEY "value"` preprocessor line. The key/value
// separator is "whitespace", not "a space": Lingvo's own sample writes
// #INDEX_LANGUAGE with a tab, and splitting on " " alone dropped the line
// entirely. The `#` must be the first character on the line - anything before
// it is a compile error in Lingvo - which is the caller's guarantee, not this
// function's.
func parseDirective(line string) (key, value string) {
	rest := line[1:]
	key = rest
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		key, value = rest[:i], rest[i+1:]
	}
	return strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// queueInclude resolves one #INCLUDE value against the file that wrote it.
// The value is a Windows path with its backslashes DOUBLED ("Extra\\more.dsl",
// lingvo-ref), so it is unescaped first and then read as a path in either
// convention. A path that does not resolve is retried by base name in the
// including file's own folder, which is where a dictionary that was copied off
// its author's machine actually keeps its parts; an absolute "c:\Lingvo\..."
// reaches that fallback and nothing else.
func (r *Reader) queueInclude(from, value string) {
	if value == "" {
		return
	}
	p := strings.ReplaceAll(strings.ReplaceAll(value, `\\`, `\`), `\`, "/")
	dir := filepath.Dir(from)
	var cand []string
	if filepath.IsAbs(p) {
		cand = append(cand, filepath.Clean(p))
	} else {
		cand = append(cand, filepath.Join(dir, filepath.FromSlash(p)))
	}
	cand = append(cand, filepath.Join(dir, path.Base(p)))
	for _, c := range cand {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			r.includes = append(r.includes, c)
			return
		}
	}
	// Missing includes are not fatal: Lingvo would refuse to compile, but a
	// reader that refuses to open the dictionary at all turns one absent file
	// into no dictionary. The entries that file held are simply not there.
	logx.Warn("dsl %s: #INCLUDE %q not found", from, value)
}

// nextSource switches the scan to the next #INCLUDE file, and reports whether
// there was one. Each is opened at most once, so an include cycle terminates.
func (r *Reader) nextSource() bool {
	for len(r.includes) > 0 {
		p := r.includes[0]
		r.includes = r.includes[1:]
		if a := absPath(p); r.opened[a] {
			continue
		} else {
			r.opened[a] = true
		}
		f, err := os.Open(p)
		if err != nil {
			logx.Warn("dsl: #INCLUDE %s: %v", p, err)
			continue
		}
		sc, err := decodedScanner(f, p)
		if err != nil {
			f.Close()
			logx.Warn("dsl: #INCLUDE %s: %v", p, err)
			continue
		}
		r.extra = append(r.extra, f)
		r.scanner = sc
		r.incPath = p
		return true
	}
	return false
}

// detectEncoding sniffs the BOM, then the NUL pattern, then UTF-8 validity,
// then the single-byte code page the header names or the declared languages
// imply (codepage.go). Every non-BOM verdict must also decode into lines - a
// DSL that comes out as one unbroken token was decoded wrong, whatever the
// byte statistics said.
func detectEncoding(br *bufio.Reader) (encoding.Encoding, error) {
	head, _ := br.Peek(4)
	switch {
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0xFF, 0xFE, 0x00, 0x00}):
		return utf32.UTF32(utf32.LittleEndian, utf32.UseBOM), nil
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x00, 0x00, 0xFE, 0xFF}):
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM), nil
	case len(head) >= 2 && bytes.Equal(head[:2], []byte{0xFF, 0xFE}):
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case len(head) >= 2 && bytes.Equal(head[:2], []byte{0xFE, 0xFF}):
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM), nil
	case len(head) >= 3 && bytes.Equal(head[:3], []byte{0xEF, 0xBB, 0xBF}):
		return unicode.UTF8BOM, nil
	}
	// No BOM. Three probes, and the order is deliberate: UTF-16 is asked FIRST.
	// Cyrillic in UTF-16LE is byte-for-byte valid UTF-8 - every U+04xx pair is a
	// byte below 0x80 followed by the constant 0x04, both legal single-byte
	// UTF-8 - so a UTF-8-first sniff accepts a BOM-less Russian export, which is
	// most of them, and ingests the whole dictionary as mojibake with no error.
	// No refinement of the UTF-8 test can see that; only a more specific
	// question asked earlier can.
	//
	// 64 KB rather than 4: the sample now has to carry the #SOURCE_CODE_PAGE
	// and #INDEX_LANGUAGE header lines to eightBitEncoding, and to hold enough
	// text for decodesToLines to be able to find a line break in it.
	sample, _ := br.Peek(1 << 16)
	if enc := utf16ByNULs(sample); enc != nil && decodesToLines(enc, sample) {
		return enc, nil
	}
	// Peek cuts on a byte boundary, which lands mid-rune roughly half the time
	// on non-Latin text. Validating that raw would reject perfectly good UTF-8
	// and fall through to the UTF-16LE assumption below, ingesting the entire
	// dictionary as mojibake. Drop the trailing partial rune first.
	for i := 0; i < utf8.UTFMax-1 && len(sample) > 0; i++ {
		if r, size := utf8.DecodeLastRune(sample); r != utf8.RuneError || size != 1 {
			break
		}
		sample = sample[:len(sample)-1]
	}
	if utf8.Valid(sample) {
		return unicode.UTF8, nil
	}
	// Neither Unicode form fits: a single-byte Windows code page, which the
	// header names or the declared languages imply (codepage.go). The old
	// unconditional UTF-16LE fallback is kept only for what it was ever right
	// about - a file whose NUL pattern was too weak to call above - and only
	// when it actually yields lines, because a single-byte file decoded as
	// UTF-16 yields exactly one line the length of the dictionary.
	cp := eightBitEncoding(sample)
	if decodesToLines(cp, sample) || !bytes.ContainsRune(sample, 0) {
		// Either the code page produces lines, or the file has no NUL in 64 KB
		// and therefore cannot be UTF-16 at all - in which case the code page
		// is the only candidate left, lines or not (a dictionary whose first
		// 64 KB is one enormous article is unusual but legal).
		return cp, nil
	}
	return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), nil
}

// utf16ByNULs answers "is this UTF-16, and which way round" from the NUL bytes,
// the one signal UTF-8 cannot produce: a NUL is legal UTF-8 but no text file
// contains one, while UTF-16 text in any script that stays under U+0100 - Latin,
// Cyrillic, Greek - is roughly half NUL, always at the same parity. That makes
// it safe to ask before utf8.Valid: it cannot misclassify real UTF-8, because
// real UTF-8 has no NULs to count.
//
// nil means "no verdict", and the caller falls through to the UTF-8 test and
// then to the UTF-16LE assumption. A UTF-16 file in a script that uses almost no
// ASCII (dense CJK: U+65E5 encodes as e5 65, no NUL) lands here and is still
// caught, because such bytes are not valid UTF-8 either.
func utf16ByNULs(sample []byte) encoding.Encoding {
	var even, odd int
	for i, b := range sample {
		if b != 0 {
			continue
		}
		if i%2 == 0 {
			even++
		} else {
			odd++
		}
	}
	// An eighth of the sample is far below the ~half that Latin or Cyrillic
	// UTF-16 yields and far above the zero any 8-bit text yields, and the
	// absolute floor keeps a short header from deciding on two bytes. The parity
	// must also be lopsided: NULs spread evenly over both are not character
	// padding, they are binary, and guessing UTF-16 there would be worse than
	// letting the UTF-8 test speak.
	const minNULs = 8
	if n := even + odd; n < minNULs || n < len(sample)/8 {
		return nil
	}
	switch {
	case odd > even*4:
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case even > odd*4:
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	}
	return nil
}

func (r *Reader) Meta() dict.Meta { return r.meta }

func (r *Reader) Close() error {
	for _, f := range r.extra {
		f.Close()
	}
	r.extra = nil
	return r.f.Close()
}

// curPath names the file the scan is in, for error text and for resolving a
// nested #INCLUDE against the file that wrote it rather than against the main
// one - they need not share a folder.
func (r *Reader) curPath() string {
	if r.incPath != "" {
		return r.incPath
	}
	return r.path
}

// stripLineComments removes every `{{...}}` zone from one raw line, returning
// whether a zone is still open when the line ends.
//
// This runs on the RAW line, before it is classified as a headword, a body line
// or a directive, and that is the only place it can run. A comment is ignored
// wholesale at compile time (lingvo-ref "Тэг {{···}}"), which has three
// consequences the later passes cannot reproduce, because by then the line has
// already been classified - or has decided which entry the lines around it
// belong to:
//
//   - a zone may span lines, and any headword between the opening and the
//     closing pair is ignored;
//   - a comment standing alone on its line leaves an empty line, so a comment
//     at column 0 is not a headword and an indented one is not a body line -
//     both simply cease to exist;
//   - where a comment and any other tag overlap, the comment wins.
//
// Inside a zone nothing is markup, not even the escape character: `{{c\}}`
// closes (the compiler is happy) while `{{c}\}` does not (it reports the
// unterminated comment). Outside one the escape still holds, so `\{\{` opens
// nothing.
func stripLineComments(line string, in bool) (string, bool) {
	if !in && !strings.Contains(line, "{{") {
		return line, false
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		if in {
			j := strings.Index(line[i:], "}}")
			if j < 0 {
				return b.String(), true
			}
			i += j + len("}}")
			in = false
			continue
		}
		switch {
		case line[i] == '\\':
			b.WriteByte(line[i])
			if i+1 < len(line) {
				b.WriteByte(line[i+1])
			}
			i += 2
		case line[i] == '{' && i+1 < len(line) && line[i+1] == '{':
			in = true
			i += 2
		default:
			b.WriteByte(line[i])
			i++
		}
	}
	return b.String(), in
}

// blankLine reports whether a line carries nothing but layout whitespace.
//
// ASCII space and tab only, deliberately. U+00A0, U+2002 and the rest of the
// Unicode space block are how a DSL author writes a blank line or an indent
// that survives the space-collapsing rule (lingvo-ref "Об использовании
// нестандартных пробелов"), so a line made of them is content, not emptiness -
// strings.TrimSpace, which folds every Unicode space, deleted exactly the
// paragraph breaks the author went out of their way to create.
func blankLine(s string) bool { return strings.Trim(s, " \t\v\f\r") == "" }

// nextLine returns the next raw line (CR stripped) from lookahead or file,
// crossing into the #INCLUDE files when the current one runs out.
func (r *Reader) nextLine() (string, bool) {
	if len(r.buffered) > 0 {
		l := r.buffered[0]
		r.buffered = r.buffered[1:]
		return l, true
	}
	for !r.eof {
		if r.scanner.Scan() {
			r.scanCount++
			// An include file carries its own BOM, and the decoder passes a
			// UTF-8 one through as U+FEFF on the first line.
			l := strings.TrimPrefix(strings.TrimRight(r.scanner.Text(), "\r"), "\uFEFF")
			l, r.inComment = stripLineComments(l, r.inComment)
			return l, true
		}
		if err := r.scanner.Err(); err != nil && r.err == nil {
			r.err = err
		}
		if !r.nextSource() {
			r.eof = true
		}
	}
	return "", false
}

func (r *Reader) Next() (dict.Entry, error) {
	if len(r.pending) > 0 {
		e := r.pending[0]
		r.pending = r.pending[1:]
		return e, nil
	}

	for {
		entry, subs, err := r.nextBlock()
		if errors.Is(err, errOrphanBlock) {
			// Body lines belonging to no headword. Lingvo refuses to compile
			// such a file, but here the file is already on the user's disk:
			// failing the read turns one stray run of lines into no dictionary
			// at all, so the block is dropped with a note and the scan goes on.
			r.orphans++
			if r.orphans <= 3 {
				logx.Warn("%s: %v (skipped)", filepath.Base(r.meta.Path), err)
			}
			continue
		}
		if err != nil {
			return dict.Entry{}, err
		}
		r.pending = subs
		return entry, nil
	}
}

// nextBlock reads one entry block: its headword lines, its body lines, and the
// sub-entries the body declares.
func (r *Reader) nextBlock() (dict.Entry, []dict.Entry, error) {
	var termLines, textLines []string
	for {
		line, ok := r.nextLine()
		if !ok {
			break
		}
		if blankLine(line) {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			textLines = append(textLines, line)
			continue
		}
		if strings.HasPrefix(line, "#") {
			// A headword is "a line beginning with any character except space,
			// tab and #" (lingvo-ref "Заголовок статьи"): a `#` at column 0 is
			// a preprocessor directive wherever it appears, never a word. Read
			// as a headword it produced a phantom entry AND swallowed the
			// article that followed it. Only #INCLUDE means anything this far
			// in - it may also appear at the head of an included file, which
			// is how a chain of them continues - and the rest are the header
			// of an include, already read where they mattered.
			if k, v := parseDirective(line); k == "INCLUDE" {
				r.queueInclude(r.curPath(), v)
			}
			continue
		}
		// headword line: if a block is complete, push this line back
		if len(textLines) > 0 {
			r.buffered = append(r.buffered, line)
			break
		}
		termLines = append(termLines, line)
	}
	if len(textLines) == 0 {
		if r.err != nil {
			return dict.Entry{}, nil, r.err
		}
		if err := r.scanner.Err(); err != nil {
			return dict.Entry{}, nil, err
		}
		return dict.Entry{}, nil, io.EOF
	}
	return r.parseBlock(termLines, textLines)
}

// errOrphanBlock marks a run of body lines that belongs to no headword. It is
// a skip, not a failure: see Next.
var errOrphanBlock = errors.New("dsl: entry block without headword")

// parseBlock converts one entry block. Port of pyglossary parseEntryBlock:
// titles yield Full/Alt headword variants; "@" lines split sub-entries,
// referenced from the main article.
func (r *Reader) parseBlock(termLines, textLines []string) (dict.Entry, []dict.Entry, error) {
	var terms []string
	var displayTitles []string
	seenTerm := map[string]bool{}
	for _, line := range termLines {
		t := transformTitle(line)
		if t.first() == "" {
			continue
		}
		// Two headword lines of one entry can expand onto the same key
		// ("(the) sun" above "sun"); one key indexed twice is one duplicate row
		// per hit in every result list.
		for _, k := range t.Keys {
			if !seenTerm[k] {
				seenTerm[k] = true
				terms = append(terms, k)
			}
		}
		if t.Display != escape(t.first()) && t.Display != "" {
			displayTitles = append(displayTitles, "<b>"+t.Display+"</b>")
		}
	}
	if len(terms) == 0 {
		return dict.Entry{}, nil, fmt.Errorf("%w near %q", errOrphanBlock, textLines[0])
	}

	var mainText strings.Builder
	var subs []dict.Entry
	// One sub-card may carry several headings piled on consecutive "@" lines,
	// and each heading may expand into two lookup keys ("(...)" optional part),
	// so both are lists. linesInCard is what tells a pile from the start of the
	// next card: an "@" line with body lines behind it closes, one without
	// simply adds another heading (goldendict-ng src/dict/dsl.cc, the
	// insidedCards loop).
	var subHeads []string
	var subText strings.Builder
	subOpen, linesInCard := false, 0
	flushSub := func() {
		defer func() {
			subHeads = nil
			subText.Reset()
			subOpen, linesInCard = false, 0
		}()
		var heads []string
		seenHead := map[string]bool{}
		for _, h := range subHeads {
			// `~` mirrors the PARENT headword into a sub-card heading, which is
			// how a phrase card is written under the word it belongs to
			// ("@ ~ up"). Substituted before the title parser runs, and escaped
			// on the way in, so a parent carrying "(" or "{" cannot turn into
			// optional-part or unsorted-part syntax in its child's key.
			t := transformTitle(expandTitleTilde(h, terms[0]))
			for _, k := range t.Keys {
				if !seenHead[k] {
					seenHead[k] = true
					heads = append(heads, k)
				}
			}
		}
		if len(heads) == 0 {
			return
		}
		body, _, err := transformBodyAbbrev(strings.TrimRight(subText.String(), "\n"), heads[0], r.abbrevs())
		if err == nil {
			subs = append(subs, dict.Entry{Headwords: heads, Body: body, Kind: dict.BodyHTML})
		}
		for _, k := range heads {
			// The back-reference is re-parsed as DSL, so the key has to survive
			// a second pass: an unescaped "[" or "~" in a sub-headword would be
			// read as markup and swallow the link. The leading "- " is what
			// Lingvo and GoldenDict both draw in front of a sub-card link.
			mainText.WriteString("\t[m2]- [ref]" + dslEscape(k) + "[/ref][/m]\n")
		}
	}
	for _, line := range textLines {
		if head, ok := atSignHeading(line); ok {
			if subOpen && linesInCard == 0 && head != "" {
				subHeads = append(subHeads, head) // another heading for the same card
				continue
			}
			flushSub()
			if head != "" {
				subHeads = append(subHeads, head)
				subOpen = true
			}
			continue
		}
		if subOpen {
			linesInCard++
			subText.WriteString(line + "\n")
			continue
		}
		mainText.WriteString(line + "\n")
	}
	flushSub()

	body, _, err := transformBodyAbbrev(strings.TrimRight(mainText.String(), "\n"), terms[0], r.abbrevs())
	if err != nil {
		return dict.Entry{}, nil, fmt.Errorf("dsl: entry %q: %w", terms[0], err)
	}
	if len(displayTitles) > 0 && !r.plainBody {
		body = strings.Join(displayTitles, "<br/>") + "<br/>" + body
	}
	return dict.Entry{Headwords: terms, Body: body, Kind: dict.BodyHTML}, subs, nil
}

// dslEscape backslash-escapes the characters the DSL lexer treats as markup,
// so a string can be embedded in generated DSL and come back out unchanged.
func dslEscape(s string) string { return dslEscaper.Replace(s) }

var dslEscaper = strings.NewReplacer(
	`\`, `\\`, "[", `\[`, "]", `\]`, "~", `\~`, "<", `\<`, ">", `\>`, "@", `\@`,
)

// titleEscaper escapes what a HEADWORD line - not a body - treats as syntax.
// The body alphabet and the title alphabet differ: "(" and "{" are inert in a
// body and are optional-part and unsorted-part markers in a title, while "[" is
// markup in both. A parent headword such as "(the) sun" or "f{oo}" substituted
// raw into a child heading would re-enter transformTitle as syntax and index
// the sub-card under keys the dictionary never declared.
var titleEscaper = strings.NewReplacer(
	`\`, `\\`, "[", `\[`, "]", `\]`, "~", `\~`,
	"(", `\(`, ")", `\)`, "{", `\{`, "}", `\}`,
)

// expandTitleTilde substitutes the parent headword for every unescaped "~" in a
// sub-card heading, which is how Lingvo writes a phrase card under the word it
// belongs to ("@ ~ up" under "give" is "give up"). An escaped "\~" is a literal
// tilde and is left for transformTitle to unescape.
func expandTitleTilde(head, parent string) string {
	if !strings.Contains(head, "~") {
		return head
	}
	esc := titleEscape(parent)
	var b strings.Builder
	for i := 0; i < len(head); i++ {
		switch c := head[i]; c {
		case '\\':
			b.WriteByte(c)
			if i+1 < len(head) {
				i++
				b.WriteByte(head[i])
			}
		case '~':
			b.WriteString(esc)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func titleEscape(s string) string { return titleEscaper.Replace(s) }

// atSignHeading recognises a sub-card line. Lingvo puts the "@" first on the
// line, but leading whitespace and leading DSL tags are allowed before it
// ("[m1]@ heading" is legal and common), and the space after it is optional:
// "@heading" and "@ heading" are the same thing. An empty heading closes the
// current sub-card. Mirrors isAtSignFirst() in goldendict-ng
// src/dict/dsl_details.cc.
var atSignLine = regexp.MustCompile(`^[ \t]*(?:\[[^\]]+\][ \t]*)*@`)

func atSignHeading(line string) (string, bool) {
	loc := atSignLine.FindStringIndex(line)
	if loc == nil {
		return "", false
	}
	return strings.TrimSpace(line[loc[1]:]), true
}
