// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"errors"
	"io"
	"io/fs"
	"strings"

	"github.com/bodgit/sevenzip"
)

// ErrEncryptedArchive is an archive whose contents - or whose very list of
// contents - are behind a password. Reported as its own thing because it is
// the one archive failure the user can do something about, and because the
// decompressor's own words for it are not a sentence anybody reads.
var ErrEncryptedArchive = errors.New("archive is password-protected")

// sevenZipArchive reads a .7z. Two properties of the format shape everything
// here, and neither applies to zip:
//
//   - There is no per-file compressed size. Files are packed into FOLDERS
//     (the format's word for a solid block), and only the folder's packed size
//     exists. Entry.Compressed is therefore zero and the ratio check in
//     writeEntry does not fire; the bound that matters is the one that is
//     enforced anyway - each entry is copied through a limit of its DECLARED
//     size, so an entry that inflates past what it promised is truncated and
//     the extraction fails. A bomb has to lie about its size to be a bomb, and
//     that lie is caught while writing rather than by arithmetic beforehand.
//   - Reading is sequential. A folder is one LZMA2 stream holding several
//     files end to end, so reaching the third file means decompressing the
//     first two. bodgit/sevenzip handles this by keeping the folder's
//     decompressor alive between Opens, which makes IN-ORDER reading linear
//     and out-of-order reading quadratic. Extract therefore writes a
//     candidate's files in ARCHIVE order (see extract.go), and this type only
//     has to bound what stays alive while it does.
//
// The plan's prohibition stands: FileHeader.Stream is NOT used to regroup or
// parallelise the work. It is read for exactly one purpose - to know when the
// last folder has been left behind, so its decompressor can be dropped instead
// of accumulating one 16-64 MiB dictionary per folder the extraction touches.
type sevenZipArchive struct {
	path    string
	rc      *sevenzip.ReadCloser
	entries []Entry
	files   []*sevenzip.File
	byName  map[string]int
	// solid[folder] is true when the folder holds more than one file, which is
	// exactly the condition under which the library pools its decompressor
	// rather than closing it. A non-solid archive - one folder per file - is
	// never reopened, because it never retains anything to reopen away.
	solid map[int]bool
	// folder is the one the last Open read from, or -1 for none yet.
	folder int
	// reopens counts the folder boundaries crossed. Kept for the test that
	// proves the bound above is real rather than argued.
	reopens int
}

// OpenSevenZip opens an archive for listing. Only the header is parsed: the
// answer to "what is in here" costs a seek and no decompressor, exactly as it
// does for a zip's central directory.
func OpenSevenZip(path string) (Archive, error) {
	rc, err := sevenzip.OpenReader(path)
	if err != nil {
		return nil, sevenZipErr(err)
	}
	a := &sevenZipArchive{path: path, rc: rc, folder: -1}
	a.entries, a.files, a.byName, a.solid = indexSevenZip(rc)
	return a, nil
}

// indexSevenZip walks the header once, applying the same rules the zip reader
// applies to a central directory. Kept separate from OpenSevenZip because a
// reopen has to redo it against the new handles.
func indexSevenZip(rc *sevenzip.ReadCloser) ([]Entry, []*sevenzip.File, map[string]int, map[int]bool) {
	entries := make([]Entry, 0, len(rc.File))
	files := make([]*sevenzip.File, 0, len(rc.File))
	byName := make(map[string]int, len(rc.File))
	perFolder := map[int]int{}
	for _, f := range rc.File {
		if len(entries) >= maxEntries {
			// Capped here as well as in Sniff, so a crafted header cannot cost
			// a gigabyte of Entry structs on its way to being refused.
			break
		}
		name := normalizeEntryName(f.Name)
		if name == "" || strings.HasSuffix(f.Name, "/") {
			continue
		}
		info := f.FileInfo()
		if info.IsDir() {
			continue
		}
		// Same rule as zip: a symlink is a name pointing elsewhere, and
		// "elsewhere" is the whole of the traversal problem in one entry.
		// Nothing here needs them, and a device node in a dictionary bundle is
		// not a packing mistake.
		mode := f.Mode()
		if !mode.IsRegular() || mode&fs.ModeSymlink != 0 {
			continue
		}
		if f.UncompressedSize > maxCandidateBytes {
			// Larger than any single dictionary this package will install, so
			// it can only ever be refused - and dropping it here keeps the
			// declared size inside int64, where the candidate arithmetic
			// assumes it lives.
			continue
		}
		if _, seen := byName[name]; seen {
			continue
		}
		byName[name] = len(files)
		files = append(files, f)
		perFolder[f.Stream]++
		dir, base := splitEntry(name)
		entries = append(entries, Entry{
			Name: name,
			Dir:  dir,
			Base: base,
			Size: int64(f.UncompressedSize),
			// Compressed stays zero: the format has no per-file answer. See
			// the type comment for what enforces the bound instead.
		})
	}
	solid := make(map[int]bool, len(perFolder))
	for folder, n := range perFolder {
		solid[folder] = n > 1
	}
	return entries, files, byName, solid
}

func (a *sevenZipArchive) Entries() []Entry { return a.entries }

// Open streams one entry. The caller must close it before opening the next -
// the interface says so, and here it is load-bearing rather than tidy: two
// open entries are two live decompressors.
func (a *sevenZipArchive) Open(name string) (io.ReadCloser, error) {
	i, ok := a.byName[name]
	if !ok {
		return nil, errors.New("no such entry: " + name)
	}
	f := a.files[i]
	// An empty file is not in any folder at all - the library answers it with
	// an empty reader and touches no stream - so it must not be read as having
	// moved us to folder zero.
	if f.UncompressedSize == 0 {
		rc, err := f.Open()
		if err != nil {
			return nil, sevenZipErr(err)
		}
		return rc, nil
	}
	if a.folder >= 0 && f.Stream != a.folder && a.solid[a.folder] {
		// Leaving a solid folder. Its decompressor is pooled and would stay
		// pooled - and a 64 MiB LZMA2 dictionary per folder touched is the one
		// memory shape this format can produce that the rest of the package
		// cannot. Reopening costs a header parse, which is the same seek that
		// opened the archive in the first place.
		if err := a.reopen(); err != nil {
			return nil, err
		}
		f = a.files[a.byName[name]]
	}
	a.folder = f.Stream
	rc, err := f.Open()
	if err != nil {
		return nil, sevenZipErr(err)
	}
	return rc, nil
}

// reopen drops every decompressor by dropping the reader that owns them, then
// re-indexes. Entries are NOT rebuilt: they were already handed to the sniffer
// and to the user, and the names are what the new index is keyed by. A file
// that changed underneath us therefore fails at the next Open with "no such
// entry" rather than silently reading something else.
func (a *sevenZipArchive) reopen() error {
	old := a.rc
	rc, err := sevenzip.OpenReader(a.path)
	if err != nil {
		return sevenZipErr(err)
	}
	_ = old.Close()
	_, files, byName, solid := indexSevenZip(rc)
	a.rc, a.files, a.byName, a.solid = rc, files, byName, solid
	a.folder = -1
	a.reopens++
	return nil
}

func (a *sevenZipArchive) Close() error { return a.rc.Close() }

// sevenZipErr turns the decompressor's errors into this package's. Only the
// encrypted case is distinguished: it is the sole 7z failure with an action
// attached to it, and the library flags it explicitly rather than leaving it
// to be guessed from a checksum mismatch.
func sevenZipErr(err error) error {
	if err == nil {
		return nil
	}
	var re *sevenzip.ReadError
	if errors.As(err, &re) && re.Encrypted {
		return ErrEncryptedArchive
	}
	return err
}
