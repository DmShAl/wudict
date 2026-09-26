// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"os"
	"strconv"

	"github.com/wuweidict/wudict/internal/dict"
)

// A prepared dictionary is frozen output of the code that built it. When that
// code changes, the data on disk does not follow, and nothing about it fails:
// it keeps answering queries with whatever the old code wrote. The versions
// below are how the running code says what it would write now, and Stale is
// the one place that compares the two.
//
// Each stamp covers one layer, so a bump invalidates exactly what it changed:
//
//	ingest_version   this file's IngestPlan: the schema's use, StripHTML, the
//	                 alias and redirect rules, body normalization
//	reader_version   one format's Reader (dict.RegisterReaderVersion)
//	markup_version   internal/artmark's role vocabulary (MarkupStale)
//	fold_version     dict.Fold, persisted only in the trigram index (FoldStale)
//	media_version    IngestMedia
//
// A missing stamp means the database predates the stamp, and is read as
// version 1 - the behaviour in force when the stamp was introduced - so the
// day this ships does not declare an entire library outdated.
//
// Stale reports; it never rebuilds. Rebuilding costs minutes to hours on a
// large library and is started only by the user (wudict reindex, or the
// panel's Rebuild), never on anyone's behalf (docs.local/PERF.md).

// IngestVersion identifies IngestPlan's behaviour. Bump it in the same commit
// as any change to what an ingest writes for the same Reader output; the
// golden tests in the format packages fail on such a change and say so.
const IngestVersion = 1

// MediaVersion identifies IngestMedia's behaviour, bumped the same way.
const MediaVersion = 1

// Reason is why a prepared dictionary no longer matches what this build would
// prepare from its source. The values are stable: the CLI prints them and the
// HTTP API reports them.
type Reason string

const (
	ReasonSchema Reason = "schema" // unreadable, or another schema: not usable at all
	ReasonSource Reason = "source" // the source file was edited or replaced
	ReasonAbbrev Reason = "abbrev" // the DSL abbreviation glossary changed
	ReasonIngest Reason = "ingest" // built by an older IngestPlan
	ReasonReader Reason = "reader" // built by an older Reader for its format
	ReasonMarkup Reason = "markup" // articles written with an older role markup
	ReasonFold   Reason = "fold"   // trigram index built by another dict.Fold
	ReasonMedia  Reason = "media"  // media.db unreadable, unpaired or outdated
)

// stampOf reads an integer version stamp, defaulting to def when the key is
// absent (a database written before the stamp existed) or not a number.
func stampOf(m map[string]string, key string, def int) int {
	if s := m[key]; s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

// Outdated is the code-version half of Stale, for a meta table already in
// hand: the reasons that depend only on which code built the database, not on
// the files beside it. Cheap - no I/O.
//
// Ingest and reader versions are compared as "older than": a library prepared
// by a NEWER build is not something this one can improve by rebuilding it.
// Markup and fold keep their established exact-match rule (MarkupStale,
// FoldStale), since either direction changes what a query or a stylesheet
// meets.
func Outdated(m map[string]string) []Reason {
	var out []Reason
	if stampOf(m, "ingest_version", 1) < IngestVersion {
		out = append(out, ReasonIngest)
	}
	if rv := dict.ReaderVersion(m["format"]); rv > 0 && stampOf(m, "reader_version", 1) < rv {
		out = append(out, ReasonReader)
	}
	if MarkupStale(m) {
		out = append(out, ReasonMarkup)
	}
	if FoldStale(m) {
		out = append(out, ReasonFold)
	}
	return out
}

// Stale reports every reason the prepared text.db for srcPath no longer
// matches what this build would prepare from it now. Empty means current.
// srcPath may be "" or gone: the reasons that need the source are then
// skipped, and the rest still reported - a caller that wants to rebuild checks
// the source itself.
func Stale(textDB, srcPath string) []Reason {
	m, schema, err := ReadMetaSchema(textDB)
	if err != nil || schema != schemaVersion {
		return []Reason{ReasonSchema}
	}
	return StaleMeta(m, textDB, srcPath)
}

// StaleMeta is Stale for a text.db whose meta the caller has already read
// successfully (and whose schema it has therefore already accepted).
func StaleMeta(m map[string]string, textDB, srcPath string) []Reason {
	var out []Reason
	if srcPath != "" {
		if sourceChangedMeta(m, srcPath) {
			out = append(out, ReasonSource)
		}
		companion, _ := dict.AbbrevCompanion(srcPath)
		if abbrevChangedMeta(m, companion) {
			out = append(out, ReasonAbbrev)
		}
	}
	out = append(out, Outdated(m)...)
	if sib := MediaSibling(textDB); sib != "" && fileExists(sib) && mediaStale(sib, m["dict_uuid"]) {
		out = append(out, ReasonMedia)
	}
	return out
}

// TextStale is Stale for the text.db alone: the reasons a rebuild of the
// text answers. Media staleness is the pack's business, judged after the text
// is current (a text rebuild can be what unpairs it).
func TextStale(textDB, srcPath string) []Reason {
	var out []Reason
	for _, r := range Stale(textDB, srcPath) {
		if r != ReasonMedia {
			out = append(out, r)
		}
	}
	return out
}

// ReadMetaSchema reads a wudict database's meta table together with its schema
// version (PRAGMA user_version). An error means the file cannot be read as a
// SQLite database with a meta table at all.
func ReadMetaSchema(dbPath string) (map[string]string, int, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, 0, err // never let the driver create an empty file
	}
	db, err := openRO(dbPath)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	var ver int
	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return nil, 0, err
	}
	m, err := readMeta(db)
	if err != nil {
		return nil, ver, err
	}
	return m, ver, nil
}

// PlanFromMeta is the Plan a prepared database was built with, read back from
// its meta - the same two facts Store.Open and the library listing read.
func PlanFromMeta(m map[string]string) Plan {
	return Plan{
		FullText: m["ingest_level"] != string(LevelHeadwords),
		Contains: m["has_trigram"] == "1",
	}
}

// KeptPlan is the Plan to REBUILD an existing text.db with: whatever it was
// built with, so a rebuild the user did not ask to change never changes it.
// Without this, every automatic rebuild - of a changed source, an outdated
// build, an unreadable file - quietly dropped the full-text and contains
// indexes the user had switched on. An unreadable database has no plan to
// keep, and gets the default (headwords only), which is also what a
// dictionary never prepared gets.
func KeptPlan(textDB string) Plan {
	m, err := ReadMeta(textDB)
	if err != nil {
		return Plan{}
	}
	return PlanFromMeta(m)
}

// MediaPaired reports that the media.db beside textDB is usable with it: it
// opens, carries the current media version, and names the text.db's
// dict_uuid. A media.db that is present but not paired is invisible to
// Store.mediaDB, so for every purpose that matters it is not packed.
func MediaPaired(textDB string) bool {
	sib := MediaSibling(textDB)
	if sib == "" || !fileExists(sib) {
		return false
	}
	uuid, err := ReadMetaValue(textDB, "dict_uuid")
	return err == nil && !mediaStale(sib, uuid)
}

// mediaStale reports that a media.db cannot serve the text.db stamped with
// uuid: unreadable, another schema, another dictionary's, or packed by an
// older IngestMedia.
func mediaStale(mediaDB, uuid string) bool {
	m, schema, err := ReadMetaSchema(mediaDB)
	if err != nil || schema != schemaVersion {
		return true
	}
	return uuid == "" || m["dict_uuid"] != uuid || stampOf(m, "media_version", 1) < MediaVersion
}

// keptUUID is the dict_uuid an ingest into dbPath should reuse, or "" for a
// fresh one. Reused only while the database there was built from the same,
// unchanged source: then the media.db beside it was packed from the same
// resources and stays paired across the rebuild, instead of being orphaned by
// a rebuild that did not touch a single resource. A changed source gets a new
// identity, and its media is repacked by whoever rebuilt it.
func keptUUID(dbPath, srcPath string) string {
	m, schema, err := ReadMetaSchema(dbPath)
	if err != nil || schema != schemaVersion || srcPath == "" {
		return ""
	}
	if !sameSource(m["source_path"], srcPath) || sourceChangedMeta(m, srcPath) {
		return ""
	}
	return m["dict_uuid"]
}
