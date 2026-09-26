// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wuweidict/wudict/internal/config"
	"github.com/wuweidict/wudict/internal/dict"
	"github.com/wuweidict/wudict/internal/logx"
	"github.com/wuweidict/wudict/internal/server"
	"github.com/wuweidict/wudict/internal/store"
)

// `wudict reindex` - bring prepared dictionaries current; the command-line
// half of the panel's Rebuild (server/reindex.go).
//
// Outdated-only by default: a dictionary is rebuilt when store.Stale says its
// prepared data is not what this build would prepare from its source - an
// older build wrote it, the source was edited, or this build cannot read it.
// -all rebuilds current ones too, for damage the version stamps cannot see.
// Each dictionary keeps its plan (store.KeptPlan), and one that had packed
// media gets it repacked when the rebuild left it unpaired. A dictionary whose
// source is gone cannot be rebuilt: it is named and left as it is. There is no
// dry run - `wudict list` already says what is prepared, and a rebuild
// replaces each database atomically, so an interrupted run loses nothing.
//
// A running server on this library holds these databases open and serves
// from them, so the rebuild is not done underneath it. The plain form hands
// the job to that server - the same job its panel starts - and follows the
// progress; -all and named dictionaries, which that job does not offer, are
// refused. Only the configured address is asked: a server started on another
// port with other flags is not found, as `wudict rm` does not look for one.

func cmdReindex(args []string) error {
	applyLibrarySettings()
	fs := flag.NewFlagSet("reindex", flag.ExitOnError)
	all := fs.Bool("all", false, "rebuild current dictionaries too, not only outdated ones")
	fs.Parse(args)

	if inst, addr, ok := libraryServer(); ok {
		if *all || fs.NArg() > 0 {
			return fmt.Errorf("a wudict server is using this library at http://%s/ - stop it first, "+
				"or use Rebuild in its dictionary panel (outdated dictionaries only)", addr)
		}
		return handOffReindex(inst, addr)
	}

	targets, err := reindexTargets(fs.Args())
	if err != nil {
		return err
	}
	var todo []store.Folder
	var gone []string
	current := 0
	for _, f := range targets {
		switch {
		case f.Source == "" || !fileExists(f.Source):
			gone = append(gone, folderLabel(f))
		case *all || len(store.Stale(store.TextDBPath(f.Dir), f.Source)) > 0:
			todo = append(todo, f)
		default:
			current++
		}
	}
	for _, name := range gone {
		fmt.Fprintf(os.Stderr, "%sthe original file is gone - not rebuilt\n", logx.Dict(name))
	}
	if len(todo) == 0 {
		if current > 0 {
			fmt.Printf("%s up to date - nothing to rebuild\n", plural(current, "dictionary is", "dictionaries are"))
		} else if len(gone) == 0 {
			fmt.Printf("nothing is prepared in %s\n", store.DefaultDBDir())
		}
		return nil
	}

	var failed int
	for i, f := range todo {
		fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", i+1, len(todo), folderLabel(f))
		if err := reindexOne(f, *all); err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
			failed++
		}
	}
	if current > 0 {
		fmt.Printf("%s already up to date\n", plural(current, "dictionary was", "dictionaries were"))
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d dictionaries could not be rebuilt", failed, len(todo))
	}
	return nil
}

// folderLabel is the name a library folder is known by in `wudict list`.
func folderLabel(f store.Folder) string { return filepath.Base(f.Dir) }

// reindexTargets resolves the arguments to library folders: every folder
// holding a text.db when there are none, else each named one, once.
func reindexTargets(args []string) ([]store.Folder, error) {
	folders, err := store.Folders()
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return folders, nil
	}
	var out []store.Folder
	seen := map[string]bool{}
	for _, arg := range args {
		f, err := resolveFolder(arg, folders)
		if err != nil {
			return nil, err
		}
		if !seen[f.Dir] {
			seen[f.Dir] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// resolveFolder accepts every identifier `wudict rm` does (resolveRemoval).
// That one knows the library through what it can read; a folder whose
// text.db this build cannot open - exactly the one most in need of a rebuild
// - is still reachable by its folder name or path.
func resolveFolder(arg string, folders []store.Folder) (store.Folder, error) {
	dir, _, name, err := resolveRemoval(arg)
	for _, f := range folders {
		if dir != "" && dict.SameDir(f.Dir, dir) {
			return f, nil
		}
	}
	// resolveRemoval misses it, or (for an existing file such as the text.db
	// itself) accepts it without a folder
	for _, f := range folders {
		if strings.EqualFold(strings.TrimSpace(arg), filepath.Base(f.Dir)) ||
			dict.SameDir(arg, f.Dir) || dict.SameDir(arg, store.TextDBPath(f.Dir)) {
			return f, nil
		}
	}
	if err != nil {
		return store.Folder{}, err
	}
	return store.Folder{}, fmt.Errorf("%q is not prepared - there is nothing to rebuild", name)
}

// reindexOne rebuilds one folder's text.db with the plan it has, when it is
// outdated (or always, with force), then repacks media it had and no longer
// serves.
func reindexOne(f store.Folder, force bool) error {
	textDB, mediaDB := store.TextDBPath(f.Dir), store.MediaDBPath(f.Dir)
	hadMedia := fileExists(mediaDB)
	name := folderLabel(f)
	if reasons := store.TextStale(textDB, f.Source); force || len(reasons) > 0 {
		if len(reasons) > 0 {
			logx.V("%soutdated (%v)", logx.Dict(name), reasons)
		}
		r, err := dict.OpenReader(f.Source)
		if err != nil {
			return err
		}
		plan := store.KeptPlan(textDB)
		start := time.Now()
		rep, err := store.IngestPlan(r, textDB, plan, entryProgress)
		r.Close()
		logx.ClearLine()
		if err != nil {
			return err
		}
		fmt.Printf("%s%s indexed in %.1fs\n", logx.Dict(name),
			plural(rep.Entries, "entry", "entries"), time.Since(start).Seconds())
	}
	if hadMedia && (force || !store.MediaPaired(textDB)) {
		if err := packMedia(f.Source, textDB, name); err != nil {
			return err
		}
	}
	_ = store.WriteInfo(f.Dir)
	return nil
}

// entryProgress is the one-line entry counter every CLI rebuild shows.
func entryProgress(done, total int) {
	if total > 0 {
		logx.Progress("  %d/%d entries", done, total)
	} else {
		logx.Progress("  %d entries", done)
	}
}

// libraryServer finds a wudict serving on the configured address and using
// this library. A server on another library holds none of these files; one
// that asks for an access key says nothing about its library, so it is
// assumed to be using this one - the cautious reading.
func libraryServer() (*runningInstance, string, bool) {
	cfg, err := config.Load("", nil)
	if err != nil {
		return nil, "", false
	}
	addr := cfg.Addr()
	inst, ok := probeRunning(addr)
	if !ok {
		return nil, "", false
	}
	if !inst.Restricted && inst.LibDir != "" && !dict.SameDir(inst.LibDir, store.DefaultDBDir()) {
		return nil, "", false
	}
	return inst, addr, true
}

// handOffReindex starts (or joins) the running server's rebuild and follows
// it to the end. Interrupting this stops only the following: the job belongs
// to the server, and its panel shows and can stop it.
func handOffReindex(inst *runningInstance, addr string) error {
	keyed := fmt.Errorf("a wudict server is using this library at http://%s/ and asks for an access key - "+
		"use Rebuild in its dictionary panel", addr)
	if inst.Restricted {
		return keyed
	}
	url := "http://" + probeHost(addr) + "/api/reindex"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "", nil)
	if err != nil {
		return fmt.Errorf("asking the server at %s to rebuild: %w", addr, err)
	}
	started := resp.StatusCode == http.StatusAccepted
	st, err := readReindexStatus(resp)
	if errors.Is(err, errKeyed) {
		return keyed
	}
	if err != nil {
		return err
	}
	if !st.Running {
		fmt.Println("every dictionary is up to date - nothing to rebuild")
		return nil
	}
	if started {
		fmt.Printf("the server at http://%s/ is rebuilding %s\n", addr, plural(st.Total, "dictionary", "dictionaries"))
	} else {
		fmt.Printf("the server at http://%s/ is already rebuilding - following it\n", addr)
	}
	misses := 0
	for st.Running {
		if st.Current != "" {
			if st.CurrentTotal > 0 {
				logx.Progress("  [%d/%d] %s  %d/%d entries", min(st.Done+1, st.Total), st.Total,
					st.Current, st.CurrentDone, st.CurrentTotal)
			} else {
				logx.Progress("  [%d/%d] %s", min(st.Done+1, st.Total), st.Total, st.Current)
			}
		}
		time.Sleep(time.Second)
		resp, err := client.Get(url)
		var next server.ReindexStatus
		if err == nil {
			next, err = readReindexStatus(resp)
		}
		if err != nil {
			// a restart or a busy moment is not the end of the job; a server
			// that stays away is
			if misses++; misses == 10 {
				logx.ClearLine()
				return fmt.Errorf("lost contact with the server at %s (%v) - "+
					"if it is still running, its dictionary panel shows the rebuild", addr, err)
			}
			continue
		}
		misses, st = 0, next
	}
	logx.ClearLine()
	for _, f := range st.Failed {
		fmt.Fprintf(os.Stderr, "  failed: %s\n", f)
	}
	ok := st.Done - len(st.Failed)
	fmt.Printf("rebuilt %d of %s", ok, plural(st.Total, "dictionary", "dictionaries"))
	if st.Canceled {
		fmt.Print(" - stopped from the panel")
	}
	fmt.Println()
	if len(st.Failed) > 0 {
		return fmt.Errorf("%d of %d dictionaries could not be rebuilt", len(st.Failed), st.Total)
	}
	return nil
}

var errKeyed = errors.New("access key required")

func readReindexStatus(resp *http.Response) (server.ReindexStatus, error) {
	defer resp.Body.Close()
	var st server.ReindexStatus
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return st, errKeyed
	case resp.StatusCode/100 != 2:
		return st, fmt.Errorf("the server answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return st, fmt.Errorf("unexpected answer from the server: %w", err)
	}
	return st, nil
}
