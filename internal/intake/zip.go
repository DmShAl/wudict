// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"strings"
)

// zipArchive reads a .zip through the standard library, which is exactly the
// shape this package wants: OpenReader parses the CENTRAL DIRECTORY and
// nothing else, so listing a 4 GB archive costs a seek to its tail and no
// decompressor at all. Each Open then inflates one entry through a fixed
// 32 KiB window - the format's, not a choice of ours - which is why zip
// intake is structurally incapable of a memory surprise.
type zipArchive struct {
	rc      *zip.ReadCloser
	entries []Entry
	byName  map[string]*zip.File
}

// OpenZip opens an archive for listing. The file is held open until Close.
func OpenZip(path string) (Archive, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	a := &zipArchive{rc: rc, byName: make(map[string]*zip.File, len(rc.File))}
	for _, f := range rc.File {
		if len(a.entries) >= maxEntries {
			// The cap is enforced here as well as in Sniff so that a crafted
			// directory cannot cost a gigabyte of Entry structs on the way to
			// being refused.
			break
		}
		name := normalizeEntryName(f.Name)
		if name == "" || strings.HasSuffix(f.Name, "/") || f.FileInfo().IsDir() {
			continue
		}
		// A symlink is a name pointing somewhere else, and "somewhere else" is
		// the whole of the traversal problem in one entry: nothing here needs
		// them, so they are not carried. Likewise anything that is not a plain
		// file - a device node in a dictionary bundle is not a packing
		// mistake.
		if !f.Mode().IsRegular() || f.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if _, seen := a.byName[name]; seen {
			// Duplicate names are legal in the container and meaningless to
			// us; the first is the one a reader would find.
			continue
		}
		a.byName[name] = f
		dir, base := splitEntry(name)
		a.entries = append(a.entries, Entry{
			Name:       name,
			Dir:        dir,
			Base:       base,
			Size:       int64(f.UncompressedSize64),
			Compressed: int64(f.CompressedSize64),
		})
	}
	return a, nil
}

func (a *zipArchive) Entries() []Entry { return a.entries }

func (a *zipArchive) Open(name string) (io.ReadCloser, error) {
	f, ok := a.byName[name]
	if !ok {
		return nil, errors.New("no such entry: " + name)
	}
	return f.Open()
}

func (a *zipArchive) Close() error { return a.rc.Close() }

// normalizeEntryName puts an entry name into the one spelling the rest of the
// package uses: forward slashes, no leading "./", no trailing slash. The zip
// specification mandates forward slashes and real archivers ignore it, so a
// bundle packed by a Windows tool arrives with backslashes and would otherwise
// be one undivided "name" holding separators - which is how a path escapes a
// containment check that was only ever shown the last element.
//
// Normalising is safe here precisely because it is not the defence:
// safeEntryName rejects what is left, and extraction checks the canonical
// destination again before it writes.
func normalizeEntryName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	for strings.HasPrefix(name, "./") {
		name = name[2:]
	}
	return strings.TrimSuffix(name, "/")
}

func splitEntry(name string) (dir, base string) {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return "", name
}
