# Agent handoff — Go reliability review work

Written 2026-09-20 so a new session does not re-derive this from scratch.
Read `AGENTS.md` first (workspace rules, branch policy), then `CLAUDE.md`
(project map, D-numbers), then the relevant `docs/` sections. This file
records what an agent session changed, what it verified, and what it left.

## Branch state (verify with git before trusting)

Branch `fix/review-hardening`, based on `dev`. Six review items committed,
then a second-tier hardening batch (zim Close race, rows.Err diagnostics,
replacer hoisting, dead gomdict range-tree removal, intake http.Client
reuse, plainArchive map) committed on top — see `git log` for the exact
hashes; it was uncommitted at the time of this edit.

| Commit | What it is |
| --- | --- |
| `0854ec5` | Setup page: native folder-picker button (Android shell bridge) |
| `d3ea6c4` | Item 1: frontLimit defer, `n=` clamp, intake job race snapshot |
| `2675563` | Item 2: release prepared DBs before an ingest renames over them (Windows) |
| `c0aca2c` | Item 3: cap every allocation a dictionary file can name + bounds tests |
| `bd1e415` | Item 4: `/res/` and `Library()` answer without SQLite |
| `9288f37` | Item 5: stream large packed media (blobReader, 4 MiB threshold) |
| `23377f8` | Item 6: abandon wedged queries on cancel; atomic config write |

Nothing has been pushed or merged into `dev`; that is the user's call.

## What remains from the review (with the reasons for leaving each)

- **Ingest is not cancellable** — deferred on purpose. Plumbing ctx through
  `IngestPlan`/`IngestMedia`/Progress touches signatures shared with the CLI,
  the server and format self-prepare: a wide diff in upstream-shared code for
  a need nobody has voiced (one ingest runs at a time, intake jobs cancel
  fine). Do it only if a "cancel indexing" button becomes a requirement.
- **`setFeatures` stale backend**: when the rebuild succeeds but the media
  step fails, the entry keeps the old backend and `revalidate` (path-string
  compare) never swaps it until eviction. Fix is ~5 lines (reopen on the
  media error path, reporting both errors); left because the window is
  narrow and the failure already surfaces to the user.
- **Recover guards for ad-hoc goroutines** — partially moot, and that is
  why it was not done wholesale: `handleDicts`/`Warm` workers call
  `dict.Open`/`Probe`, which recover internally. The genuinely bare spots
  are `registry.go` `recordLinks` (container parsing) and the `lemmas.go`
  install goroutine (download + archive extraction); wrap those two first
  if any panic ever escapes.
- **Lemma catalogue fetch holds `catMu` across the network** — every
  `/api/lemmas` poll queues behind a hung fetch. Rare (needs a stalled
  connection during an install); fix shape is serve-stale + singleflight.
- **Deliberately deferred in item 5**: `upgraded.linked` (links-locator
  media fetch buffers whole resource; Seek semantics make streaming harder
  there) and `.spx` transcoding (clips sit orders of magnitude below the
  4 MiB streaming threshold).
- **Fixed since this list was first written** (so nobody re-reports them):
  zim `Close()`/`c.zr` race, `rows.Err()` in `Keywords`/`Media.Names`,
  per-call `strings.NewReplacer` in stardict/slob/bgl, dead record-range
  tree in gomdict, per-call `http.Client` in intake fetch/probe, O(n²)
  name scans in `plainArchive`, and the intake dispose ordering
  (`install()` now closes the archive before the tail removes the source —
  on Windows the remove over the open reader silently kept the file; the
  two intake tests that caught it are green again).

## Windows verification recipes (this machine)

- Go: `go build ./...`; targeted `go test ./internal/<pkg> -run '...' -count=1`.
- Android Java: from `android/`,
  `ANDROID_HOME="$LOCALAPPDATA/Android/Sdk" ./gradlew.bat :app:compileFossDebugJavaWithJavac --offline`.
  No ANDROID_HOME in the bash environment by default.
- No `node`, no `gcc` in this shell: JS syntax checks by hand/`vm`, and
  `-race` cannot run (cgo). Do not burn time on either.
- `gofmt -l` flags nearly every tracked Go file — CRLF working-copy noise,
  not real. `git diff --check` is the meaningful check.
- Known Windows test failures that fail identically on clean HEAD (do NOT
  chase them as regressions — compare against a clean checkout via
  `git worktree add /tmp/x HEAD`):
  - `internal/server`: TestAndroidAliases(×2), TestDamagedTextResource…,
    TestIntakeUploadAndInstall, TestOpenAPICoversEveryRoute,
    TestRescanRecoversFromDeletedPreparedFolder, TestResourceAndIndex,
    TestResourceOverrideFromLibraryFolder, TestSetupFlow,
    TestSetupMultipleFolders.
  - `internal/format/dsl`: TestMediaSourcesEveryZipSpelling.
  - Flaky everywhere: TestFailedDemandIsRetried (TempDir cleanup races the
    ingest goroutine; failed 4/5 on clean HEAD once).
  - `internal/intake`: TestJobDisposesSource and
    TestSpooledSourceIsAlwaysRemoved USED to fail here (dispose ran before
    the archive reader closed, so Windows kept the "delete the source"
    file); fixed in the second-tier batch — if they come back, look at the
    archive Close ordering in `install()`.
  - Seven server tests used to fail on Windows at the rename step before
    item 2 (`2675563`) fixed it — if they regress, look at
    `internal/server/registry_windows.go`.

## Architecture facts this session leaned on

- Android shell ↔ page channels: `wudict://` navigations caught in
  `Shell.openExternal`; `window.prompt('wudict:…')` answered from
  `Shell.windows().onJsPrompt` (dictionary picker, folder picker);
  `window.wudictNativeShell=1` injected by `Shell.applyBackground` marks a
  page that will be answered. Web assets stay upstream-shared; Android
  behavior is injected, not compiled in (D54).
- Registry entry backends: `upgraded{Store over text.db + lazy src}`,
  `native` (no source), direct preview. `dsl.Dict` and `bgl.Dict` embed
  `*store.Store` themselves (auto-prepare formats) — they hold text.db too,
  which is why `releasePrepared` closes ANY serving backend, not just
  `*upgraded` (Windows-only; `registry_other.go` is a no-op).
- `entry.rebuilding` bars `open()` with `errReindexing` for the length of an
  ingest that renames over the prepared DB; defers in setFeatures /
  ensureBaseIndex / reabsorbAbbrev own the un-set on every error path.
- Allocation ceilings convention: 256 MiB (`maxLZOBlock`, zim
  `maxClusterBytes`, slob `maxItemBytes`, stardict `maxIndexBytes`);
  bgl blocks 64 MiB. `readAllBounded` pattern per package.
- `entry.preparedDB` (no source-changed check) is for PATH lookups
  (`serveOverride`); `preparedTextDB` (with the SQLite-verified check) stays
  for open decisions and the panel's dict rows.
- `store.Library()` reads `info.txt` receipts first (`receiptMeta`);
  `WriteInfo` writes a machine-readable `contains = 0|1` line; receipts
  predating it fall back to `ReadMeta` until re-ingest.
- `Media.Resource`: length-first probe, whole-row read under
  `blobStreamMin` (4 MiB), `blobReader` (substr chunks over the read-only
  pool) above; Range requests then read only the requested bytes.
- `search.runQuery`: queries run in an inner goroutine; on ctx death they
  are abandoned (never interrupted — dict interfaces take no ctx), bounded
  to one goroutine per wedged backend. A finished query outranks a
  cancellation landing in the same tick (re-check under the ctx branch).
- `config.SaveKeyRaw`: the WHOLE read-modify-write holds `saveMu`; the
  write is temp-in-same-dir + Sync + rename (`writeFileAtomic`). Nested
  locking would deadlock — `writeFileAtomic` takes no lock itself.

## Not verified on a device

Everything above is build- and test-verified on Windows x64 and
cross-compiled for linux/arm64. No device (Android phone) has run the
folder-picker UI, the rename fix under a real rebuild, or streamed media
over the Range path. The Android-side checklist lives in
`docs/ANDROID-UI-HANDOFF.md`.

## Appearance implementation (2026-09-20)

Uncommitted on `fix/review-hardening`. The former native Settings controls for
Background color and Background Image now live above App/Article/Files in the
web Appearance sheet. `wudict:appearance` reads/writes the *same* `ShellPrefs`
keys, and the native shell still paints both windows before HTML loads. The
image picker reads `WindowBackground.images` from the same Files store;
browser-only use hides these native controls.

Same-day rework after user review, still uncommitted. The sheet gained a real
head row (`#stylerHead`: "Appearance" + the ✕, top-right in both the native and
browser layouts — the ✕ used to sit in the mid-sheet toolbar row). The
Background Image dropdown + Add… pair is gone: `appearanceRender` now builds a
thumbnail strip (`#appearanceStrip`) — a None tile, one tile per image
(`/files/<name>`, `object-fit:cover`, lazy), a dashed tile for a selected-but-
missing name (tapping it clears the stale pref), and a ＋ tile that opens the
system file chooser synchronously (user activation is never spent on a pane
switch) and, via `stylerPickForBackground`, promotes the last uploaded image to
the background; the input's `cancel` event clears the flag. The Files tab stays
the manager for CSS-referenced files; deleting a chosen background image from
there still resets the pref (via `appearanceState`). The custom-CSS textarea
now uses the app typeface at 13px like the Background rows (was monospace
12.5px), and both section headings share one weight.

Verified in a desktop browser against a throwaway server (temp config, one
minimal .dsl, two uploaded PNGs): sheet opens from the panel, Background hidden
without the shell, strip renders with the right selection ring, thumbnail/None/
missing taps round-trip the bridge state, ✕ and Escape close, Files tab intact.
Java compilation, `go build ./...` clean. No APK was built, installed, or
tested on a phone. On device still to check: strip layout with real wallpaper
sizes and large system fonts, immediate repaint of both windows after a tile
tap, cold-start background without white flash.

Second user-review round, same day, NOT build- or browser-verified (user asked
for neither; they will check on the phone). Section headings are separated by
hairlines (under `#stylerHead` and under the window-background block), and the
Background heading is now "Window background" — the android:windowBackground
term, not "Windows background". The ＋ tile is smaller than a thumbnail and
dashed; its box is sized off an absolute 12px base because em sizes on it
would resolve against the inherited 16px sheet font and grow it right back.
Scroll affordance: `appearanceStripHints` toggles `fade-l`/`fade-r` mask
gradients on the strip by scroll position (scroll + resize listeners; renders
re-run it). `BUILTIN_BACKGROUNDS` (paper_01.jpg, paper_02.jpg) render no
Delete button in Files and carry a "built in, restored at startup" note — the
shell recopies them when absent, so Delete could only promise what it cannot
do; `stylerFileDelete` also refuses them. The Files tab keeps its Add…: the
store holds fonts and stylesheets the strip never offers, and in a desktop
browser the Background section does not exist at all, so Add… is the only
upload door there.

Third user-review round, same day, NOT build- or browser-verified (user asked
for neither; they will check on the phone). The Background-color checkbox now
wears the groups editor's square (20px, --fg border, drawn check, --paper-bg
under it) instead of the platform box. The strip's edge fades are gone:
`#appearanceStripWrap` carries two carousel chevrons (`#appearanceStripL/R`),
shown exactly while that side still hides thumbnails (appearanceStripHints
toggles `hidden`; scroll + resize listeners; renders re-run it), and a tap
scrolls ~80% of the view smoothly. Note `.stripArrow[hidden]{display:none}` is
load-bearing — the class's display:flex outvotes the UA hidden rule otherwise.
The ＋ tile is back to the thumbnails' exact 4.1em box (dashed border is the
only distinction); its glyph is a span at 1.9em because bare text would
inherit the 12px basis and look lost. Shell.java's WebChromeClient now answers
`onJsConfirm` and `onJsAlert` on the shared `BackgroundDialogBuilder` surface
(so the Files list's "Delete X?" sits on the same wallpaper and palette as the
pickers; every exit answers the JsResult once, a bad window token cancels).

Fourth user-review round, same day, NOT build- or browser-verified (user asked
for neither; they will check on the phone). The thumbnails now dissolve BEFORE
the arrows instead of under them: `fade-l`/`fade-r` masks are back (one
gradient per combination — stacked mask layers composite source-over and a
second layer would un-fade the first edge), tuned to reach full transparency
at 2.8em from the edge, which is exactly where the 2.5em+.3em arrow circle
ends; both fade and arrow ride on the same condition in appearanceStripHints.
Mask lengths resolve against the strip's inherited 16px — the same base the
arrow is drawn against. The chevrons were redrawn (1.8 stroke, 1.25em box).
The color field gained a recent-colors dropdown: the last ten accepted colors
in localStorage (`wudict_color_history`, web-side convenience — the shell
still owns the applied color), swatch rows in `#appearanceColorMenu`, opening
on focus/pointerdown, Escape stops at the menu, outside tap closes; only
colors the shell accepted are remembered. The dropdown wears the search-history
surface verbatim (bg-card, or paper tint + wallpaper under `data-shell-*`).
The 35% row cap on the color input moved to `#appearanceColorBox` (a
percentage on the input would now resolve against the box and cap nothing).

Open question answered, not implemented: deriving a matching background color
from the chosen image (average of a downscaled copy, or a dominant-bucket
histogram for textures like the paper wallpapers) — feasible both web-side
(canvas on /files/<name>, same-origin) and in WindowBackground, which already
holds a downsampled bitmap. Waiting for the user to pick a shape (a "from
image" affordance vs auto-suggest on selection) before building it.

Fifth user-review round, same day, NOT build- or browser-verified (user asked
for neither; they will check on the phone). The auto-suggest question was
settled: choosing a background image - a thumbnail tap or the ＋-tile upload -
now also computes the image's dominant color and fills, applies and remembers
it in the color field (`appearanceImageChosen` → `appearanceDominantColor`:
24×24 canvas sample, 4-bit-per-channel histogram, largest bucket averaged,
transparent pixels skipped; a sequence guard orders rapid taps; the on/off
checkbox is left alone; any failure yields "no suggestion"). The arithmetic
is instant; only the decode costs, and the browser has usually done it for
the thumbnail. The row labels shrank to "Color" / "Image" and the label
column to 4.2em, so the strip gains the width. The arrows are bare chevrons
now (no circle) with a hand-computed ~150° tip — apex (13,8), tips at
±75° from the axis, path M11.4 2 L13 8 l-1.6 6 and its mirror — colored
var(--fg) with a drop-shadow halo in var(--bg), hover/focus accent; the
mask's transparent stop moved to 2.9em to match the new 2.6em+.3em hit area.

Sixth user-review round, same day, NOT build- or browser-verified (user asked
for neither; they will check on the phone). The toolbar row now shares one
chrome — `#styler .tab`, `#styler .btn` and `#stylerPreset` in one rule
(12.5px, same padding/height/palette/radius); the preset `<select>` gets
`appearance:none` because on Android the platform select face was the loudest
of the mismatches; the narrow media query shrinks all three to 11.5px
together. Files-pane mini-buttons keep their compact 12px (later rule, same
specificity). The strip's fade mask was tightened to end at 2.3em from the
edge — just before the drawn chevron's outer tips (2.35em), not before the
2.9em hit box — so a thumbnail stays visible until it is a whisker from the
arrow and fades only across its last 1.4em. Same-day tweak to that tweak:
the fade was still ending at the hit box's boundary, so the mask now runs to
1.8em — a whisker short of the box's center (1.6em, where the chevron's apex
sits) — and the picture holds until it is essentially under the arrow.
