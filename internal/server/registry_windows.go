// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package server

// releasePrepared hands back the prepared databases an ingest is about to
// replace. A rebuild ends in os.Rename over the existing text.db (and media
// packing over media.db), and Windows refuses a rename over an open file -
// SQLite opens without FILE_SHARE_DELETE - so a held database would otherwise
// burn a minutes-long rebuild at its very last step with "Access is denied".
// Where the rename is legal this is a no-op (registry_other.go) and the entry
// serves its prepared view mid-rebuild, exactly as before.
//
// The holder is whichever backend this entry currently serves, and it is not
// always the upgraded wrapper: dsl and bgl embed their own store (they are the
// formats that auto-prepare on first open). Closed outright, not after
// closeGrace - the grace exists for in-flight readers, and none of them can
// exist in a useful sense once the data they read is being replaced.
//
// With the backend gone, opens are barred for the length of the work
// (entry.rebuilding): a search landing mid-rebuild reports errReindexing
// instead of resolving the entry afresh and re-opening - and re-holding - the
// database about to be replaced. Every caller holds ingestMu, so the janitor
// cannot drop or reopen anything here either.
func releasePrepared(e *entry, textDB string) {
	if !fileExists(textDB) {
		return // nothing on disk to rename over: a first ingest creates it
	}
	e.rebuilding.Store(true)
	e.dMu.Lock()
	old := e.d
	e.d, e.err, e.backing = nil, nil, ""
	e.dMu.Unlock()
	// A backend superseded moments ago (the preview a first prepare replaced -
	// dsl and bgl embed their own store) holds the same file until its grace ends.
	e.retired.closeAll()
	if old == nil {
		return // nothing served; the bar alone covers opens until the reopen
	}
	// Outside dMu, on the same terms as drop(): Close is not a quick call.
	old.Close()
	e.forgetStyles()
	scheduleReclaim()
}
