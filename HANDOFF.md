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
  name scans in `plainArchive`.

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
  - `internal/intake`: TestJobDisposesSource, TestSpooledSourceIsAlwaysRemoved.
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
