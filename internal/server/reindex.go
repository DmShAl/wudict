// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/wuweidict/wudict/internal/logx"
	"github.com/wuweidict/wudict/internal/store"
)

// The panel's Rebuild: bring every OUTDATED dictionary current, as one job the
// user starts, watches and can stop. Outdated is store.Stale - built by an
// older build, from a source edited since, or unreadable by this one - and is
// what /api/dicts reports per row; the page sums those rows into the one line
// that offers this job, and shows nothing when the sum is zero.
//
// It is never started on anyone's behalf. On a large library a rebuild is
// hours of work, which is why an upgrade that changes the indexing only marks
// the library outdated and waits (store/stale.go). There is deliberately no
// "rebuild everything" here: rebuilding a current dictionary gains nothing,
// and the one case where it might - suspected damage the stamps cannot see -
// is `wudict reindex -all`, from the command line.
//
// Shape: the background lane (indexLimit), one dictionary at a time, so it
// yields to everything the user does in the meantime; HoldActiveProcs for the
// whole job, because a person is watching a progress line and the Android
// shell keeps its foreground service up only while that hold is taken.
// Cancel takes effect between dictionaries: each rebuild is the usual atomic
// temp+rename, so stopping one halfway would only throw its work away.

// ReindexStatus is the job as the page - and `wudict reindex`, handing off to
// a running server - polls it. The zero value is "nothing has run": Total 0
// and not Running.
type ReindexStatus struct {
	Running bool `json:"running"`
	// Done of Total dictionaries finished (failed ones included).
	Done  int `json:"done"`
	Total int `json:"total"`
	// Current is the dictionary being rebuilt, with its own entry progress.
	Current      string `json:"current,omitempty"`
	CurrentDone  int    `json:"currentDone,omitempty"`
	CurrentTotal int    `json:"currentTotal,omitempty"`
	// Failed names each dictionary that could not be rebuilt, with why.
	Failed []string `json:"failed,omitempty"`
	// Canceled: the user stopped the job; what finished stays finished.
	Canceled bool `json:"canceled,omitempty"`
}

// reindexJob owns the single rebuild job. The zero value is usable.
type reindexJob struct {
	mu     sync.Mutex
	st     ReindexStatus
	cancel bool
}

func (j *reindexJob) status() ReindexStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.st
	st.Failed = append([]string(nil), j.st.Failed...)
	return st
}

// start claims the job for todo, or reports false when one is already running.
func (j *reindexJob) start(todo int) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.st.Running {
		return false
	}
	j.st, j.cancel = ReindexStatus{Running: true, Total: todo}, false
	return true
}

// stop asks a running job to end after the dictionary in hand.
func (j *reindexJob) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.st.Running {
		j.cancel = true
	}
}

func (j *reindexJob) canceled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cancel
}

func (j *reindexJob) update(f func(*ReindexStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	f(&j.st)
}

// entryOutdated is the per-dictionary test the job selects by. It agrees with
// dictInfo.Outdated by construction: a folder PreparedFor accepts is judged by
// store.Stale on its meta, and a folder it refuses (schema, source) is stale
// by the same function.
func entryOutdated(path string) bool {
	if !rebuildable(path) {
		return false
	}
	dir, ok := store.LookupDir(path)
	return ok && len(store.Stale(store.TextDBPath(dir), path)) > 0
}

// handleReindex starts the job over every outdated dictionary. 202 with the
// status when it starts, 200 with the status when there was nothing to do or
// a job is already running (the page then simply follows that one).
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	var todo []*entry
	for _, e := range s.reg.all() {
		if entryOutdated(e.Path) {
			todo = append(todo, e)
		}
	}
	if len(todo) == 0 || !s.reindex.start(len(todo)) {
		writeJSON(w, s.reindex.status())
		return
	}
	go s.runReindex(todo)
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.reindex.status())
}

// handleReindexStatus is the poll. Never fails, as the import poll never does.
func (s *Server) handleReindexStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.reindex.status())
}

// handleReindexCancel stops the job after the dictionary in hand.
func (s *Server) handleReindexCancel(w http.ResponseWriter, r *http.Request) {
	s.reindex.stop()
	writeJSON(w, s.reindex.status())
}

func (s *Server) runReindex(todo []*entry) {
	defer HoldActiveProcs()()
	defer s.reindex.update(func(st *ReindexStatus) {
		st.Running, st.Current, st.CurrentDone, st.CurrentTotal = false, "", 0, 0
	})
	for _, e := range todo {
		acquire(indexLimit)
		// the lane is FIFO and a rebuild takes minutes: the cancel may have
		// arrived while this slot was being waited for
		if s.reindex.canceled() {
			release(indexLimit)
			s.reindex.update(func(st *ReindexStatus) { st.Canceled = true })
			return
		}
		name := e.probeName()
		s.reindex.update(func(st *ReindexStatus) {
			st.Current, st.CurrentDone, st.CurrentTotal = name, 0, 0
		})
		last := time.Time{}
		progress := func(done, total int) {
			if time.Since(last) < 200*time.Millisecond {
				return
			}
			last = time.Now()
			s.reindex.update(func(st *ReindexStatus) { st.CurrentDone, st.CurrentTotal = done, total })
		}
		_, err := e.refresh(progress)
		release(indexLimit)
		s.reindex.update(func(st *ReindexStatus) {
			st.Done++
			if err != nil {
				st.Failed = append(st.Failed, name+": "+err.Error())
			}
		})
		if err != nil {
			logx.Warn("could not rebuild %s: %v", filepath.Base(e.Path), err)
		}
	}
}

// refresh rebuilds this dictionary's prepared data if, and only if, it is
// outdated, and reports whether it did. The text is rebuilt with the plan it
// already has (store.KeptPlan) - a rebuild the user asked for to bring data
// current must not change what they chose to index - and a media.db that was
// there before is repacked if the rebuild (or its own age) left it unpaired.
// A dictionary never packed is not given media it did not have.
func (e *entry) refresh(progress store.Progress) (bool, error) {
	e.ingestMu.Lock()
	defer e.ingestMu.Unlock()
	defer e.rebuilding.Store(false) // releasePrepared may have armed it
	if !rebuildable(e.Path) {
		return false, nil // no source: nothing to rebuild from
	}
	dir, ok := store.LookupDir(e.Path)
	if !ok {
		return false, nil // never prepared: nothing is outdated
	}
	textDB, mediaDB := store.TextDBPath(dir), store.MediaDBPath(dir)
	if len(store.Stale(textDB, e.Path)) == 0 {
		return false, nil // current - a concurrent rebuild got here first
	}
	name := e.probeName()
	hadMedia := fileExists(mediaDB)
	if stale := store.TextStale(textDB, e.Path); len(stale) > 0 {
		logx.V("%sprepared data is outdated (%v) - rebuilding", logx.Dict(name), stale)
		if err := e.rebuild(name, textDB, store.KeptPlan(textDB), progress); err != nil {
			return true, err
		}
	}
	if hadMedia && !store.MediaPaired(textDB) {
		e.dMu.RLock()
		cur := e.d
		e.dMu.RUnlock()
		if err := e.repackMedia(cur, textDB, mediaDB, progress); err != nil {
			return true, err
		}
	}
	_ = store.WriteInfo(dir)
	return true, e.reopen()
}
