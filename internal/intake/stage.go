// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Where a half-extracted dictionary lives while it is being extracted.
//
// The location is not a temporary directory, and that is the whole point.
// Extraction ends in os.Rename, which is atomic only within one filesystem; a
// TMPDIR staging area would make every import a second full copy of itself,
// and on Android TMPDIR is the internal cache partition, so a 2 GB bundle
// would be both a cross-device copy AND an overflow of the wrong partition.
// Staging therefore sits inside the destination folder, where the rename is a
// directory entry being moved.
//
// It is hidden because dict.Discover skips hidden subtrees by rule: a scan
// racing an extraction must not offer a dictionary whose files are still
// arriving.
const StageDirName = ".wudict-intake"

// staleAfter is how old an abandoned staging directory must be before a sweep
// removes it. A sweep runs at startup, when nothing of ours is extracting, but
// a second wudict may be running against the same folder - a phone and a
// desktop over a synced drive, two ports on one machine - and removing its
// live job would be indistinguishable from a corrupt archive. An hour is far
// longer than any extraction and far shorter than a user will wonder where
// their disk went.
const staleAfter = time.Hour

// StageRoot is the staging parent inside dest. Not created by this function:
// naming a path and making one are different acts, and Sweep must be able to
// name it without creating it.
func StageRoot(dest string) string { return filepath.Join(dest, StageDirName) }

// Stage creates a private directory for one job under dest and returns it.
// The 0700 mode is deliberate on a multi-user desktop: the contents are the
// user's dictionaries mid-flight, and nothing else has business reading a
// directory whose whole lifetime is measured in minutes.
func Stage(dest string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	dir := filepath.Join(StageRoot(dest), hex.EncodeToString(b[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Sweep removes staging directories left behind by a process that died
// mid-extraction - power loss, an Android low-memory kill, a SIGKILL - from
// every folder given. Called once at startup, beside the library's own
// orphan check, because that is the one moment at which every job this
// installation owns is known to be finished.
//
// It never reports an error. A folder that cannot be read is a folder nothing
// was staged in, and a startup that refuses to proceed because it could not
// tidy is worse than the untidiness.
func Sweep(dests []string) {
	cut := time.Now().Add(-staleAfter)
	for _, d := range dests {
		root := StageRoot(d)
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		left := 0
		for _, e := range ents {
			fi, err := e.Info()
			if err != nil {
				continue
			}
			if fi.ModTime().After(cut) {
				left++
				continue
			}
			if os.RemoveAll(filepath.Join(root, e.Name())) != nil {
				left++
			}
		}
		if left == 0 {
			// Empty, so the folder the user actually looks at is left as they
			// left it. Never forced: a non-empty root means somebody is still
			// working in it.
			_ = os.Remove(root)
		}
	}
}

// windowsReserved are the device names MS-DOS bequeathed to every Windows
// filesystem, which are still not usable as file names and still resolve to
// the device WITH any extension attached. An archive is free to contain
// "CON.mdx"; a folder named after it is not free to exist.
var windowsReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// safeDirName turns a candidate's name - which came out of an archive and is
// therefore attacker-chosen - into one path segment that can be created on any
// of the three platforms this runs on.
//
// A rewriting pass rather than the rejection test the entry names get, because
// the two questions differ: an entry name that is not safe can be dropped,
// since the archive holds others, but a candidate's name is the one the user
// has just been shown and agreed to install. Refusing it would mean refusing
// the dictionary over the spelling of its folder. Everything structural - the
// separators, the traversal, the device names - is removed rather than
// escaped, and the result is still checked for containment when it is used.
func safeDirName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			// control characters, including the newline that makes a folder
			// name unreadable in every listing
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	// Leading dots hide the folder from the scan that is supposed to find it;
	// trailing dots and spaces are silently stripped by Windows, which turns
	// "a." and "a" into a collision the caller cannot see coming.
	out := strings.TrimRight(strings.TrimLeft(b.String(), ". "), ". ")
	if i := strings.IndexByte(out, '.'); windowsReserved[strings.ToLower(out)] ||
		(i > 0 && windowsReserved[strings.ToLower(out[:i])]) {
		out = "_" + out
	}
	// Bytes, not runes: the limit every filesystem here enforces is 255 bytes
	// per component. Cut on a rune boundary so the name stays printable.
	for len(out) > 200 {
		_, n := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-n]
	}
	if out == "" {
		return "dictionary"
	}
	return out
}

// uniqueDir picks a folder inside dest that does not exist yet, so importing
// the same bundle twice produces a second dictionary rather than an extraction
// into the first one's folder. The suffix is the form a file manager uses,
// because this name is shown to the user and read by them in a listing.
func uniqueDir(dest, name string) (string, error) {
	base := safeDirName(name)
	for n := 1; n <= 200; n++ {
		try := base
		if n > 1 {
			try = base + " (" + strconv.Itoa(n) + ")"
		}
		p := filepath.Join(dest, try)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p, nil
		}
	}
	return "", os.ErrExist
}

// spoolPrefix names the per-upload directory Spool creates. It is also the
// guard on removing one: disposal deletes a DIRECTORY, and a directory is only
// ever deleted when its name says this package made it.
const spoolPrefix = "incoming-"

// Spool writes uploaded bytes into the staging area and describes them as a
// Source. It exists because two of the three ways a dictionary arrives - a
// browser drop and an Android share - deliver BYTES rather than a path, and
// this package reads from files: a zip is read through its central directory,
// which means seeking to the tail of a stream nobody has yet received the
// middle of.
//
// The file keeps THE NAME IT WAS GIVEN, inside a directory of its own. That is
// not tidiness: a loose dictionary file is named after the dictionary, so a
// spooled copy called "incoming-3141592.mdx" would install a dictionary called
// "incoming-3141592" - and the directory is what keeps the sibling scan that
// finds a loose file's companions from finding somebody else's upload instead.
//
// It lands beside the extraction it feeds rather than in TMPDIR for the same
// reason the staging directory does, and it is marked Temp, so it is removed
// when the job ends however the job ends - a spooled copy is an intermediary,
// never the user's own file, and "keep the source" cannot mean keeping it.
func Spool(dest, name string, r io.Reader) (Source, error) {
	if err := os.MkdirAll(StageRoot(dest), 0o700); err != nil {
		return Source{}, err
	}
	dir, err := os.MkdirTemp(StageRoot(dest), spoolPrefix+"*")
	if err != nil {
		return Source{}, err
	}
	if name == "" {
		name = "archive"
	}
	name = safeDirName(filepath.Base(name))
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		_ = os.RemoveAll(dir)
		return Source{}, err
	}
	// Buffered rather than io.Copy's default, and matching the extraction
	// buffer: this is the same megabyte-at-a-time discipline, applied to the
	// one path that is allowed to be as large as the archive itself.
	buf := make([]byte, copyBufBytes)
	_, err = io.CopyBuffer(struct{ io.Writer }{f}, r, buf)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return Source{}, err
	}
	return Source{Path: f.Name(), Name: name, Temp: true}, nil
}

// removeSource disposes of one job's source. A spooled copy is a directory
// this package made and takes the directory with it; anything else is the
// user's own file and only ever loses the one file named.
func removeSource(src Source) {
	if src.Temp {
		dir := filepath.Dir(src.Path)
		if strings.HasPrefix(filepath.Base(dir), spoolPrefix) {
			_ = os.RemoveAll(dir)
			return
		}
	}
	_ = os.Remove(src.Path)
}
