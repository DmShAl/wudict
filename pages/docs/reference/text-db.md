---
title: The text.db format
description: The on-disk SQLite format of a prepared wuDict dictionary - every table, every meta key, how article bodies are stored, and how to read or write one from another tool.
---

# The `text.db` format

A prepared dictionary is an ordinary **SQLite 3** database plus, optionally, a
second one for media. Nothing else: no proprietary container, no custom
compression scheme, no index you cannot rebuild. Any tool that can open SQLite
can read every article, and the rules on this page are all it needs.

This page is the export contract. It is written for the two readers who need
it: someone moving their dictionaries into another program, and a script or
agent doing it for them.

``` text title="one dictionary = one folder"
~/.wudict/db/Webster/
  text.db          articles + search indexes   ← everything on this page
  media.db         packed audio and images     (optional)
  media.link.db    a cache; safe to delete     (optional)
  info.txt         a human-readable receipt
  res/             your own replacement files  (optional)
```

`text.db` alone is a complete dictionary. Everything else is optional or
derived.

!!! info "Format version"

    `PRAGMA user_version` is **1**, and wuDict refuses to open a database with
    any other value. New indexes have been added without bumping it (see
    [Compatibility](#compatibility)), so a reader must feature-detect tables
    rather than assume they exist.

## Schema

``` sql title="text.db"
PRAGMA user_version = 1;

CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT);

CREATE TABLE entry(id INTEGER PRIMARY KEY,
                   w  TEXT NOT NULL,          -- display headword
                   m  TEXT NOT NULL);         -- article body (see below)
CREATE INDEX idx_entry_w ON entry(w COLLATE NOCASE);

CREATE TABLE alias(w        TEXT    NOT NULL, -- an alternative spelling
                   entry_id INTEGER NOT NULL REFERENCES entry(id));
CREATE INDEX idx_alias_w ON alias(w COLLATE NOCASE);

CREATE VIRTUAL TABLE entry_fts USING fts5(    -- rowid = entry.id
    w, txt, content='', columnsize=0,
    tokenize='unicode61 remove_diacritics 2');

CREATE VIRTUAL TABLE entry_trigram USING fts5(-- only when "contains" is on
    w, content='', columnsize=0, tokenize='trigram');
```

`entry` is the whole dictionary. The other tables are search machinery and can
be dropped and rebuilt from it.

### Reading articles

`entry.m` is **either plain HTML text or a DEFLATE-compressed BLOB**, decided
per row by its first byte:

| First byte | What the row holds |
| --- | --- |
| `0x00` | raw DEFLATE stream, starting at byte 1 |
| anything else | the article HTML itself, as text |

Article HTML never begins with a NUL, which is what makes the test safe. Both
forms occur in the same table — short articles (under 120 bytes) and ones that
did not compress are stored literally — so **a reader must handle both**.

``` python title="one row, either form"
import sqlite3, zlib

db = sqlite3.connect("file:text.db?mode=ro", uri=True)
for w, m in db.execute("SELECT w, m FROM entry ORDER BY id"):
    blob = m if isinstance(m, bytes) else m.encode()
    html = zlib.decompress(blob[1:], -15).decode() if blob[:1] == b"\x00" \
           else blob.decode()
    print(w, html[:80])
```

`-15` is raw DEFLATE with no zlib or gzip header — `zlib.decompress(blob[1:])`
without it will fail.

Compression is per row and independent: there is no shared dictionary or block
cache, so any row can be read on its own.
[`NO_COMPRESS`](configuration.md#no_compress) turns it off for future
preparations; reading always understands both forms.

### Headwords, aliases and sub-entries

- **`entry.w`** is the headword as it is displayed. Lookups compare
  `COLLATE NOCASE`, which is the collation both indexes are built with.
- **`alias`** holds every other spelling that must find this entry: StarDict
  `.syn` entries, MDX `@@@LINK` redirects, Slob aliases, DSL variant
  headwords. Redirects are resolved at preparation time, so an alias always
  points at a real `entry.id` — there are no chains to follow.
- A headword beginning with **`@`** (`@examples_woman`) is an MDict-style
  **sub-entry**: a fragment an article pulls in by link, never a word. It stays
  reachable by exact lookup, is excluded from browsing and substring search,
  and is *not* counted in `meta.entry_count` (`meta.sub_entries` counts them).
  Exporters should normally skip `w LIKE '@_%'`.

### The search indexes

Both FTS5 tables are **contentless** (`content=''`) and keyed by
`rowid = entry.id`; they store no article text of their own that you would
need to export.

| Table | Column | Holds | Present when |
| --- | --- | --- | --- |
| `entry_fts` | `w` | the headword | always |
| `entry_fts` | `txt` | the article with tags stripped | full-text search is on; empty string otherwise |
| `entry_trigram` | `w` | the **folded** headword | "contains" search is on |

*Folded* means lowercased, NFD-normalised, with combining marks dropped — so
`Café` is indexed as `cafe`. `meta.fold_version` records the rules used; a
mismatch only means the index is rebuilt, never that articles are wrong.

`entry_fts` exists even in headwords-only mode, because accent-insensitive
exact and prefix lookup go through it.

## `meta`

A flat key/value table. Unknown keys must be ignored; absent keys must be
treated as empty rather than as an error.

| Key | Meaning |
| --- | --- |
| `name` | **The title shown to the reader.** |
| `description` | **The text under *About this dictionary*.** Plain text or a small fragment of HTML; wuDict decides which by looking for a tag. |
| `format` | Where it came from: `mdx`, `stardict`, `slob`, `dsl`, `bgl`, `zim`, … |
| `entry_count` | Entries excluding sub-entries |
| `sub_entries` | How many `@`-prefixed fragments there are |
| `ingest_level` | `text` = article text is indexed, `headwords` = only headwords |
| `has_trigram` | `1` when `entry_trigram` was built (still feature-detect the table) |
| `body_encoding` | `deflate` or `plain` — a note for humans; the read path decides per row |
| `dict_uuid` | 32 hex characters; what pairs this file with its `media.db` |
| `index_lang`, `contents_lang` | Languages, only when the source declared them |
| `source_path`, `source_size`, `source_mtime`, `source_sha256_1M` | The file it was prepared from, for staleness checks |
| `created` | RFC 3339, UTC |
| `fold_version`, `markup_version` | Which text-folding and article-markup rules built this file |

`name` and `description` are HTML-unescaped on the way out, so a title stored
as `A &amp; B` displays as `A & B`.

## Article HTML

Bodies are stored **as the dictionary wrote them**, converted to HTML where
the source format was not HTML. In particular, references to media are left in
their original spelling:

``` html
<img src="pictures/lion.jpg">
<a href="sound://lion.mp3">🔊</a>
```

wuDict rewrites those to `/res/<dictionary id>/…` when it serves an article,
never in the database. An exporter therefore gets the original names and can
map them onto its own layout — look each one up in `media.db` (below), or
beside the original dictionary file.

## `media.db`

Written only when media is packed. Same SQLite conventions, same
`user_version`.

``` sql title="media.db"
PRAGMA user_version = 1;
CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT);   -- dict_uuid, name, format
CREATE TABLE resource(name TEXT PRIMARY KEY, mime TEXT, data BLOB);
```

- `resource.name` is the name as articles reference it. Resolution tries the
  exact spelling, then case-insensitively, then both Unicode normalisations —
  MDX resource names are stored lowercased while loose files keep their real
  spelling, and an article may use either.
- `resource.data` is the file's bytes, **not** compressed by wuDict.
- `meta.dict_uuid` **must** equal the `dict_uuid` in the `text.db` beside it.
  A pair that disagrees is refused, so a copied folder can never serve another
  dictionary's audio.

`media.link.db` is a different thing: a cache of *where* unpacked media lives
inside the original files. It is derived, never part of what you copy, and
deleting it is always safe.

## Exporting

The simplest route needs no SQL at all:

``` sh title="whole dictionary as CSV, pyglossary layout"
wudict dump -o out ~/.wudict/db/Webster
```

That writes `out/Webster.csv` — leading `"#key","value"` metadata rows, then
one row per entry: headword, article, and a third column of alternative
spellings when the entry has any — with resources unpacked beside it into
`Webster.csv_res`, which is the input layout
[pyglossary](https://github.com/ilius/pyglossary) converts to any format it
supports.

To read the database directly, the one query that produces a complete
dictionary is:

``` sql title="entries with their alternative spellings"
SELECT e.id, e.w, e.m,
       (SELECT group_concat(a.w, char(10)) FROM alias a WHERE a.entry_id = e.id) AS aliases
FROM entry e
WHERE e.w NOT LIKE '@_%'          -- skip sub-entry fragments
ORDER BY e.id;
```

…then decode `m` as shown [above](#reading-articles). Open the file read-only
(`file:…?mode=ro`) if wuDict may be running.

## Authoring a dictionary directly

Writing `text.db` yourself is a supported way to get data *into* wuDict — no
intermediate format, no conversion. Drop the finished folder into the library
folder (or any scanned folder) and it is found by its `text.db`.

The rules that are easy to get wrong:

- [x] `PRAGMA user_version = 1`.
- [x] Create `entry`, `alias` **and** `entry_fts` even for a headwords-only
      dictionary; leave `entry_fts.txt` as `''` when you do not index text.
- [x] Create both indexes exactly as shown, **`COLLATE NOCASE`**. Without that
      collation the query planner ignores them and every lookup scans a table
      whose rows carry article bodies inline.
- [x] `entry_trigram` stores the *folded* headword, not the raw one.
- [x] Bodies may be plain text — compression is optional, and a plain body is
      always legal.
- [x] Set `meta.name` and `meta.description`: they are the title and the
      *About this dictionary* text.
- [x] Set `meta.dict_uuid` (16 random bytes, hex) if you also write a
      `media.db`, and put the same value in both.
- [x] Leave `meta.source_path` out. If it names a file that exists, wuDict
      asks *that* file's format for the About text and your `description` is
      never shown.
- [x] Build into a temporary file and rename it into place, so a half-written
      database is never discovered.

`meta.format` is a free label; use something of your own (`native`) rather than
claiming to be `dsl` or `stardict`, whose articles wuDict knows it generated
itself and may offer to rebuild.

## Compatibility

- `user_version` stays at 1. Tables have been **added** without bumping it
  (`entry_trigram` was), so detect a table before using it:
  `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='entry_trigram'`.
- Unknown `meta` keys are ignored by every reader, including wuDict's. Adding
  your own is safe.
- A database missing an optional index still opens; it simply offers fewer
  search modes.
- FTS5 is **required** — a SQLite build without it cannot open a `text.db` at
  all. The trigram tokenizer needs SQLite 3.34 or newer.

[The library folder](../dictionaries/library.md){ .md-button }
[CLI reference](cli.md){ .md-button }
