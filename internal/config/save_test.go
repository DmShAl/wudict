// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestSaveKeyRawConcurrentSavesKeepEveryKey: each save is a read-modify-write
// of the whole file, and the pages save per key - so saves that overlap must
// serialise, or every save but the last would vanish.
func TestSaveKeyRawConcurrentSavesKeepEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wudict.toml")
	if err := os.WriteFile(path, []byte("# header comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := SaveKey(path, fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i)); err != nil {
				t.Errorf("save %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if !strings.Contains(string(data), fmt.Sprintf("key%d = \"value%d\"", i, i)) {
			t.Errorf("key%d did not survive the concurrent saves:\n%s", i, data)
		}
	}
	if !strings.Contains(string(data), "# header comment") {
		t.Error("the header comment did not survive")
	}
	// The atomic write cleans up after itself: the directory holds the config
	// and nothing else.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		for _, e := range entries {
			t.Errorf("config directory holds %q", e.Name())
		}
	}
}

// TestSaveKeyRawKeepsWhatWriteFileKept: the atomic replace must not change
// what the plain write it replaced left alone - a symlinked config stays a
// link (the save lands in its target), and an existing file keeps its mode.
func TestSaveKeyRawKeepsWhatWriteFileKept(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles.toml")
	if err := os.WriteFile(target, []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o600); err != nil { // umask-proof
		t.Fatal(err)
	}
	link := filepath.Join(dir, "wudict.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := SaveKey(link, "port", "8080"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the config symlink was replaced (%v, %v)", fi, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), `port = "8080"`) || !strings.Contains(string(data), "# mine") {
		t.Fatalf("the save did not land in the link's target: %q %v", data, err)
	}
	// Windows has no permission bits to keep: Chmod toggles only read-only,
	// and every writable file stats as 0666.
	if runtime.GOOS == "windows" {
		return
	}
	if fi, err := os.Stat(target); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode after save = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
}
