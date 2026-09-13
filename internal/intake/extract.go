// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Errors an extraction refuses on. Each names a claim the archive made that
// the bytes then broke, which is the only class of failure here that is not
// simply the disk's.
var (
	ErrIncomplete = errors.New("dictionary is missing files it cannot work without")
	ErrUnsafePath = errors.New("archive entry points outside the destination")
	ErrSizeLied   = errors.New("archive entry is larger than it declared")
	ErrBomb       = errors.New("archive entry is compressed beyond any plausible ratio")
)

// Extract writes one candidate's files into dest, which must already exist and
// should be empty: it is the caller's staging directory, and on failure the
// caller removes it whole rather than trying to undo a partial write.
//
// Paths are re-rooted on the candidate's own directory, so a bundle packed as
// "Dicts/English/Oxford.mdx" installs as "Oxford.mdx" at the top of dest
// rather than rebuilding somebody else's folder tree inside the library - with
// the single exception of a StarDict "res/" subtree, which is addressed by
// relative path from inside the dictionary and keeps its shape.
//
// RAM is one copyBufBytes buffer and one open entry, always. Entries are
// written in the order THE ARCHIVE STORES THEM, on this goroutine. That is not
// cosmetic: a 7z folder is one stream holding several files end to end, so
// reading them in order is linear and reading them backwards means
// decompressing the folder again from its start for each one. A zip is
// indifferent to the order, so one rule serves both.
//
// ctx is checked per buffer rather than per entry, so cancelling a 2 GB import
// stops within a megabyte of the tap instead of at the end of the file that
// made the user cancel. progress, when not nil, is called with the size of
// each chunk written; it is called from this goroutine and must not block.
func Extract(ctx context.Context, a Archive, c Candidate, dest string, progress func(n int64)) error {
	if !c.Complete() {
		return fmt.Errorf("%w: %s", ErrIncomplete, strings.Join(c.Missing, ", "))
	}
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	base := candidateDir(c)
	// One pass over the directory rather than a scan per file: a candidate is
	// a handful of entries and an archive can be a hundred thousand. The same
	// pass records each entry's position, which is the order the files are
	// then written in.
	entries := a.Entries()
	index := make(map[string]Entry, len(entries))
	order := make(map[string]int, len(entries))
	for i, e := range entries {
		index[e.Name] = e
		order[e.Name] = i
	}
	// A copy, because the candidate belongs to the caller and is what the user
	// was shown: the display order is the dictionary's own (main file first),
	// and the read order is the archive's.
	files := append([]string(nil), c.Files...)
	sort.SliceStable(files, func(i, j int) bool { return order[files[i]] < order[files[j]] })
	buf := make([]byte, copyBufBytes)
	for _, name := range files {
		rel := strings.TrimPrefix(strings.TrimPrefix(name, base), "/")
		if rel == "" || !safeEntryName(rel) {
			return fmt.Errorf("%w: %s", ErrUnsafePath, name)
		}
		out, err := resolve(root, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		e, ok := index[name]
		if !ok {
			return errors.New("no such entry: " + name)
		}
		if err := writeEntry(ctx, a, e, out, buf, progress); err != nil {
			return err
		}
	}
	return nil
}

// writeEntry streams one entry to disk, enforcing the two numbers its
// directory declared. Both are enforced WHILE WRITING rather than compared
// afterwards: an entry that inflates to eight gigabytes has already cost eight
// gigabytes of disk by the time a check at the end can notice, and on a phone
// that is the whole partition.
func writeEntry(ctx context.Context, a Archive, e Entry, out string, buf []byte, progress func(int64)) error {
	// An archive is free to declare a large entry and free to store a small
	// one; declaring a small stored size AND a vast uncompressed one is the
	// shape of a bomb and of nothing else. Checked before the handle exists.
	if e.Compressed > 0 && e.Size/e.Compressed > maxRatio {
		return fmt.Errorf("%w: %s", ErrBomb, e.Name)
	}
	src, err := a.Open(e.Name)
	if err != nil {
		return err
	}
	defer src.Close()

	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	// One byte past the declared size, so a truthful entry copies whole and a
	// lying one is caught by the first byte it should not have had.
	//
	// The destination is wrapped in a plain io.Writer: *os.File implements
	// ReaderFrom, and io.CopyBuffer hands the whole copy to it and IGNORES the
	// buffer given here, allocating its own 32 KiB one per entry instead. The
	// wrapper is what makes "one buffer, always" true rather than intended.
	r := &ctxReader{ctx: ctx, r: io.LimitReader(src, e.Size+1), progress: progress}
	n, err := io.CopyBuffer(struct{ io.Writer }{f}, r, buf)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > e.Size {
		return fmt.Errorf("%w: %s", ErrSizeLied, e.Name)
	}
	return nil
}

// candidateDir is the archive directory the candidate's main file sits in,
// which is the prefix every one of its files is re-rooted against.
func candidateDir(c Candidate) string {
	if i := strings.LastIndexByte(c.Main, '/'); i >= 0 {
		return c.Main[:i]
	}
	return ""
}

// resolve is the second line of the traversal defence, after safeEntryName.
// The relative path has already been rejected for climbing, but "already
// rejected" is a property of the code that rejected it, and this is a write to
// the user's disk: the destination is resolved and checked to be inside root
// before the handle is opened, so a path that got past the first test still
// cannot be written through.
//
// Symlinks in root itself are not the concern - root is a directory this
// process just created - so a lexical containment check on the cleaned path is
// what is needed, and it costs no stat.
func resolve(root, rel string) (string, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if p != root && !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, rel)
	}
	return p, nil
}

// ctxReader is cancellation and progress in the one place both are cheap: the
// read loop. Deliberately NOT a WriterTo - the whole point is that the copy
// comes back through here once per buffer.
type ctxReader struct {
	ctx      context.Context
	r        io.Reader
	progress func(int64)
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 && c.progress != nil {
		c.progress(int64(n))
	}
	return n, err
}
