# Changelog

Notable changes in **wuDict2**, the Android fork of
[WuWeiDict](https://github.com/wuweidict/wudict), relative to the upstream
version it was forked from. Fork release tags are prefixed `wudict2-`; one
tagged line per release, newest first.

## wudict2-v0.1.0 — 2026-09-20

Baseline: upstream wuDict master at commit `223b990` (2026-09-17),
shortly after **v3.7.4**, before `v3.7.5-alpha.1`. Everything below is
fork-only work on top of it.

### Android app

- **Distinct wuDict2 identity** — own application ID, launcher name and a
  dedicated lookup activity for external readers, so it installs and runs
  **beside** the upstream wuDict app; default server port 6889
  (upstream: 6888). The shell verifies the responding server's library
  directory before adopting it.
- **Transparent system splash** — cold start no longer flashes white.
- **Window background**: color and image, with built-in paper wallpapers;
  applied to both app windows and to system dialogs, including the
  dictionary-selection dialog.
- **Native dialogs**: dictionary picker, and folder picker ("Browse…") on
  the setup page.
- `build-android.cmd` — one-command local APK build from Windows.

### Search and results UI

- **Search history** with suggestions.
- **Tap a word in an article** to look it up — no selection needed.
- As-you-type updates only the suggestion list; articles refresh on word
  selection, Enter or leaving the field — far less re-render churn on
  phones.
- **Dictionary groups** with sorting.
- Pinnable search bar; fixed auto-hide on scroll and inertial scrolling of
  the results panel; dictionary selector moved left of the input field;
  status messages no longer shift the title/article.

### Appearance

- One **Appearance sheet**: window background (color/image, thumbnail
  strip, dominant-color auto-suggest from the chosen image, recent colors),
  App/Article/Files CSS, keyboard-aware sizing.
- **CSS presets as toggleable layers**: background group (Sepia /
  True black / Warm dark / Background image) and fonts group (Serif /
  Condensed / Light) are pick-one, the rest are free toggles; presets layer
  under your own CSS, state survives reload, `?style=off` bypasses
  everything.

### Server reliability and performance

- Corrupt or hostile dictionary files can no longer trigger gigabyte
  allocations — every parser path is bounded (bgl, mdx, slob, stardict).
- Windows: rebuilding or re-packing a dictionary no longer fails at the
  final rename with "Access is denied".
- Large packed media (video, big PDFs) **streams with Range/seek** instead
  of loading whole blobs per probe.
- `/res/` resources and the library listing answer without opening SQLite
  per item — articles with many images serve noticeably faster.
- Wedged queries are abandoned on cancel instead of stalling the stream;
  concurrent settings saves are serialized and atomic (overlapping saves
  no longer lose keys); assorted race fixes, per-request HTTP client reuse,
  intake dispose ordering on Windows.
