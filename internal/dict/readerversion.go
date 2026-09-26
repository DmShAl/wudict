// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package dict

import "sync"

// A format's Reader decides what a prepared dictionary CONTAINS: which records
// become entries, how a body is decoded, which headwords become aliases. The
// result is persisted in text.db, so a fix to a Reader reaches a library only
// through a rebuild - and nothing would say that one is due. The reader
// version is how the running code says so: each format registers the version
// of its Reader's behaviour, ingest records it (meta reader_version), and a
// prepared dictionary built by a different version is reported as outdated
// (store.Stale).
//
// Bump a format's version in the same commit as any change to what its Reader
// yields. The golden test in that format's package fails on such a change and
// names this function; the bump and the new golden go in together.
var (
	readerVersionMu sync.RWMutex
	readerVersions  = map[string]int{} // key: Meta.Format ("mdx", "dsl", …)
)

// RegisterReaderVersion records the behaviour version of a format's Reader.
// Called from the format package's init, beside RegisterReader. v must be at
// least 1: a prepared dictionary with no recorded version is taken to be
// version 1, the behaviour in force when versioning was introduced.
func RegisterReaderVersion(format string, v int) {
	if format == "" || v < 1 {
		panic("dict: RegisterReaderVersion(" + format + "): version must be ≥ 1")
	}
	readerVersionMu.Lock()
	defer readerVersionMu.Unlock()
	readerVersions[format] = v
}

// ReaderVersion returns the registered behaviour version of a format's Reader,
// or 0 when the format registered none - which means "no opinion", never
// "outdated": a text.db whose format this binary cannot read cannot be
// rebuilt by it either.
func ReaderVersion(format string) int {
	readerVersionMu.RLock()
	defer readerVersionMu.RUnlock()
	return readerVersions[format]
}
