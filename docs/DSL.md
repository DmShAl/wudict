# Lingvo DSL — working reference

**Status markers**

| Marker | Meaning |
| --- | --- |
| `[V]` | VERIFIED — read in our code, or observed in a real dictionary/run in this repo. |
| `[D]` | DOCUMENTED, NOT TESTED HERE — stated by a Lingvo reference; no fixture exercises it. |
| `[I]` | INFERRED — reasoned from the format or from adjacent behaviour. Treat as a hypothesis. |

**Sources cited**

- **lingvo-ref** — *«Язык DSL. Справочное руководство»*, the community reference
  manual at `~/projects/language/dsl-language` (a maintainer-local copy,
  not in this repo). It is the most complete DSL description that exists: it
  covers every tag chapter by chapter with Lingvo-version history, the
  metacharacter table, the preprocessor directives, the compiler's error report,
  the colour palette, the supported-language and ANSI code-page tables, and the
  sorting/indexing rules. Chapter titles are cited in the code comments and
  below, in Russian, exactly as the manual spells them.
- ABBYY's own *Lingvo Content* / DSL compiler help — the manual's own upstream;
  thinner, and wrong in places the manual documents (x6 help claims `[ref]`
  targets are not verified at compile time; they are).
- https://github.com/yozhic/DSL-Reference — an English condensation of the same
  material; nothing in it is absent from lingvo-ref.
- http://lingvo.helpmax.net/en/troubleshooting/dsl-compiler/inserting-pictures-and-sounds/
- goldendict-ng `src/dict/dsl.cc`, `dsl_details.cc` — the de-facto second
  implementation; cited where its behaviour, not Lingvo's, is what dictionaries
  in circulation were written against.

A rule that is in **lingvo-ref only** is still normative here: the corpus was
authored against Lingvo's compiler, so its acceptance is the specification.

---

## 1. Containers and companions

| File | Role | wudict |
| --- | --- | --- |
| `<name>.dsl` | main text | `[V]` opened by `dsl.Open` / `dsl.NewReader` |
| `<name>.dsl.dz` | dictzip-compressed main text | `[V]` sequential gunzip via `compress/gzip` (`Reader.init`) |
| additional `.dsl` named by `#INCLUDE` | extra text files spliced in at compile time | `[V]` read after the main file, in directive order (§3.1) |
| `<name>.dsl.files.zip` | GoldenDict's resource archive | `[V]` first source in `MediaSources` |
| `<name>.dsl.files/` | resource folder (also `<name>.files/` for a `.dz`) | `[V]` second source, walked and indexed |
| beside the `.dsl` | loose resources | `[V]` last source, `NewDirExact` — exact paths only, never walked or listed (the folder is not the dictionary's own; other dictionaries live there) |
| `<name>.ann` | annotation/about text, `#LANGUAGE`-partitioned | `[V]` **read live** for the About surface (§1.2); never ingested |
| `<name>_abrv.dsl` | abbreviations shown on hover | `[V]` **absorbed** into the parent at ingest (§1.1) and hidden from discovery |
| `<name>.bmp` | dictionary icon | `[V]` **ignored** (`#ICON_FILE` likewise parsed into the header map and unused) |
| `<name>.lsd` | Lingvo's compiled binary form | `[V]` not supported, not planned |

Resolution order is `MediaSources(srcPath)` in `internal/format/dsl/dsl.go`, and it
is the O8 provider registered for the format, so a *prepared* dictionary reaches its
media from the recorded source path without reopening/reparsing the `.dsl`. `[V]`

A `.dsl.dz` names its resources after either the compressed file or the `.dsl`
inside it; both spellings exist in the wild and both are tried. `[V]`

### 1.1 The `_abrv` companion `[V]`

In Lingvo a `<name>_abrv.dsl` is not a dictionary: it is the glossary that
supplies the expansion shown on hover over a `[p]…[/p]` label ("plural" over
`pl`). It is a full DSL file with its own mandatory header — `#NAME`,
`#INDEX_LANGUAGE`, `#CONTENTS_LANGUAGE` are required there too, and
`#SOURCE_CODE_PAGE` is allowed; `#INCLUDE` and `#ICON_FILE` are not. `[D]`
wudict follows Lingvo and never lists it.

- **Pairing** — `dict.AbbrevCompanion` / `dict.IsAbbrevCompanion`
  (`internal/dict/companions.go`), name-based, case-insensitive, both `.dsl`
  and `.dsl.dz` spellings. **Orphan rule:** a `*_abrv.dsl` is a companion only
  when a sibling `<stem>.dsl{,.dz}` exists, so a genuine standalone
  abbreviation dictionary keeps working. An explicit path
  (`wudict lookup x_abrv.dsl`) always opens, like an `.mdd`.
- **Hiding** — `dict.Discover` skips companions, and the server's
  `libraryPaths` skips a library folder whose recorded source is one, which
  retires folders prepared by an older build (the folders stay on disk; removal
  is the user's call through the panel).
- **Absorbing** — `internal/format/dsl/abbrev.go` loads the companion with the
  same reader (headword → plain-text expansion, exact key plus a case-folded
  fallback) and `closeLabel` bakes a hit into the article as
  `<abbr class="wu-abbr" title="…">`. Baking at ingest is what makes the
  tooltip free everywhere: shadow DOM, sandboxed iframe, the Android WebView,
  `-format clean` (both the element and `title=` survive) and `wudict dump`,
  with no client code. A miss emits exactly the pre-existing bytes.
- **Bounds** — companion over 8 MiB ignored; 20 000 keys; 200 runes per
  expansion; self-referential and empty entries dropped. A malformed companion
  is a `-v` note, never an ingest failure.
- **Staleness** — the parent records `abbrev_path`/`_size`/`_mtime`/`_count` in
  `meta` (via the reader's optional `ExtraMeta`), and `store.AbbrevChanged`
  compares them, treating present↔absent as changed. The server re-indexes on a
  mismatch through the background index lane, one dictionary at a time.

### 1.2 The `.ann` annotation `[V]`

A `<name>.ann` beside the dictionary is its editorial blurb — publisher,
edition, copyright, often in several languages at once. It is *display text*:
it is part of no article, so unlike the `_abrv` companion (§1.1) it is **never
ingested, never baked and never a staleness input**. It is read live whenever
someone asks for it, which costs nothing until they do.

- **Path** — chop `.dsl` / `.dsl.dz`, append `.ann`; `.ANN` is tried too, for a
  Windows-authored set on a case-sensitive filesystem. `internal/format/dsl/ann.go`,
  registered as a `dict.RegisterAbout("dsl", …)` provider — a path-only,
  per-format registry (`internal/dict/about.go`) modelled on `resource.Register`.
  An `_abrv.ann` is never read: the companion is not a dictionary, so nothing
  asks it for an About.
- **Encoding** — `decodedScanner` + `detectEncoding`, shared with the
  dictionary reader (§2). Every `.ann` in the reference corpus is UTF-16LE with
  a BOM; CRLF is stripped. Gzip is sniffed from the magic bytes, not the name.
- **`#LANGUAGE` sections — all of them, in file order.** In an `.ann` the
  directive may sit on **any** line, not only the first, and each one opens a
  section that runs to the next directive or to EOF (lingvo-ref «Директива
  #LANGUAGE»). Lingvo shows only the section matching its UI language (English
  when the UI language has no section), and GoldenDict does not process the
  directive at all. `[D]` We keep **every** section and head it with its
  language name: picking one hides the Russian annotation of a Ru-Ru dictionary
  from a reader running an English UI. `[V]`
- **Bounds** — 256 KiB cap, truncated with `…` rather than refused; a missing,
  unreadable, empty or heading-only file is simply "no annotation", never an
  error.
- **Where it surfaces** — `GET /api/about?dict=<id>` (same-origin only; not in
  the D69 CORS allowlist, because the response names a file on the user's
  disk), the panel card's **About** disclosure, and `wudict info`. The
  `#INDEX_LANGUAGE → #CONTENTS_LANGUAGE` string the header synthesises
  (`reader.go`) is the **fallback**, shown when a dictionary ships no `.ann`.

## 2. Encodings

`detectEncoding` (`reader.go`), BOM first: `[V]`

| BOM | Encoding |
| --- | --- |
| `FF FE 00 00` | UTF-32LE |
| `00 00 FE FF` | UTF-32BE |
| `FF FE` | UTF-16LE |
| `FE FF` | UTF-16BE |
| `EF BB BF` | UTF-8 |
| none, sample is valid UTF-8 | UTF-8 |
| none, sample is not valid UTF-8 | the single-byte page `#SOURCE_CODE_PAGE` names, else the one the declared languages imply, else Windows-1252 (`codepage.go`) |

**`#SOURCE_CODE_PAGE` is honoured.** `[V]` It names a Windows ANSI code page and
is required only for ANSI files; Lingvo ignores it on a Unicode source
(lingvo-ref «Директива #SOURCE_CODE_PAGE»), and so do we. The documented values
and the pages they select (`codepage.go`, from the manual's chapter «Кодовые
страницы ANSI в Windows»):

| Value | Page | Value | Page |
| --- | --- | --- | --- |
| `EasternEuropean` | windows-1250 | `Turkish` | windows-1254 |
| `Cyrillic` | windows-1251 | `Arabic` | windows-1256 |
| `Latin` | windows-1252 | `Baltic` | windows-1257 |
| `Greek` | windows-1253 | `Thai`/`Hebrew`/`Vietnamese` | windows-874/1255/1258 — the manual's three unnamed rows, accepted under their MSDN names |

Lingvo requires the documented casing exactly and errors on `"english"`; we match
case-insensitively, because refusing a spelling here would mean falling back to
Windows-1252 and mojibake, not a diagnostic. `[V]` Every non-BOM verdict must also
**decode into lines** — a DSL that comes out as one unbroken token was decoded
wrong, whatever the byte statistics said. `[V]`

Line ends: `\r` is trimmed per line, and a leading U+FEFF is trimmed from the
first line of every source, including each `#INCLUDE` file, which carries its
own BOM. `[V]`

## 3. Header directives

Syntax (lingvo-ref «Директивы предварительной обработки»): `#` in **column 0**
— a space or anything else before it is a compile error — then the keyword,
then one or more spaces **or tabs** (Lingvo's own samples use a tab; splitting
on `" "` alone dropped the line), then the value, normally in `"` quotes. One
directive per line; blank lines between them are allowed; their order is free
(`#NAME`, `#INDEX_LANGUAGE`, `#CONTENTS_LANGUAGE` first is a recommendation,
not a rule). `[D]` `[V]` (`Reader.init`, `parseDirective`)

Three are **mandatory** in a main or `_abrv` file and the compiler refuses the
dictionary without them: `#NAME`, `#INDEX_LANGUAGE`, `#CONTENTS_LANGUAGE`. `[D]`
We require none of them — a reader that refuses a file Lingvo would refuse
simply loses a dictionary the user already has.

| Directive | Lingvo meaning | wudict |
| --- | --- | --- |
| `#NAME` | dictionary title, shown in the card header and every dictionary list | `[V]` → `Meta.Name`; falls back to `#FULL_NAME`, then to the file base name. Also the value a `[ref dict="…"]` names (§6.1) |
| `#INDEX_LANGUAGE` | language of the **headwords**, an ISO-639-2 English name, case-sensitive | `[V]` → `Meta.IndexLang` via `lang.FromDeclared` (absorbs collation names like `SpanishModernSort`), and into `Description`; case-insensitive here |
| `#CONTENTS_LANGUAGE` | language of the definitions, same value space | `[V]` → `Meta.ContentsLang` and `Description` |
| `#SOURCE_CODE_PAGE` | ANSI code page, only for ANSI files | `[V]` **honoured** (§2) |
| `#INCLUDE "path"` | splice another `.dsl` at compile time; backslashes **doubled**; absolute or relative | `[V]` **implemented** (§3.1) |
| `#ICON_FILE` | icon path (undocumented by ABBYY; x5+) | `[V]` parsed, unused |
| `#FULL_NAME` | Lingvo 6.0/7.0, undocumented: the dictionary name, superseded by `#NAME` in 8.0 | `[V]` accepted as a `#NAME` fallback |
| `#LANGUAGE` in a **main** file | Lingvo 6.0/7.0, undocumented: the headword language, superseded by `#INDEX_LANGUAGE` in 8.0 | `[V]` accepted as an `#INDEX_LANGUAGE` fallback |
| `#LANGUAGE` in an `.ann` | partitions the annotation by UI language, on any line | `[V]` every section kept and shown, none selected by locale (§1.2) |

A `#` in column 0 is a directive **wherever it appears**, because the headword
rule (§5) excludes `#` from the characters a headword may start with. A line we
do not recognise is skipped, never indexed: read as a headword it produced a
phantom entry *and* swallowed the article that followed it. `[V]`

### 3.1 `#INCLUDE` `[V]`

- The value is a Windows path whose backslashes are **doubled** in the source
  (`"Extra\\more.dsl"`, `"..\\Thesaurus\\syn-2.dsl"`, `"c:\\Lingvo\\x.dsl"`).
  We unescape `\\`→`\`, then read `\` as a separator, so both conventions work.
- Relative paths resolve against the **including file**, not the main one — a
  chain of includes need not share a folder — and a nested `#INCLUDE` inside an
  included file is honoured, resolved against that file.
- A path that does not resolve is retried as its **base name in the including
  file's folder**, which is where a dictionary copied off its author's machine
  actually keeps its parts; an absolute `c:\Lingvo\…` reaches that fallback and
  nothing else.
- Each file is opened **at most once** (absolute-path set), so an include cycle
  terminates instead of looping.
- Each include is decoded on its own (`decodedScanner`): a UTF-8 part beside a
  UTF-16 main file is read correctly.
- Files are read **after** the main file's own entries, in directive order.
  Lingvo concatenates at compile time and the entry order in the compiled `.lsd`
  is not observable, so this is a free choice. `[I]`
- A missing include is a **warning**, not a failure: Lingvo would refuse to
  compile, but a reader that refuses the dictionary turns one absent file into
  no dictionary at all.

## 4. Entry-block grammar

`Reader.Next` / `parseBlock`: `[V]`

```
headword line          ← column 0 (no leading space/tab, no leading '#')
headword line          ← consecutive col-0 lines = MORE HEADWORDS FOR THE SAME CARD
<TAB>body line
<TAB>body line
<blank>                ← blank lines are skipped, they do not terminate a block
headword line          ← a col-0 line AFTER body lines starts the next block
```

- Every body line must begin with a space or a tab (lingvo-ref «Тело статьи»);
  an unindented line is by definition the next headword.
- A blank line **between** two headwords of one card is a compile error in
  Lingvo ("no article body"). `[D]` We skip blank lines wherever they are, so
  such a file still reads. `[V]`
- Blank lines never separate blocks; a col-0 line after at least one body line
  does. A pushback buffer (`r.buffered`) holds that line for the next `Next()`.
- Every headword of a block becomes a lookup key; `terms[0]` — the first line's
  fully-expanded variant — is the one `~` substitutes in the body.
- Scanner limit: 16 MiB per line, 1 MiB start buffer. A longer line fails the read.
- A `{{…}}` comment that opens on one line and closes on another is removed
  **before** any of this classification happens (§6.4), because such a zone may
  span cards and swallow the headwords between them.

### Sub-entries (`@`)

Line shape (`atSignHeading`, `reader.go`; mirrors `isAtSignFirst` in
goldendict-ng `src/dict/dsl_details.cc`): `[V]`

```
^[ \t]*(?:\[[^\]]+\][ \t]*)*@
```

- The `@` must be first on the line **after** leading whitespace **and after any
  leading DSL tags** — `[m1]@ heading` and `[m3]@` are legal and appear in real
  dictionaries. Those tags are discarded; they do not become part of the heading.
- The space after `@` is **optional**: `@heading` == `@ heading`.
- Heading text = everything after the `@`, whitespace-trimmed.
- An **empty** heading (`@` alone, tags aside) closes the current sub-card.
- `\@` is not a sub-card line (the regexp cannot reach a `\`), which is also how a
  literal `@` is written inside a body. An unescaped `@` that is *not* first on its
  line is a compile error in Lingvo; GoldenDict warns and keeps it as text; we keep
  it as text silently. `[D]`/`[V]`

**Piled headings.** Consecutive `@` lines with no body line between them are several
headings for **one** card — the discriminator is "have any body lines been seen since
the card opened", not the heading count (`linesInCard`, and `linesInsideCard` in
goldendict-ng `src/dict/dsl.cc`). `[V]`

```
@ dictionary making        ← opens a card
@ dictionary compiling     ← same card, second heading
[m1]body                   ← the card's body
@ next                     ← body seen ⇒ closes the previous card, opens a new one
```

**`~` in a heading** is the parent's first headword, substituted before the
heading is parsed (`expandTitleTilde`; goldendict-ng calls this `expandTildes`).
The parent is escaped on the way in with a **title-safe** escaper — `\ [ ] ~ ( )
{ }` — because `(` and `{` are syntax in a headword and inert in a body: a
parent like `(the) sun` substituted raw would re-enter the title parser as an
optional part and index the child under keys the dictionary never declared. `[V]`

Each sub-entry becomes a **separate `dict.Entry`** carrying every heading's every
key as a headword (heading order, fully-expanded variant first), and the parent
gets one back-reference line **per key**: `[V]`

```
\t[m2]- [ref]<escaped key>[/ref][/m]
```

The leading `- ` is deliberate: Lingvo and GoldenDict both draw a hyphen before each
sub-card link. `[V]` A heading with an optional part therefore produces one link per
variant — GoldenDict does the same via `expandOptionalParts` in `ArticleDom`
(`dsl_details.cc`), and `{…}` unsorted parts are stripped from the key by
`processUnsortedParts` there and by `transformTitle` here. `[V]` The key is
re-escaped for the second DSL pass (`dslEscape`: `\ [ ] ~ < > @`).

Lingvo itself renders the sub-card inline-collapsed and lists its title in the
headword list. `[D]` Same user-visible outcome (title in the index, click to read),
different mechanism.

## 5. Headword grammar

A headword is **any line whose first character is not a space, a tab or `#`**
(lingvo-ref «Заголовок статьи»). Maximum length **246 characters**, counting
escapes and the contents of `(…)` and `{…}` but not trailing spaces past the
246th non-space character; over the limit the compiler drops the whole entry,
and in rare "fragmented" headwords it refused above ~236. `[D]` We impose no
limit. `[V]`

`transformTitle` (`title.go`) returns the lookup keys and one display form: `[V]`

| Construct | Keys | Display (HTML) |
| --- | --- | --- |
| plain text | kept | escaped |
| `(…)` optional part | **both** variants, for **every** part independently | brackets kept, as Lingvo renders it |
| `{…}` unsorted part | omitted | rendered as DSL markup (a `[']` stress mark, `[c]`, `[s]`, `[br]`…) |
| `{{…}}` comment | omitted | omitted |
| `\x` | literal `x` | literal `x` |

- **Optional parts multiply.** `(пре)вращать(ся)` is **four** headwords —
  превращаться, вращаться, превращать, вращать — not two (lingvo-ref
  «Заголовок статьи»); *n* parts give 2ⁿ keys, emitted fully-expanded first so
  `Keys[0]` stays the canonical form that `~` mirrors and that the sub-card
  back-reference points at. Duplicates are dropped. Past
  `maxOptionalParts` (6, i.e. 64 keys) the line falls back to the two extremes,
  all-in and all-out, so one pathological line cannot become millions of index
  rows. `[V]`
- `(` does not nest; a second one is literal. `)` outside a paren is literal. `[V]`
- `{…}` may sit **inside** `(…)` and vice versa — one flat loop with an `inParen`
  flag, not a paren scanner, precisely because `(слов{[']}а{[/']}рной)` exists. `[V]`
- The unsorted part is what Lingvo hides from the headword **list** while still
  drawing it in the **card**, and it is excluded from search (lingvo-ref); the
  space that separates it may sit inside or outside the braces
  (`{to }go away` == `{to} go away`). Keys get interior whitespace collapsed
  (`collapseSpace`), Display does not — otherwise removing an unsorted part
  leaves a key with a double space nobody can type. `[V]`
- Keys are stored **raw**, not XML-escaped (a deliberate deviation from
  pyglossary); only Display is escaped. `[V]`
- When Display differs from the escaped first key, it is prepended to the body as
  `<b>…</b>`, several titles joined by `<br/>`. `[V]`
- Multiple headword lines are all indexed; Lingvo shows only the one the reader
  arrived by in the card. `[D]` We show the display forms of all of them. `[V]`

**Sorting** (Lingvo's headword list, for context — wudict sorts in SQLite, not
here): alphabet first and case-insensitively, so «аврал» < «Аврора» and
«ерунда» < «ёрш»; ties by Unicode code point, so `-` and `_` float to the top,
numbers sort `1, 10, 100, 2`, «Аврора» < «аврора», and Latin precedes Cyrillic.
`[D]`

## 6. Tags and commands

Tag names are **lower case** — `[REF]` is not `[ref]` — and a tag may not nest
inside itself (`[b][b]x[/b][/b]` is a compile error). `[D]`

Columns: Lingvo semantics `[D]` unless noted → our output (`transform.go`,
`processTag`/`closeTag`) → status. Every class below is from the
`internal/artmark` vocabulary (`wu-` prefix); colours and indents ride as the
custom properties `--wd-c` / `--wd-m`, so a reader's own stylesheet wins without
`!important`, and `-format clean` keeps the classes because they are wudict's own.

| Tag | Lingvo | wudict output | Status |
| --- | --- | --- | --- |
| `[b]` | bold | `<b>` / `</b>` | `[V]` |
| `[i]` | italic | `<i>` / `</i>` | `[V]` |
| `[u]` | underline | `<u>` / `</u>` | `[V]` |
| `[']` | stress mark on the enclosed vowel; forbidden inside `[ref]`/`<<…>>` | `<span class="wu-acc">` / `</span>` | `[V]` |
| `[c]`, `[c <colour>]` | colour; bare = green; the value is a bare attribute, from Lingvo's named palette or a `#hex` | `<span class="wu-c" style="--wd-c:…">`; an unrecognised value is dropped rather than passed into a style attribute | `[V]` |
| `[sup]` `[sub]` | super/subscript | `<sup>` `<sub>` | `[V]` |
| `[m]`, `[m0]`…`[m9]` | left margin, N ems; bare `[m]` = no shift | `<p class="wu-m" style="--wd-m:N">`, bare/`[m0]` without the property; `[/m]` and `[/mN]` → `</p>` | `[V]` |
| `[br]` | line break, **no closing descriptor** (x5+); in a headword it must be wrapped in `{…}` | `<br/>` | `[V]` |
| `^` | invert the case of the next character ("перевёртыш", 6.0+), chiefly as `^~` | rune-aware case flip; `^~` flips the first letter of the mirrored headword; `^` before markup or at EOF disappears | `[V]` |
| `[p]` | grammatical/usage label | buffered, then `<span class="wu-p">`, wrapped in `<abbr title="…">` when the `_abrv` companion knows it (§1.1) | `[V]` |
| `[t]` | phonetic transcription | `<span class="wu-ipa">` / `</span>` | `[V]` |
| `[*]` | secondary/optional zone, hidden behind Lingvo's toggle | `<span class="wu-sec">` / `</span>` (always shown) | `[V]` |
| `@` | sub-card | see §4 | `[V]` |
| `[ex]` | example | `<span class="wu-ex">` / `</span>` | `[V]` |
| `[com]` | editorial comment | `<span class="wu-com">` / `</span>` | `[V]` |
| `[trn]` `[!trn]` `[trs]` `[!trs]` | include/exclude from translation/transcription indexing | `<span class="wu-trn">`, `wu-trn-not`, `wu-trs`, `wu-trs-not` | `[V]` |
| `[trn1]` | x5 variant of `[trn]` | unknown tag → dropped, content kept (same visible result) | `[V]` |
| `[lang id=…]`, `[lang name="…"]` | mark a language span; the id is a Lingvo language code | `<span class="wu-lang" data-lang="<raw>" lang="<BCP-47>">` when the code or name is known (`lang.go`) | `[V]` |
| `[s]` | **multimedia zone** — image, sound or video | see §7 | `[V]` |
| `[video]` | undocumented x5 synonym of `[s]` | identical to `[s]` | `[V]` |
| `[preview]` | undocumented, legal only inside `[s]`/`[video]`, no effect | consumed inside the media zone, dropped outside | `[V]` |
| `[ref]`, `[ref dict="…"]` | link to a headword in this or **another** dictionary | see §6.1 | `[V]` |
| `<<…>>` | inline form of `[ref]`, no attributes | same as a bare `[ref]` | `[V]` |
| `[url]` | external link (`http://`, `https://`, `www.`, or mail) | `<a href="…">`, `http://` prefixed when the value has no `://` | `[V]` |
| `{{…}}` | comment, removed before compilation; may span lines | §6.4 | `[V]` |
| unknown tag | compile error in Lingvo | **dropped, content kept** — pyglossary logs a warning, we do not | `[V]` |

Nesting is by output only: we emit open and close markup as tags arrive and never
build a tree, so unbalanced DSL yields unbalanced HTML rather than an error. `[V]`
The renderers parse into a shadow root or an iframe, where the browser closes it.

### 6.1 `[ref]` and the cross-dictionary link `[V]`

`[ref]` has exactly **one** attribute in Lingvo: `dict="…"`, whose value is the
**`#NAME` of the target dictionary**, spelled exactly as that dictionary's own
header spells it (lingvo-ref «Тэг [ref]···[/ref]»). With it the link leaves the
dictionary it was written in; Lingvo draws a hover tooltip naming the target
dictionary and no other marker.

```
адгезивы
	[m1]То же самое, что и [ref]клеи[/ref] — вещества…[/m]
	[m1][p]См. тж.[/p] [ref dict="Справочник реставратора (Ru-Ru)"]адгезивы в реставрации[/ref][/m]
```

What we emit:

| Form | HTML |
| --- | --- |
| `[ref]word[/ref]`, `<<word>>` | `<a href="bword://word">word</a>` |
| `[ref dict="D"]word[/ref]` | `<a class="wu-xref" data-dict="D" title="D" href="bword://word">word</a>` |
| `[ref target="t"]word[/ref]` | `<a href="bword://t">word</a>` — `target=` is **not** Lingvo's; it is a GoldenDict-era extension we keep because dictionaries in circulation use it |

- `title=` carries the dictionary name because that is exactly Lingvo's own
  tooltip, and because `title` is one of the few attributes `-format clean`
  keeps. `data-dict` is the machine-readable copy; it is allowlisted on `<a>` in
  `internal/server/articleformat.go` (inert data, no URL, no behaviour) so that
  sanitising an article cannot silently retarget the link.
- **Resolution in the UI** (`index.html` `dictIDByName`, `frame.js` posts
  `xdict`): the name is matched against each installed dictionary's `#NAME` and
  its displayed label, case- and whitespace-insensitively. A hit scopes the
  search to that dictionary for one search only (`scopeOnce`); a **miss searches
  everything** — never the dictionary the link came from, which is the one place
  the target certainly is not.
- Target-spelling rules a dictionary author must follow, and what they mean here:
  a target with an optional part must be named **resolved** (`[ref]вдохновить[/ref]`,
  not `[ref]вдохновить(ся)[/ref]`); escaped parens in the target must be
  reproduced; an unsorted `{…}` part must be omitted; and Lingvo matches
  **case-sensitively**. `[D]` Our `bword://` lookup goes through the store's own
  headword index, which is case-insensitive and therefore strictly more
  forgiving. `[V]`
- The target text must fill the whole zone — no spaces between it and the
  descriptors — and `[']` may not be used inside a link zone. `[D]`
- `[ref]` and `[url]` are legal in a headword only inside an unsorted `{…}`
  part. `[D]`
- Since x5 the compiler verifies `[ref]` targets and reports dead ones (the x6
  help says otherwise and is wrong). `[D]`

### 6.2 Metacharacters `[D]`

Escape with `\`; the character is then literal and is not indexed as syntax.

| Where | Metacharacters |
| --- | --- |
| body **and** headwords | `[` `]` `@` `#` `\` `~` `^` `<<` `>>` `{{` `}}` (single `<` `>` are literals) |
| headwords **only** (literal in a body) | `(` `)` `{` `}` |

Square brackets have a second escape: **doubling**. `[[…]]` is a literal pair,
but a doubled bracket may not be followed by a tag — `[[[t]` is a compile error;
write `[[ [t]` or `\[[t]`. `[V]` We fold `[[` → `[` and `]]` → `]`.

### 6.3 Character-level rules (`transformer.run`) `[V]`

| Input | Result |
| --- | --- |
| `\x` | literal `x` (escapes `[`, `]`, `\`, `~`, `@`, `^`, `#`, `(`, `)`, `{`, `}`, `<`, `>`) |
| `\ ` (backslash-space) | `&nbsp;` |
| `\<\<` / `\>\>` | literal `<<` / `>>`, escaped for HTML |
| trailing lone `\` at EOF | literal backslash |
| `~` | the block's first headword, HTML-escaped |
| `^~` | the same, with its first letter's case inverted |
| `^x` | `x` with its case inverted (rune-aware) |
| `^` before `[` or `\`, or at EOF | dropped — it has nothing to act on |
| `[[` / `]]` | literal `[` / `]` |
| lone `]` with no opening `[` | passed through as-is (pyglossary parity) |
| `[` never closed | the rest of the input as literal text |
| `[]`, `[ ]`, `[/]` | literal text (real articles contain `([ ])`) |
| newline | leading spaces/tabs of the next line are skipped, then `<br/>` — **unless** the next thing is `[m`, whose `<p>` provides the break |
| `<` not followed by `<` | `&lt;` |

Attribute lexing accepts quoted (`'`/`"`) and unquoted values, backslash escapes
inside them, and is EOF-tolerant. A bare attribute with no `=` is recorded with an
empty value — which is how `[c red]` finds its colour. `[V]`

### 6.4 `{{…}}` comments `[V]`

Per lingvo-ref «Тэг {{···}}»: usable **anywhere** in a DSL file; no DSL
construct works inside one, **not even the escape character** (`{{c\}}` closes
and compiles, `{{c}\}` does not close and is reported); single braces inside a
zone are literals; a zone may **span several lines**, and any headword caught
between the opening and the closing pair is ignored by the compiler; where a
comment and any other tag overlap, **the comment wins** (in
`<<word {{ word>> comment}}` the link's closing `>>` is eaten by the comment,
and the compiler then reports the broken link, not the comment). Comments exist
only in the source: compilation drops them.

That last clause is the whole rule, and it is stronger than "remove the comment
text". A comment is removed **before the line is anything** — before it is a
headword, a body line or a directive — so:

- a comment standing alone on a line at **column 0** is not a headword (it
  leaves an empty line, and the body lines after it still belong to the card
  above);
- an **indented** one is not a body line;
- one between the directives and the first card is neither.

`stripLineComments` (`reader.go`) therefore removes **every** zone, single-line
and spanning alike, from each **raw line before classification**, returning the
open/closed state so a zone carries to the next line; `blankLine` then drops
whatever is left of a comment-only line. It runs in **both** line loops — the
header scan in `init` and `nextLine` — over one shared `inComment` flag, because
a licence or authoring note between the directives and the first card is a
standard placement.

Nothing later in the pipeline can substitute for this, which is why the earlier
"strip only zones that span lines" split was wrong: by the time an entry exists,
the comment has already decided which entry the surrounding lines belong to, and
a comment-only line at column 0 has already been counted as a headword. The
symptom was `dsl: entry block without headword`.

`stripComments` (`transform.go`) still runs on a body, and `transformTitle` on a
headword line: they are the entry points for a fragment that never came through
the reader (an `_abrv` expansion, a sub-card heading, a direct `transformBody`).
`stripComments` carries the rule that a comment alone on its line takes the line
with it, so it cannot leave a stray `<br/>`.

An unterminated `{{` with no `}}` anywhere consumes the rest of the file, which
is what the compiler does too; an escaped `\{\{` opens nothing.

### 6.5 Whitespace `[D]` unless marked

| Rule | lingvo-ref | wudict |
| --- | --- | --- |
| Any run of spaces in a headword **or** a body collapses to one space ("правило сокращения пробелов", explicitly modelled on HTML). The same applies to an `.ann`. | «Словарная статья» | `[V]` keys are collapsed (`collapseSpace`); the body is left alone and **the browser applies exactly this rule** when it renders, so the result matches without our touching the text |
| Non-standard spaces — U+00A0, U+2000–U+200A, U+3000 — are **not** collapsed, and are the documented way to write a blank line between paragraphs, a first-line indent, or letter-spacing | «Об использовании нестандартных пробелов» | `[V]` preserved verbatim, and preserved by the browser too. `blankLine` deliberately tests **ASCII space and tab only**: `strings.TrimSpace` folds the whole Unicode space block and so deleted the very lines the author created that way |
| An **escaped** space `\ ` is the other way to write one, and a body line holding just that is the blank-line idiom («отбивка») | «Тело статьи» | `[V]` `\ ` → `&nbsp;`. A `\` with the **line break** right behind it — what an editor leaves after trimming the trailing space — is read the same way, and does not consume the break, so the line renders as `<br/>&nbsp;<br/>` and the blank line survives (`TestBodyBlankLineIdiom`) |
| A body line must begin with **one or more spaces or a tab**; any other first character makes it a headword | «Тело статьи» | `[V]` exactly this test, ASCII only — a line starting with U+00A0 is a headword in Lingvo too |
| Leading whitespace of a continuation line is structure, not content | — | `[V]` `skipAny(" \t")` after a newline, then `<br/>` (suppressed before `[m`, whose `<p>` already breaks). Non-standard spaces are not skipped, so an intentional indent survives |
| Blank lines are allowed **between** cards only; one between a headword and its body, or between two headwords of one card, is a compile error | «Словарная статья», «Заголовок статьи» | `[V]` blank lines are skipped wherever they occur and never end a block — the tolerant reading; a col-0 line after at least one body line is what ends one |
| A directive's `#` must be at column 0, with a space **or tab** before the value | «Директивы…» | `[V]` both separators; an indented `#` line is treated as body rather than refused |
| The compiler inserts a space **before** `[*]`, **after** `[/com]`, and **before** `[/lang]` | «Тэг [*]», «Тэг [com]», «Тэг [lang]» | `[V]` **not reproduced**. It is a quirk the manual itself tells authors to work around, GoldenDict does not do it, and adding a space to every such zone is visible damage in the far more common case where the author already wrote one |
| Headword length is 246 characters, spaces included, except spaces past the 246th non-space character | «Заголовок статьи» | `[V]` not enforced (§5) |
| One "word" — a run of non-space characters — is capped at 255 in a body (tags and `\` excluded); GoldenDict has no limit | «Тело статьи» | `[V]` not enforced; a longer run renders, as in GoldenDict |

## 7. The media zone in depth

Syntax rules, all `[D]` from lingvo-ref «Тэг [s]···[/s]» unless marked:

- The content is **one bare file name with an extension**. Absolute or relative paths
  do not work. The name must fill the whole zone: **no spaces** between the name and
  the delimiters.
- **No other tag may appear inside** — except undocumented `[preview]`, which the x5
  compiler accepts and which does nothing.
- Usable in a headword, but the whole `[s]…[/s]` must then be wrapped in an unsorted
  `{…}` part so the tag does not appear in the headword list.
- Content of media (and link) tags is **excluded from Lingvo's search index**.
- Lingvo packs the files into the compiled `.lsd`; GoldenDict instead reads
  `<name>.dsl.files.zip`.

Formats Lingvo itself supports:

| Kind | Extensions | Notes |
| --- | --- | --- |
| image | `bmp` `jpg`/`jpeg` `tif`/`tiff` `pcx` `dcx` (6.5+), `png` `gif` `wmf` `emf` (x5+) | ≤200 px shown full size, larger ones as a 200 px thumbnail opening in a window; 96 dpi recommended (72 dpi renders a third too large, 300 dpi three times too small) |
| sound | `wav` (the only one ABBYY documents; AC3-compressed allowed), `wav` written by WaveMP3 (stripped MP3, no Unicode meta tags), `asf` (hands off to the system player) | shown as a speaker icon |
| video | `avi` only — **MP4 must be renamed to `.avi`**; GoldenDict also plays FLV renamed `.avi` and WMV as `.wmv` | shown as a camera icon |

### What wudict emits `[V]`

`mediaExt` + `lexTagS` in `internal/format/dsl/transform.go`. The extension is
lower-cased through `path.Ext`; the four kinds are exhaustive — **every payload
renders something**.

| Kind | Extensions | HTML |
| --- | --- | --- |
| audio | `wav mp3 ogg spx m4a` | `<a class="wu-audio" href="NAME">🔊</a>` (`&#128266;`) |
| image | `bmp gif ico jpeg jpg png svg tif tiff webp avif` | `<img align="top" src="NAME" alt="NAME" />` |
| video | `mp4 webm ogv mov m4v 3gp` | `<video class="wu-video" controls preload="none" src="NAME"></video>` |
| file | **everything else**, extension-less names included | `<a class="wu-file" href="file://NAME">📄 NAME</a>` (`&#128196;`) |

Design points that are not obvious and should not be "simplified" away:

- The video list is **browser-playable formats only**. Lingvo's `avi` and
  GoldenDict's `wmv`/`flv`, plus `mkv mpg mpeg asf`, take the file link: an inline
  `<video>` for a codec no browser decodes is a permanently broken player, whereas a
  link reaches the system player — which is what Lingvo does with them anyway.
  Same reasoning puts Lingvo's `pcx dcx wmf emf` images in `file`, not `<img>`.
- `preload="none"` — a card may hold several tens-of-megabytes clips; nothing is
  fetched until the reader presses play.
- The file link uses the **`file://` pseudo-scheme**, and only there. The article
  rewriter treats `sound://`/`file://` as "the author naming their own file" and
  rewrites regardless of extension (§9), so a container-owned `.pdf` is reachable
  **without** widening `dict.IsAssetName`, which is also the allowlist for files
  lying loose beside an `.mdx`. Keep those two concerns apart.
- Audio is an `<a>`, not GoldenDict's `<object type="audio/x-wav">`: the anchor
  survives `format=clean`, is understood by both renderers, and needs no inline
  handler.
- The name goes through `quoteAttr` in attributes and `escape` in text; a name
  containing `"` or `&` cannot break out. Covered by `TestTransformMediaKinds`.
- An empty zone (`[s][/s]`) emits nothing and records no resource. `[V]`

## 8. Deviations we keep

| Case | Lingvo / pyglossary | wudict | Why |
| --- | --- | --- | --- |
| mandatory `#NAME`/`#INDEX_LANGUAGE`/`#CONTENTS_LANGUAGE` | compile error when missing | defaults | refusing a file the user already has loses a dictionary, and gains nothing |
| malformed/empty tag (`[ ]`) | drops the entry | literal text | real articles contain `([ ])` |
| headword variants | XML-escaped (pyglossary) | raw | they are lookup keys; escaping breaks matching |
| unknown tag | compile error / warning logged | silently unwrapped | a warning per article is noise at 100+ dictionaries scale |
| unterminated `{{` | consumes the rest of the file | same across lines, literal within one line | a stray `{{` in one body line is a typo, not a request to delete the entry |
| `[ref]` target matching | case-sensitive | case-insensitive, through the store index | forgiving in the direction that can only find more |
| `#SOURCE_CODE_PAGE` casing | exact, errors otherwise | case-insensitive | the alternative to accepting it is mojibake, not a diagnostic |
| `.ann` `#LANGUAGE` | one section by UI language | all sections | see §1.2 |
| `[*]` secondary zone | hidden behind a toggle | always shown | `[V]` gap, listed in §11 |
| body lines belonging to no headword | compile error, no dictionary | the block is skipped with a warning (first three only), the scan continues | the file is already on the user's disk; one stray run of lines must not cost them the whole dictionary (`errOrphanBlock`, `Reader.Next`) |

`dslEscape` (`reader.go`) escapes `\ [ ] ~ < > @` when a sub-entry key is embedded
back into generated DSL; `titleEscaper` additionally escapes `( ) { }` when a
parent headword is substituted into a sub-card heading (§4). The two alphabets
differ on purpose — `(` is syntax in a title and inert in a body — and merging
them would be wrong in one direction or the other. `TestDslEscapeRoundTrip` and
`TestExpandTitleTilde` pin them. `[V]`

## 9. Pipeline map

```
.dsl bytes
  └ internal/format/dsl/reader.go   detectEncoding → header (+#INCLUDE queue)
      │                             → stripSpanComment → blocks → parseBlock
      └ title.go       transformTitle   headword → Keys[] / Display
      └ transform.go   transformBody    body → HTML   (+ resFiles: names referenced)
          └ store.IngestPlan → <db dir>/<name>/text.db      (headwords only by default, D24)
               └ media pack (opt-in) → media.db             (store/media.go, IngestMedia)
  └ query: store.Store lookup → entry HTML
      └ internal/server/rewrite.go  RewriteEntryHTML(html, dictID)
          · htmlref tokenizer walks every reference site
          · fetch sites (img/video/audio/source src, link href) → always rewritten
          · <a href> → rewritten only when isResourceRef: sound:// or file://
            pseudo-scheme, or dict.IsAssetName(ref) by extension
          · resURL strips the pseudo-scheme → /res/{dictID}/{name}
      └ internal/server/articleformat.go   format=clean|text (raw is the default and
        what the built-in UI requests; clean keeps <video src|controls|preload>,
        the wu- classes, title= and data-dict on <a>)
  └ GET /res/{dict}/{name}  internal/server/server.go handleResource
      · serveOverride first  (<library folder>/res/<name>, user replacements)
      · d.Resource(name) → format backend or media.db
      · .spx → transcoded to WAV in-process (D18)
      · webMIME override table, then the backend's own MIME
      · io.ReadSeeker → http.ServeContent (Range, 206, Accept-Ranges)
        otherwise → io.Copy through nulWatcher (text resources; damaged-blob warning)
  └ renderers
      · internal/server/web/index.html — shadow DOM, document click dispatch:
        parseRef → data-dict scope resolution → audio extensions → <img> link →
        .wu-file → http(s) → "#" → bare word
      · internal/server/web/frame.js  — sandboxed srcdoc iframe, capture-phase click:
        same order; posts {t:"ref", w, dict, xdict, frag} to the parent, and
        {t:"open", url} for .wu-file and external links
```

`internal/artmark.Version` is the markup contract between an ingested article and
the stylesheet. It is **2** as of the pass that added `wu-xref`, the full optional-part
expansion and the `#INCLUDE`/`^`/`[br]` handling: articles prepared by an older
build are reported as stale (a rebuild offered, never forced). `[V]`

Seekability matters end to end: `resource.Dir` returns an `*os.File` (seekable), a zip
entry is **not** seekable, and `store.Media.Resource` returns
`readSeekNopCloser{*bytes.Reader}` — `io.NopCloser` would hide `Seek` and silently
disable ranges for every packed dictionary. `[V]`

## 10. Resource-name matching (`internal/resource`)

The article's name is text; the container's name is bytes. So each stored entry is
indexed under **every plausible reading** of its bytes and the article selects one —
no guess about the container's code page is made. `[V]`

- `Key(name)` = `Clean` (backslashes → `/`, no leading `/` or `./`, no `..` escape) +
  NFC + lower case. Case and normalization are folded there and nowhere else.
- `readings(raw, utf8Declared)` adds a key per legacy code page that decodes without
  U+FFFD (cp1251, cp1252, cp866, KOI8-R, cp1250, cp1253–1258, ISO-8859-2/5/7, cp437,
  cp850). Display name is chosen by `score`, which judges whole words, not runes
  ("café" vs "ÊÓÁÎÊ").
- `Index.Lookup` falls back to the **basename** when the full path misses, unless that
  basename is ambiguous (`dupe`) — this is what lets an article say `кубок.jpg` when
  the zip stores `files/кубок.jpg`.
- `Dir.Open` tries the original spelling first (case-sensitive filesystems), then the
  index. `NewDirExact` (loose files beside the `.dsl`) skips the index entirely.
- `IsJunk` removes `.DS_Store`, `._*` AppleDouble shadows, `__MACOSX/`, `Thumbs.db`
  at any path depth, case-insensitively.
- `store.Media.Resource` does its own folding, since media.db is queried by SQL:
  exact → `COLLATE NOCASE` → NFC → NFD. macOS hands out NFD filenames while the
  article says NFC; `COLLATE NOCASE` folds case only. `[V]`

## 11. State of the implementation

Closed in the spec-audit pass (this document's current revision):

| Was | Now |
| --- | --- |
| `[ref dict="…"]` ignored — the link stayed inside the source dictionary | `wu-xref` + `data-dict`/`title`, resolved to a dictionary id in both renderers (§6.1) |
| `(…)` gave two keys — all-in and all-out | full 2ⁿ expansion, capped at 6 parts (§5) |
| `[br]` unknown → dropped | `<br/>` |
| `^` literal | rune-aware case inversion, `^~` included |
| `]]` emitted twice | folded to one `]` |
| a `#` line after the header was read as a headword | always a directive; unknown ones skipped |
| `#INCLUDE` swallowed, its entries silently missing | read, cycle-guarded, nested, with a base-name fallback (§3.1) |
| `#FULL_NAME` / main-file `#LANGUAGE` unknown | accepted as 6.0/7.0 spellings of `#NAME` / `#INDEX_LANGUAGE` |
| `~` literal in a sub-card heading | expanded from the parent, title-escaped (§4) |
| a `{{…}}` comment spanning cards | removed before block classification (§6.4) |
| a `{{…}}` block between the directives and the first card, or a comment-only line at column 0, aborted preparation with "entry block without headword" | every zone is removed from the raw line before classification, in the header scan and the entry scan alike, and a headword-less block is a skip, not a failure (§6.4, §8) |
| a body line made of non-standard spaces (the «отбивка» blank line) was dropped as empty | `blankLine` folds ASCII space and tab only (§6.5) |
| `\` at end of line — the blank-line idiom after an editor trimmed the trailing space — ate the line break and rendered nothing | `&nbsp;` plus the break (§6.5) |

Closed earlier: `@` recognition (`@heading`, `[m1]@ heading`, piled headings),
one back-reference per expanded key, `dslEscape` double-escaping, the media-zone
rewrite (every payload renders), `Accept-Ranges`/206 on `/res/`, media.db
normalization folding, `#SOURCE_CODE_PAGE` (`codepage.go`).

Open, with what correct behaviour would be:

| Gap | Correct behaviour |
| --- | --- |
| `[*]` secondary zone | Lingvo hides it behind a toggle; we always render it. A `details`-like control would match the format's intent. `[V]` gap |
| `[trn1]` | falls through as an unknown tag; harmless today, but it belongs with the other search-processing wrappers so the intent is explicit. `[V]` gap |
| `[']` inside a link zone | forbidden by the spec; we render it. Harmless, listed for completeness. `[V]` |
| 246-character headword limit | Lingvo drops the entry; we index it. Deliberate — a longer key costs nothing here. `[V]` |
| `[c]` palette | any name `artmark.IsColor` accepts is passed through as `--wd-c`; Lingvo's palette is a closed list (lingvo-ref «Палитра цветов»). Anything outside CSS's own names would need a mapping table. `[V]` |
| `[s]` image sizing | Lingvo thumbnails anything over 200 px and opens it in a window; we render the image at its natural size. `[V]` gap |
