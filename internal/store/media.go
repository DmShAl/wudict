// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/wuweidict/wudict/internal/dict"
	"github.com/wuweidict/wudict/internal/htmlref"
	"github.com/wuweidict/wudict/internal/resource"
)

// Media is one opened `media.db` (SPEC §3): binary resources
// packed at ingest=full, paired to its text.db by dict_uuid.
type Media struct {
	db   *sql.DB
	UUID string
}

func OpenMedia(path string) (*Media, error) {
	db, err := openRO(path)
	if err != nil {
		return nil, err
	}
	var ver int
	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		db.Close()
		return nil, err
	}
	if ver != schemaVersion {
		db.Close()
		return nil, fmt.Errorf("%s: not a wudict media database", path)
	}
	m, err := readMeta(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Media{db: db, UUID: m["dict_uuid"]}, nil
}

func (m *Media) Close() error { return m.db.Close() }

// blobStreamMin is the size at which a packed resource stops being read
// whole. Below it - stylesheets, icons, audio clips - one row read is the
// cheapest possible answer. Above it - the videos and multi-megabyte PDFs
// some dictionaries pack - reading whole meant every request, including every
// Range probe a browser makes while playing, allocated the blob entire to
// serve a few hundred KB of it.
const blobStreamMin = 4 << 20

// blobChunk is one substr fetch out of a streamed blob.
const blobChunk = 1 << 20

func (m *Media) Resource(name string) (io.ReadCloser, string, error) {
	// Spellings tried in order, each one a way the SAME file can be named:
	// exact, then case-insensitively (MDD names are indexed lower-cased while
	// an article may reference mixed case, and loose files are packed under
	// their real name), then the two Unicode normalisations.
	//
	// Normalisation matters wherever a non-ASCII name came off a filesystem:
	// macOS hands out "espan\u0303ol.pdf" (NFD) while the article, written by
	// the dictionary's author, says "espa\u00f1ol.pdf" (NFC). The two are the
	// same name to a human and different byte strings to SQLite, and COLLATE
	// NOCASE folds case only - so without this a packed dictionary 404s on a
	// file it demonstrably contains, only for accented names, only on some
	// machines. resource.Key does the same job for the container backends.
	nfc, nfd := norm.NFC.String(name), norm.NFD.String(name)
	probes := []struct{ where, arg string }{
		{"name = ?", name},
		{"name = ? COLLATE NOCASE", name},
	}
	if nfc != name {
		probes = append(probes, struct{ where, arg string }{"name = ? COLLATE NOCASE", nfc})
	}
	if nfd != name && nfd != nfc {
		probes = append(probes, struct{ where, arg string }{"name = ? COLLATE NOCASE", nfd})
	}
	// The first pass asks only for the MIME and the LENGTH. length() reads the
	// blob's size off the record header - no blob pages move - so the whole-
	// read-or-stream decision below is made before a byte of content has been
	// paid for, and a miss (probes after the first) costs no content either.
	var mime string
	var size int64
	var where, arg string
	err := sql.ErrNoRows
	for _, p := range probes {
		err = m.db.QueryRow("SELECT mime, length(data) FROM resource WHERE "+p.where, p.arg).Scan(&mime, &size)
		if err != sql.ErrNoRows {
			where, arg = p.where, p.arg
			break
		}
	}
	if err == sql.ErrNoRows {
		return nil, "", dict.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if size <= blobStreamMin {
		var data []byte
		if err := m.db.QueryRow("SELECT data FROM resource WHERE "+where, arg).Scan(&data); err != nil {
			return nil, "", err
		}
		// A *bytes.Reader, kept seekable. io.NopCloser hides Seek behind a
		// plain io.Reader, and the resource handler tests for io.ReadSeeker to
		// decide whether it can answer Range requests (server.handleResource).
		return readSeekNopCloser{bytes.NewReader(data)}, mime, nil
	}
	return nopCloser{&blobReader{db: m.db, where: where, arg: arg, size: size}}, mime, nil
}

// readSeekNopCloser is io.NopCloser that keeps the Seek method.
type readSeekNopCloser struct{ *bytes.Reader }

func (readSeekNopCloser) Close() error { return nil }

// nopCloser keeps Seek visible through the io.ReadCloser Resource returns.
type nopCloser struct{ io.ReadSeeker }

func (nopCloser) Close() error { return nil }

// blobReader is a ReadSeeker over one large resource.data blob, fetched
// through substr in chunks. Streaming is only half the point; the other half
// is the Seek: the resource handler answers Range requests from any
// io.ReadSeeker (server.handleResource), so a browser's scrubber - or
// Safari's opening range probe, without which it refuses to start a video -
// reads exactly the bytes it asked for instead of pulling the whole blob
// through Scan first. The db handle is the Media's own read-only pool, and
// Close is a no-op because nothing here owns a connection.
type blobReader struct {
	db    *sql.DB
	where string // the WHERE clause that resolved the name's spelling
	arg   string
	size  int64
	pos   int64
	buf   []byte
	bufAt int64 // file position of buf[0]
}

func (b *blobReader) Read(p []byte) (int, error) {
	if b.pos >= b.size {
		return 0, io.EOF
	}
	if b.pos < b.bufAt || b.pos >= b.bufAt+int64(len(b.buf)) {
		if err := b.fill(b.pos); err != nil {
			return 0, err
		}
	}
	n := copy(p, b.buf[b.pos-b.bufAt:])
	b.pos += int64(n)
	return n, nil
}

func (b *blobReader) fill(off int64) error {
	n := int64(blobChunk)
	if off+n > b.size {
		n = b.size - off
	}
	// substr is 1-based; a range running past the blob returns what exists.
	// Placeholder order follows the SQL: substr's start and length, then the
	// WHERE argument.
	if err := b.db.QueryRow("SELECT substr(data, ?, ?) FROM resource WHERE "+b.where,
		off+1, n, b.arg).Scan(&b.buf); err != nil {
		return err
	}
	b.bufAt = off
	return nil
}

func (b *blobReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += b.pos
	case io.SeekEnd:
		offset += b.size
	}
	if offset < 0 {
		return 0, fmt.Errorf("seek before the start of the resource")
	}
	b.pos = offset
	return b.pos, nil
}

func (b *blobReader) Close() error { return nil }

// References are found with the shared HTML tokenizer (internal/htmlref), not
// a pattern over the markup. The regex this replaced accepted quoted values
// only - its own comment claimed "unquoted attributes are rare in dictionary
// markup", which is simply untrue: OALD10 writes `href=plaintiff__gb_1.ogg"`
// on every pronunciation link, and every such asset was silently left out of
// the pack. It also matched inside <script> strings and comments, packing
// files no article ever loads.

// ReferencedAssets returns the relative resource names an already-prepared
// dictionary's articles refer to. Packing uses it to include files that live
// beside the .mdx rather than inside the .mdd (a repack's stylesheet and
// scripts) - but only the ones actually referenced: dictionary folders often
// hold several dictionaries, so sweeping the directory would pack a neighbour's
// assets.
//
// Reads the prepared text.db rather than re-parsing the source: the bodies are
// already there, and packing media is an explicit, minutes-long operation where
// one decompression pass costs nothing.
func ReferencedAssets(textDB string) ([]string, error) {
	db, err := openRO(textDB)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT m FROM entry")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	const maxDistinct = 20000 // a pathological article must not blow up memory
	// A collector: Refs walks every reference site and rewrites nothing.
	var walker htmlref.Rewriter
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		for _, ref := range walker.Refs(decodeBody(raw)) {
			name := ref.URL
			if !isRelativeAsset(name) || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if len(out) >= maxDistinct {
				return out, nil
			}
		}
	}
	return out, rows.Err()
}

// isRelativeAsset keeps references that name a file belonging to this
// dictionary: no scheme (http:, data:, bword:, sound:…), no fragment or query
// only, nothing climbing out of the folder.
func isRelativeAsset(s string) bool {
	if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "?") || strings.HasPrefix(s, "//") {
		return false
	}
	if schemeRef.MatchString(s) {
		return false
	}
	if strings.Contains(s, "..") {
		return false
	}
	return true
}

// schemeRef matches a real URI scheme at the start of a reference.
var schemeRef = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)

// MediaNames is what a media pack of src takes: every resource the format
// lists (resource.Filter), plus the files beside the source that textDB's
// articles reference and the format does not list (a repack's stylesheet and
// scripts live next to the .mdx, not in the .mdd). Referenced-only, never the
// whole folder: dictionary folders commonly hold several dictionaries, and
// sweeping would pack a neighbour's assets. extra counts the referenced ones.
// IngestMedia skips whatever of these cannot be read.
func MediaNames(src dict.Dictionary, textDB string) (names []string, extra int) {
	if lister, ok := src.(dict.ResourceLister); ok {
		names = resource.Filter(lister.Resources())
	}
	refs, err := ReferencedAssets(textDB)
	if err != nil || len(refs) == 0 {
		return names, 0
	}
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[strings.ToLower(n)] = true
	}
	for _, n := range refs {
		if !have[strings.ToLower(n)] {
			names = append(names, n)
			extra++
		}
	}
	return names, extra
}

// IngestMedia packs every resource of d into a media.db at dbPath,
// stamped with dictUUID (must be the paired text.db's dict_uuid).
func IngestMedia(d dict.Dictionary, names []string, dbPath, dictUUID string, progress Progress) (err error) {
	tmp := tempDBName(dbPath)
	_ = os.Remove(tmp)
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	db, err := sql.Open(driverName, dsnIngest(tmp))
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err = db.Exec(fmt.Sprintf(`
		PRAGMA user_version = %d;
		CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT);
		CREATE TABLE resource(name TEXT PRIMARY KEY, mime TEXT, data BLOB);
	`, schemaVersion)); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	m := d.Meta()
	for k, v := range map[string]string{
		"dict_uuid":     dictUUID,
		"name":          m.Name,
		"format":        m.Format,
		"media_version": fmt.Sprint(MediaVersion), // which IngestMedia packed this (stale.go)
	} {
		if _, err = tx.Exec("INSERT INTO meta(key, value) VALUES(?, ?)", k, v); err != nil {
			return err
		}
	}
	ins, err := tx.Prepare("INSERT OR IGNORE INTO resource(name, mime, data) VALUES(?, ?, ?)")
	if err != nil {
		return err
	}
	for i, name := range names {
		rc, mime, rerr := d.Resource(name)
		if rerr != nil {
			continue // missing/corrupt resource: skip, keep packing
		}
		data, rerr := io.ReadAll(rc)
		rc.Close()
		if rerr != nil {
			continue
		}
		if _, err = ins.Exec(name, mime, data); err != nil {
			return err
		}
		if progress != nil && (i+1)%100 == 0 {
			progress(i+1, len(names))
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(len(names), len(names))
	}
	syncFile(tmp)
	if err = os.Rename(tmp, dbPath); err != nil {
		return err
	}
	// the receipt now has media to describe (best-effort, see IngestLevel).
	if strings.EqualFold(filepath.Base(dbPath), MediaDBName) {
		_ = WriteInfo(filepath.Dir(dbPath))
	}
	return nil
}

// ReadMetaValue reads one meta value from a wudict database file.
func ReadMetaValue(dbPath, key string) (string, error) {
	db, err := openRO(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var v string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}
