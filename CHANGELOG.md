# Changelog

Notable changes in **wuDict2**, the Android fork of
[WuWeiDict](https://github.com/wuweidict/wudict), relative to the upstream
version it was forked from. Fork release tags are prefixed `wudict2-`; one
tagged line per release, newest first.

## wudict2-v0.2.0 — 2026-09-26

Also carries everything merged from upstream since v0.1.0 — upstream
**v3.7.5** and the commits after it: the Browse A–Z word lists, full-text
query changes, index-version tracking with reindex prompts, configurable
dictionary groups, double-tap search, read-aloud fixes. Everything below
is fork-only work on top of that.

### Settings, reorganized

- **The ☰ panel is five sections** — Dictionaries, Results, Appearance,
  History, Info. The single "Folders & setup" fold is gone; the folder paths
  move to Info, next to About.
- **A Dictionary settings window** owns the per-dictionary cards, opened from
  "Edit dictionary settings" beside "Edit dictionary groups": name, format,
  entry count, capability chips, provenance, a Browse link and an "About this
  dictionary" fold-out. Collection-level actions (edit folders, rescan,
  lemmatization, full-text for every dictionary) stay in the panel, and the
  window remembers your scroll position.
- **Dictionaries can no longer be switched off.** The "Enable all" master
  switch and the per-card toggles are gone from the UI, and the page no longer
  reads the stored flag, so a dictionary switched off in an older version
  cannot be left invisible with nothing left to switch it back on. Every
  installed dictionary is searched.
- **Setting rows read as a label and a choice** — "Highlight matches" On/Off,
  "Open first" My order/Fastest, "Sort dictionaries" Alphabetical/My order;
  history length and Clear get their own section.

### Day and night

- **The theme control is two buttons** — Auto, and a state button whose label
  names the action ("Switch to night"). The accent marks which of the two is
  in force, so "following the phone" and "pinned" are no longer the same
  press. Storage is unchanged, so nothing needs migrating.
- **Appearances are per theme.** Your app and article sheets are two pairs
  now (day and night) with only the resolved one linked, so a day sheet no
  longer paints over a dark page; the window colour and wallpaper are per
  theme in the shell too, applied before the page exists. A third built-in
  wallpaper, `paper_03.jpg`, is the dark paper.
- **Style layers are per theme.** The presets manifest gained night slots:
  True black and Warm dark are night-only, Sepia and High contrast day-only,
  Background image has a file for each, and a layer with no file for the theme
  you are in contributes nothing. The conflict rule is an overlap test now, so
  **Sepia by day and True black by night can be on together**.
- A layer that cannot take effect where you are shows as a dimmed row with the
  reason on it ("Light theme only — the page is in the dark one"), and enabled
  article layers are loaded at start-up instead of waiting for the Appearance
  sheet to be opened once.

### Saved appearances

- **A whole appearance can be saved under a name and applied again.** The
  panel's Presets row is the drop-down plus save, update and delete; it reads
  "Custom" when the screen no longer matches anything saved, and switching
  away from unsaved changes asks before discarding them. Three ship built in —
  **Clean**, **Warm** and **Old paper** — and only your own can be updated or
  deleted.
- A preset covers text size and weight, which style layers are on, the
  per-theme window colour and wallpaper, your sheets, and the screen edges. It
  deliberately excludes the theme, so applying one at night does not turn the
  lights on.
- The style-layer door is renamed **"Style layers…"**, because "Presets" now
  means the saved appearance.

### Appearance sheet

- **One subject at a time**, named in the sheet's own head and opened from its
  door in the panel: Edges of the screen, Window background, Style layers,
  Custom CSS. The sheet is non-modal, so the page stays visible while you
  adjust it.
- The screen rows are **radio groups with every option visible** instead of
  drop-downs, and the margin-colour field sits on its option's row, dimmed
  when that option is not the chosen one.
- **Font weight** (Normal, Medium, Bold) joins font size and reaches article
  text as well as the app's own.
- **A built-in colour picker** (RGB sliders, swatch, hex preview) replaces the
  platform chooser, which could not open at the field's existing colour.

### Dictionary picker and search bar

- **The picker is one list** — the dictionaries that answered the current
  search, in the order their sections appear. The All/Found toggle is gone
  from the page and from the shell, and no stored mode is read any more.
- Rows are exactly what is on screen, and the group drop-down names the
  dictionary being searched when the scope is a single dictionary.
- **Picking a dictionary jumps to it instantly**, whatever the system's
  reduced-motion setting says; the other two scrolls still follow it.
- The search bar's controls are separated by hairlines, the scope chip reads
  "All" instead of a bare glyph, and the caret is handed back to the field only
  when the URL carries no `?q=` — a word opened from a word list no longer
  comes up under the history drop-down.

### Android app

- **New launcher icon** — the lens with a "2", in both its ordinary and
  monochrome forms, and in the Play listing images.
- **The Browse page wears the host background**, like the setup and lemmas
  pages.
- A closed dialog (the colour window, dictionary settings) no longer paints
  for a frame during launch.
- **Debug builds can carry an x86_64 server** (`build-android.cmd debug
  intel`) so the app runs on an x86_64 emulator; release APKs stay arm64-only.
- The native Settings screen lost its Screen section — those rows live in the
  web Appearance sheet now — and gained a clear-cache button. The Play listing
  text's "uDict" typo is fixed.

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
