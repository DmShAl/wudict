// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures in testdata/ are real 7-zip archives, built by the 7z tool,
// because this package has no 7z writer and a hand-rolled one would be testing
// our idea of the format rather than the format. All four hold the same two
// dictionaries:
//
//	Oxford.mdx, Oxford.mdd          4096 bytes each
//	sd/x.ifo, sd/x.idx, sd/x.dict   a StarDict set
//	sd/res/a.png                    its shared resources
//
//	bundle.7z  solid: every file in ONE folder
//	blocks.7z  the same files packed into THREE folders of two files each
//	           (-ms=8k), which is the only way to reach the folder-boundary
//	           path that the reopen exists for
//	sealed.7z  contents encrypted, names readable
//	locked.7z  header encrypted too, so even the listing needs the password
const (
	mdxBody  = "mdx-body-"
	mddBody  = "mdd-body-"
	idxBody  = "idx-"
	dictBody = "dict-"
)

func fixture(t *testing.T, name string) Archive {
	t.Helper()
	a, err := OpenSevenZip(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// body is the fixture's content for an entry: a short seed repeated to n
// bytes, so a mis-seeked solid stream produces a mismatch rather than a
// plausible-looking file.
func body(seed string, n int) string {
	return strings.Repeat(seed, n/len(seed)+1)[:n]
}

func TestSevenZipListsFilesOnly(t *testing.T) {
	a := fixture(t, "bundle.7z")
	var got []string
	for _, e := range a.Entries() {
		got = append(got, e.Name)
	}
	// "sd" and "sd/res" are directory entries in the archive and must not
	// appear: everything downstream treats an Entry as something to open.
	want := []string{"Oxford.mdd", "Oxford.mdx", "sd/res/a.png", "sd/x.dict", "sd/x.idx", "sd/x.ifo"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for _, e := range a.Entries() {
		if e.Compressed != 0 {
			t.Errorf("%s: Compressed = %d, want 0 - the format has no per-file answer", e.Name, e.Compressed)
		}
		if e.Name == "Oxford.mdx" && e.Size != 4096 {
			t.Errorf("Oxford.mdx size = %d, want 4096", e.Size)
		}
	}
}

func TestSevenZipSniffsTheSameGroupsAZipWould(t *testing.T) {
	a := fixture(t, "bundle.7z")
	got, err := Sniff(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates = %d, want 2: %+v", len(got), got)
	}
	byName := map[string]Candidate{}
	for _, c := range got {
		byName[c.Name] = c
	}
	ox, ok := byName["Oxford"]
	if !ok {
		t.Fatalf("no Oxford candidate in %+v", got)
	}
	if !ox.Complete() || ox.Format != ".mdx" {
		t.Errorf("Oxford = %+v, want a complete .mdx", ox)
	}
	if strings.Join(ox.Files, ",") != "Oxford.mdx,Oxford.mdd" {
		t.Errorf("Oxford files = %v", ox.Files)
	}
	x, ok := byName["x"]
	if !ok {
		t.Fatalf("no StarDict candidate in %+v", got)
	}
	if !x.Complete() {
		t.Errorf("StarDict set reported incomplete: missing %v", x.Missing)
	}
	// res/ belongs to the sole dictionary in its directory, exactly as it does
	// in a folder on disk.
	if !contains(x.Files, "sd/res/a.png") {
		t.Errorf("res subtree not attached: %v", x.Files)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// recorder wraps an Archive and notes the order entries were opened in. The
// order is the whole point for 7z: a folder is one stream, so reading it
// backwards means decompressing it again per file.
type recorder struct {
	Archive
	opened []string
}

func (r *recorder) Open(name string) (io.ReadCloser, error) {
	r.opened = append(r.opened, name)
	return r.Archive.Open(name)
}

func TestSevenZipExtractsInArchiveOrder(t *testing.T) {
	a := fixture(t, "bundle.7z")
	cands, err := Sniff(a)
	if err != nil {
		t.Fatal(err)
	}
	var ox Candidate
	for _, c := range cands {
		if c.Name == "Oxford" {
			ox = c
		}
	}
	// The candidate lists its main file first, which is the DISPLAY order.
	if ox.Files[0] != "Oxford.mdx" {
		t.Fatalf("candidate order changed: %v", ox.Files)
	}
	rec := &recorder{Archive: a}
	dest := t.TempDir()
	if err := Extract(context.Background(), rec, ox, dest, nil); err != nil {
		t.Fatal(err)
	}
	// The archive stores .mdd first, and that is the order it is read in.
	if strings.Join(rec.opened, ",") != "Oxford.mdd,Oxford.mdx" {
		t.Errorf("read order = %v, want archive order", rec.opened)
	}
	wantFile(t, filepath.Join(dest, "Oxford.mdx"), body(mdxBody, 4096))
	wantFile(t, filepath.Join(dest, "Oxford.mdd"), body(mddBody, 4096))
}

func wantFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s: %d bytes, not what was packed", path, len(got))
	}
}

// The folder boundary is where a solid archive can cost memory: the library
// keeps a folder's decompressor alive between opens, so an extraction spanning
// three folders would otherwise hold three LZMA2 dictionaries at once.
func TestSevenZipDropsADecompressorAtEveryFolderBoundary(t *testing.T) {
	a := fixture(t, "blocks.7z")
	cands, err := Sniff(a)
	if err != nil {
		t.Fatal(err)
	}
	var sd Candidate
	for _, c := range cands {
		if c.Name == "x" {
			sd = c
		}
	}
	if len(sd.Files) == 0 {
		t.Fatalf("no StarDict candidate in %+v", cands)
	}
	dest := t.TempDir()
	if err := Extract(context.Background(), a, sd, dest, nil); err != nil {
		t.Fatal(err)
	}
	// The StarDict set spans two solid folders in this fixture, so exactly one
	// boundary is crossed - and every byte still arrives.
	if n := a.(*sevenZipArchive).reopens; n == 0 {
		t.Errorf("reopens = 0: the fixture no longer spans folders, so this proves nothing")
	}
	wantFile(t, filepath.Join(dest, "x.idx"), body(idxBody, 4096))
	wantFile(t, filepath.Join(dest, "x.dict"), body(dictBody, 4096))
	wantFile(t, filepath.Join(dest, "res", "a.png"), body("png-", 512))
}

// A non-solid archive never pools anything, so it must never pay for a reopen
// either - which is the difference the solid map exists to tell.
func TestSevenZipDoesNotReopenASingleFolderArchive(t *testing.T) {
	a := fixture(t, "bundle.7z")
	cands, err := Sniff(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if err := Extract(context.Background(), a, c, t.TempDir(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := a.(*sevenZipArchive).reopens; n != 0 {
		t.Errorf("reopens = %d, want 0: one folder was never left", n)
	}
}

func TestSevenZipRefusesAnArchiveBehindAPassword(t *testing.T) {
	// Header encrypted: the listing itself is unreadable, so the refusal
	// happens at open.
	if _, err := OpenSevenZip(filepath.Join("testdata", "locked.7z")); !errors.Is(err, ErrEncryptedArchive) {
		t.Errorf("locked.7z: err = %v, want ErrEncryptedArchive", err)
	}
	// Contents encrypted, names in the clear: it lists, and fails at the first
	// byte read. Either way the user is told the archive needs a password
	// rather than shown a checksum error.
	a, err := OpenSevenZip(filepath.Join("testdata", "sealed.7z"))
	if err != nil {
		if !errors.Is(err, ErrEncryptedArchive) {
			t.Fatalf("sealed.7z: err = %v", err)
		}
		return
	}
	defer a.Close()
	rc, err := a.Open("Oxford.mdx")
	if err == nil {
		_, err = io.Copy(io.Discard, rc)
		rc.Close()
	}
	if !errors.Is(err, ErrEncryptedArchive) && !bytes.Contains([]byte(errText(err)), []byte("password")) {
		t.Errorf("sealed.7z read: err = %v, want the password to be named", err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestArchiveKindKnowsSevenZip(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"bundle.7z", true},
		{"BUNDLE.7Z", true},
		{"bundle.zip", true},
		{"bundle.rar", false},
		{"bundle.7z.001", false}, // a volume set is not one file to open
		{"bundle", false},
	} {
		if got := SupportedArchive(tc.name); got != tc.want {
			t.Errorf("SupportedArchive(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
	a, err := OpenArchive(filepath.Join("testdata", "bundle.7z"))
	if err != nil {
		t.Fatalf("OpenArchive dispatch: %v", err)
	}
	defer a.Close()
	if len(a.Entries()) == 0 {
		t.Error("OpenArchive returned an empty 7z")
	}
}

func TestArchiveExtForSevenZipContentType(t *testing.T) {
	if got := archiveExtForType("application/x-7z-compressed"); got != ".7z" {
		t.Errorf("archiveExtForType = %q, want .7z", got)
	}
}
