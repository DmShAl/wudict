# Agent handoff — Go reliability review work

Written 2026-09-20 so a new session does not re-derive this from scratch.
Read `AGENTS.md` first (workspace rules, branch policy), then `CLAUDE.md`
(project map, D-numbers), then the relevant `docs/` sections. This file
records what an agent session changed, what it verified, and what it left.

## Branch state (verify with git before trusting)

Branch `dev` at merge commit `adb5507` (2026-09-20): upstream `master`
(wuweidict/wudict @ `312b88b` — Browse A-Z headword pages, state.json
dupes cleanup, accordion-clipping ResizeObserver fix, docs) merged into
`dev` and pushed. One conflict, `index.html` (the fork keeps styles in
`web/app.css`); resolution: fork structure + upstream's browseLink and
card-chip markup in index.html, the `.pd .acts a.browse` CSS into
app.css after `.pd .err`, ResizeObserver border-box fix stays in
index.html. Verified by a throwaway-worktree trial merge BEFORE the real
one: `go build`, `go vet`, store/dict green; server failures all from the
known Windows list. Note: `git worktree list` shows two unrelated codex
worktrees (`.codex/worktrees/…`) — not ours, left alone. A half-done
manual merge attempt (10 files deleted in the working tree, index
intact) was found and recovered with `git merge --abort` before redoing
the merge properly; nothing was lost.

**Rechecked 2026-09-21 (later session):** `dev` is at `653c36a` ("Merge
branch 'master' into dev"). The appearance-sheet work below is no longer
uncommitted — it is `f72bf04` "Appearence tab" on `Dictionary-Settings2`, and
`dev` contains it. The working tree is **not** clean, and what is in it is the
two newest pieces of work, each with its own section below: the Settings
drawer's five sections (stage 1) and the picker's single mode. Their files are
`internal/server/web/{index.html,app.css}`, `android/.../{Shell,ShellPrefs}.java`
and the two `docs/` UI notes — nothing else. One untracked directory is not
this work and was left alone: `.zcode/` (agent tooling — a plan file). It is not
ours to ship or delete.
Everything below those two sections is a historical snapshot, not current state.

Older appearance/presets/review notes below are historical.

**Rechecked 2026-09-22:** `dev` is at `09fd7ea` — the two pieces above are
committed, and the tree is clean again apart from one new uncommitted piece,
this session's icon change, plus the same untracked `.zcode/`.

**Rechecked 2026-09-23:** `dev` is at `8c50272`, the merge of upstream `master`
(`8b45514`) into `dev`. Trial-merged in a throwaway worktree
(`git worktree add … dev`), so the main checkout, which sits on `master`, was
never touched. `go build ./...` and `go vet ./...` clean — there is no `gcc`
here, so the tag-less/pure-Go path is what was built; `go test ./...` leaves
only the two known Windows failures below. Upstream's `fcd6edb` is a
cherry-pick of THIS branch's work with its own evolution on top, which is why
57 of its 72 files merged clean and the bounds tests came out byte for byte
ours; the 15 that needed a decision and how each was resolved are in the merge
commit's message. Three facts that are not derivable from the code:

- **The setup page's 📁 is now a host capability, not our prompt.** The page
  calls `wudictPickFolder` and shows the button only while the host has set
  `data-folder-picker` (foss `Storage.java`, reached through `MainActivity` →
  `Intake` → `Storage`); the Play flavour never defines the hook, so 📁 stays
  hidden there. `Shell.java`'s `wudict:folder-picker` prompt path (`onJsPrompt`
  → `settleDir`) is now unreachable — deliberately left in place; delete it
  when someone is next in that file. `showDirectoryPicker` went with it: in a
  browser it can name only the folder, never the path the server needs.
- **Double tap and double click run through three places that must stay in
  step**: `pick.js` (master's version — it fixes a `lang="en_US"` that threw
  out of the whole hit test, and bounds its scan around the tap), `frame.js`'s
  iframe handler and index.html's shadow-root handler. Both handlers try the
  selection first and fall back to the word, and the two are NOT the same
  message: a selection is prose and goes by `pick` to `lookupSelection`, a
  segmenter word is already exact and goes by `pickword` to `searchFor`. Keep
  that split — `lookupSelection`'s trailing-digit trim turns "CO2" into "CO"
  and "1984" into nothing.
- **This merge is what takes `dev` from twelve failing tests on this machine to
  two.** The server resource/index, `dsl` and `lemmas` failures were upstream's
  fixes arriving, not something left to re-fix.

## Emulator builds: `build-android.cmd debug intel` (2026-09-24, this session)

The user's Android Studio AVD is x86_64 (`sdk_gphone16k_x86_64`, Android
17/API 37, 16 KiB pages), where the app's Java half starts and the exec'd Go
server never does. The diagnosis is in the verification-recipes section below;
what this session CHANGED is the build path that follows from it. Uncommitted
on `dev` on top of `3178bc1`.

- **`build-android.cmd [debug|release] [intel]`** — the token is accepted in
  either position, a bare `intel` means a debug build, and `release intel` plus
  any unknown token are refused before anything is built. `%~2` is no longer
  "retired"; `-PemuX86=1` is what the script hands Gradle.
- **The trap worth remembering**: `androidComponents.onVariants` runs for EVERY
  variant, `fossRelease` included, even when only `assembleFossDebug` was asked
  for. An earlier version of this change THREW from there on a non-debug
  variant and failed the debug build outright. The abi token is therefore
  decided per VARIANT (`emuX86 && variant.buildType == 'debug'`) and nothing
  throws there — a release built with the flag is simply arm64, and its name
  says arm64.
- **Verified** (2026-09-24, Windows cmd): `debug intel` puts both ABIs in
  `wudict2-android-arm64-x86_64-foss-debug.apk`; the x86_64 lib taken back OUT
  of that APK runs on the emulator and prints `wudict2-v0.1.0-43-g3178bc1-dirty`;
  plain `debug` and plain `release` stay arm64-only with the x86_64 lib sitting
  on disk the whole time; `release intel` and `debug arm` are refused.
- **AGP deletes the other APK from the variant's output dir**: building plain
  `debug` after `debug intel` removes the emulator APK (different output file
  name, same directory). Normal hygiene, but the two cannot sit side by side.
- **Owed**: the APK was never installed, so no device has run the app on the
  emulator. `adb install -r android/app/build/outputs/apk/foss/debug/wudict2-android-arm64-x86_64-foss-debug.apk`.
- **No Makefile target was added** — `make` is not installed on this machine, so
  it could not have been verified. `android-go-x86_64` plus an emulator APK
  target is the obvious follow-up for the Unix side.

## The theme button said "night" in daylight (2026-09-25, this session)

Reported from the phone: at launch the button beside the ✕ showed night while
the app was light, the first press changed nothing, and the second turned night
on. All three are one cause, and the cycle itself is fine: the stored mode is
`auto`, whose glyph was `◐` — at this size a black half-disc that reads as a
MOON. So `auto` announced night; the first press is `auto → light` and both are
light over a light system, so nothing moved; the second is `light → dark`.

- **`auto` now shows `☀☾`** — "follows day and night". It says what the state
  actually is, it cannot be read as night, and every press of the cycle is now
  visible. `light` stays ☀, `dark` stays ☾.
- **Verified on the emulator**, all three: auto → `☀☾` (41px) with a light page;
  light → `☀` (32px), light; dark → `☾` (27px), dark. Titles are
  "Theme: auto — following the system" / "Theme: light" / "Theme: dark".
- **A page can never be light while the glyph is ☾**: `dark` sets
  `data-theme=dark` and the crescent together. Checked by driving all three
  through localStorage and reading the resolved `data-dark` and the text colour,
  so the report could only have been the glyph.
- **The old glyph was named in two documents** — README's shortcut list and
  `pages/docs/start/search.md`. Both updated; the docs page is upstream's, so
  that one line is a trivial conflict owed on the next sync.

## The Settings panel: one shape per setting, and a door that names itself (2026-09-24, this session)

Three reports from the phone: the Results strip's controls were unlike each
other and wrapped into whatever column the width allowed, "Highlight matches"
was a lone toggle among two pairs, and the Appearance section named its door
after the heading it sits under.

- **The Results strip is a column of same-shaped rows.** `.facts.reading` is
  `flex-direction:column;align-items:flex-start`; every control is a `.seg` — a
  label, then a pair of buttons. Measured on the device: three rows at x=147,
  y=339/368/398, all 367 wide, no overflow.
- **`#hlBtn` is gone**; it is `#hlOn`/`#hlOff` now, wired through the same
  `for(const [id,want] of [...])` shape the other two pairs use. So `applyHL`
  returns `moved` and the search re-runs only on a real change — the old single
  toggle always moved, so it always re-ran. Its inline `style.color` went with
  it (the accent on the pressed half is the `.seg` CSS's job), and so did the
  highlighter's pen: the label it now wears says the same thing in the register
  the other two labels use.
- **The Appearance door is `Screen, background, CSS…`** — it names the sheet's
  three groups instead of repeating the heading above it. The heading, the
  sheet's own title, and the element's id and `title` are unchanged.
- **Verified on the emulator**: the rows stack and their labels share one x;
  clicking Off then On flips `aria-pressed` both ways; `hlOff` still
  round-trips through state.json; `go build ./...` and the asset tests pass.
- **The rows are two columns**: `.facts .seg>span{flex:1 1 auto}` lets the
  label take the slack, so the pair sits at the row's right edge. Measured:
  labels all at x=147, pairs all ending at 514, starting at 447/394/368 — the
  pair's own width is what differs, which is the shape that was asked for.

## The chips name their scope, with no receding state (2026-09-24, this session)

Two reports from the phone, one cause: `.chip.dflt`, which upstream gave the
"all" scope because All was a MODE there (the picker's All/Found toggle). This
fork removed that toggle — its picker has one mode — so "All dictionaries" is
an ordinary group, and a state that hides its own name and fades to the faint
colour no longer describes anything.

- **`.chip.dflt` is gone** — both rules and both `classList.toggle("dflt", …)`
  call sites. What is left is `#dictChip,#modeChip{background:none;
  color:var(--fg)}`: no accent tint, no receding state, one look in every mode
  and every scope.
- **`syncChips` names the all-scope** like any group. The only nameless state
  left is the transient where a saved id is not in the `<select>` yet, and it
  shows the bare glyph. `groupLabel()` already returned "All dictionaries" for
  the same value, so the chip now agrees with the rest of the page.
- **Measured on the device**: `all`, `g:dir:mono` and back to `all` all give
  `rgba(0,0,0,0)` + `rgb(59,50,41)`.
- **The name is shortened for the chip, not dropped.** "All dictionaries" is 16
  characters, the chip's cap is 11ch, and the cap cannot grow — a chip wide
  enough for the full name would leave the 320px field about 25px. So the chip
  says "All" (`SCOPE_SHORT`), the same bargain `MODE_SHORT` already strikes for
  "starts with", and the picker's list keeps the full wording exactly as the
  mode dropdown does.
- **The cost is the field**: the bare glyph was 40px and "All" is 49px, so the
  all-scope takes 9px rather than the 31px the full name would have. 320px goes
  118 → 101px, 540px goes 338 → 321px; a group name still renders at the 71px
  cap, and nothing overflows at 320/360/412/540.

## The mode chip looks the same in every mode (2026-09-24, this session)

Superseded by the above, kept because the reasoning is the same one: `.chip.dflt`
receded the mode chip to the faint colour while the mode was the default and
wore the accent tint otherwise, so the control the reader taps most often was
drawn as if it were off.

## The search bar's four controls are separated now (2026-09-24, this session)

Reported from the phone: the pill draws ONE frame around the pin, the field,
the mode chip and the dictionary chip, and inside it nothing said where one
control ended and the next began — the pin has no fill, the field is borderless
by design, and a chip wears a background only when it is NOT the default.
`.pill>*+*` now carries a hairline and .45em of left padding, and
`.pill .qled` moved with it (it sat flush with the field's edge).

- **The colour is `--line`**, so the hairline follows whatever theme or host
  background is in force — measured `#cdbb96` under the emulator's sepia +
  wallpaper + presets, not a fixed grey.
- **Measured on the device**: three 1px borders, all 32px tall (the pill's own
  inner height), at x=47/397/458; the glyph and the field both at x=56. No
  overflow at 320/360/412/540px; the field gives the separators ~19px at every
  width and still holds 118px at 320px.
- **Not touched**: the native mode/dictionary picker. An emulator screenshot
  made it look as if it painted white instead of the host background — the
  install's own presets (`background_image`, `compact`) had simply not applied
  at that moment, and with them on the dialog wears the background as designed.

## The Browse page wears the host background now (2026-09-24, this session)

Both doors — `Browse A–Z…` in the Settings panel (which lands on the chooser)
and a card's `Browse` in Dictionary settings (`?dict=<id>`, the word list) — are
ONE page in two states, and `browse.html` was the only page that did not wear
the host background. It now carries setup.html's/lemmas.html's hook and recipe.

- **The hook runs from sessionStorage, not from the URL.** This page is reached
  by TAPPING A LINK inside the WebView, so it never carries `shell_bg`/
  `shell_image`; what fires is the pair the app page wrote there (its own
  `applyBackground` runs on every `onPageFinished`). The URL half is kept
  because the sibling pages spell it and a reload of a page opened with it must
  not lose it.
- **It is the PAGES' recipe, not the app windows':** the page goes transparent
  over what the host paints, its surfaces stay translucent
  (`rgba(255,255,255,.14)` for the bar and the chooser card — the bar keeps its
  blur and loses its 94% fill), and the palette follows the TONE of the colour
  the host sent. `#styler`/`.menu-card` wear a different one (`--paper-bg` +
  `--paper-bk-image`) because they are surfaces drawn INSIDE the app.
- **The tone rules are `html:root[tone]`, not `html[tone]` — load-bearing.**
  This page's own dark blocks are `:root:not([data-theme=light])` and
  `:root[data-theme=dark]`, both (0,2,0); a bare `html[data-shell-tone=…]` is
  (0,1,1) and would LOSE, putting light text on a light paper. The type
  selector makes it (0,2,1). Verified on the device under an emulated dark
  system preference AND with `wudict_theme=dark` pinned — the tone still wins.
- **Scoped, not global**: with the three attributes removed the page computes
  exactly what it did before (`body` `#fbfaf8`, `--bar` `rgba(251,250,248,.94)`).
- **Verified on the emulator** (which turned out to have five indexed
  dictionaries): the chooser and `?dict=f24921fc1083` (300 word links, 29 chips)
  both report a transparent body, `rgba(255,255,255,.14)` bar and card, word
  links `#4d6b86`, `--bg` `#edd1a6`; the screenshots read correctly.
- **Left alone**: `browse.html` keeps its own palette and its own dark-mode
  handling for the no-background case, and still does not load `setup.css` —
  that sheet centres a single card, and this page is a full-bleed list.

## The colour window flickered on every launch (2026-09-24, this session)

Reported from the phone as "the colour picker window flashes when the app
starts", with video frames of it. The shell has no native colour picker, so it
is the page's `#colorDialog` — and it is not being OPENED: a trap on
`showModal`/`show`/the `open` property, installed before the page's own scripts
run, recorded nothing. It is being PAINTED.

- **Cause**: `app.css`'s `.panel-card` — the ☰ drawer's card, a class the four
  `<dialog class="group-dialog panel-card">`s also wear — sets `display:flex`.
  An author `display` outranks the user-agent's `dialog:not([open]){display:none}`
  whatever either specificity is, so a CLOSED dialog is drawn. The windows' own
  guard is `dialog.group-dialog:not([open]){display:none}` in
  `group-editor.css`, and index.html links that sheet at the END of `<body>`, so
  it arrives after the windows have been parsed. Measured over CDP: at 58 ms
  `#colorDialog` computed `display:flex` at 420x209 and `#dictSettings` at
  420x94, both gone by 63 ms. A phone spends long enough on the same two
  requests to read it as a flicker, which is why only the colour window was
  reported — it is the one of the two with content already in the markup.
- **Fix**: the same guard added to `app.css` beside `.panel-card` — in the sheet
  that INTRODUCES the display, not the one that owns the windows.
  `group-editor.css` keeps its copy with a note that neither may be dropped
  alone.
- **Verified on the emulator** (the debug APK installs there now): after the fix,
  338 frames over 6 s with no window ever painted, and both windows still open
  through their real paths — `colorDialogOpen` → `flex` 508x286,
  `showDictSettings` → `flex` 508x928 — and close back to `display:none`.
- **Recipe worth reusing**: `adb forward tcp:9222 localabstract:webview_devtools_remote_<pid>`
  (the pid changes on every app start), then `Page.addScriptToEvaluateOnNewDocument`
  + `Page.reload` to instrument BEFORE the page's scripts, and sample
  `getComputedStyle` per `requestAnimationFrame` for the first seconds — a
  one-frame flash is invisible to `screencap` in a loop and to the eye's own
  timing. This machine's python3 has no `websocket` module, so the client is
  written over `socket` by hand (~60 lines); the app's own server is on 6889 and
  its devtools target is a `page` called `wudict`.
- **Left as found**: the same author-`display`-beats-`hidden` trap is documented
  in app.css for `#styler [hidden]` and guarded per element. `#colorDialog` is
  NOT inside `#styler` (its `</div>` closes the sheet first), so that rule was
  never going to cover it — checked, not assumed.

## The launcher icon carries a "2" now (2026-09-22, this session)

The user asked for a "2" in the bottom-left corner of the wuDict2 icon ("рядом с
луппой", i.e. next to the lens) and listed four file paths they believed were
the icon. Two really are it: the Play listing's `icon.png` and
`featureGraphic.png`. The other two, `internal/tray/icons/{tray,tray-template}.png`,
are the DESKTOP systray's — rendered from `internal/server/web/favicon.svg` by
`tools/make-icons.sh` — and the phone's actual icon is not a PNG at all:
`android/app/src/main/res/drawable/ic_launcher_foreground.xml`, a
VectorDrawable, which their list did not contain.

- **Changed**: that VectorDrawable (the phone's adaptive + monochrome icon) and
  the two Play PNGs, rendered from the same mark. The digit's geometry, the mark
  rules it is composed against and its clearances are stated in both files'
  comments — not restated here.
- **Where the digit lives**: the VectorDrawable, and `tools/make-icons.sh`, which
  derives the Play renders from `favicon.svg` by substitution (the technique that
  script already uses for the macOS template) so an upstream change to the mark
  still reaches them. The digit is the only thing written down twice; both
  comments say so.
- **Left alone on purpose**: `favicon.svg` and its `pages/docs/assets/` copies,
  the tray PNGs, `packaging/*.icns|.ico`. Those are all 16–32px renditions, where
  a digit is a smudge, and they belong to the shared web/desktop artifacts of a
  fork that ships no desktop build. If the mark should change ALL the way, it is
  one `make icons` run plus the three SVG copies — but that is a permanent merge
  cost on upstream files. The user's list included the tray pair; that is why.
- **Verified**: the VectorDrawable's own XML was converted back to SVG and
  rendered against the intended drawing — RMSE 0.015, which is the lens's
  circle-vs-two-arcs antialiasing and nothing else, so the path on the phone's
  icon is the designed one. `tools/make-icons.sh` ran end-to-end in a throwaway
  tree with its `rsvg-convert` calls shimmed onto ImageMagick's librsvg delegate
  (this machine has no librsvg) and produced BOTH Play PNGs pixel-identical to
  the committed pair (`compare -metric RMSE` 0). The store images' canvases and
  mark scales were measured off the pair they replace (glyph box 2/3 of the 512
  icon, 300px wide in the 1024×500 graphic, both centred, both on full-bleed
  `#4c6680`), and a digit-less re-render of the feature graphic reproduced the
  old committed file to RMSE 3.2e-05.
- **Owed**: nothing was built or installed, so no device has shown the new
  launcher icon, its themed monochrome form, or the store listing. Static checks
  pass: `sh -n tools/make-icons.sh`, the vector's XML parse, `git diff --check`.
- **Found while working, left as found**: `tools/make-icons.sh` exits early when
  `iconutil` is missing (upstream's own behaviour), so on a machine without
  macOS tools `make icons` never reaches the Windows `.ico` section either. The
  new Play section was placed BEFORE that guard on purpose, so it runs anywhere.

## Dictionary word list → article, and the picker it lands in (2026-09-21, this session)

The user's task: in the Dictionary settings window every dictionary has a
**Browse** link, which opens that dictionary's word list, and **clicking a word
must open the standard view with that word shown**. What it turned out to be is
the answer to `docs/OPEN.md` **O11**, whose open question was exactly "is the
jump out of the word list enough (then nothing is built), or must the picker
itself offer a single dictionary". The jump was already there and was verified
rather than written; the picker half then came back — in the same message — in a
narrower form, and THAT is what this session changed: three edits in
`internal/server/web/index.html`, all of them about what the picker window says
and lists. No Java, no Go, no CSS, no `browse.html`.

- **What the path is**: the card's `/browse?dict=<id>` (`index.html:1548`, drawn
  only while the dictionary is indexed) → `browse.html`, whose every word is
  `/?q=<word>&dict=<id>&mode=exact` (`browse.html:217`) → `applyURL`
  (`index.html:3621`) sets `#mode`, sets `#dict` to that id and runs the search
  through the normal `doSearch`, so the scope is the app's own `dict=` scope.
- **Verified** (throwaway server on 127.0.0.1:6899, temp config + db dir,
  `test_data/`'s three dictionaries, desktop Chromium; server killed and the
  temp dir deleted afterwards), each step read off the live page:
  - all three cards carry a `Browse` link whose id is the one `/api/dicts` and
    `/api/browse` use;
  - the word list's anchors are `/?q=…&dict=…&mode=exact`, 300 per page;
  - clicking a word lands on `/?q=act&mode=exact&dict=56c232b0aaa1` with
    `#mode`=exact, `#dict`=that id and the chip naming that dictionary;
  - **the scope is real, not cosmetic**: the same word searched unscoped
    (`dict=all`) renders **two** sections (Asperger + Oxford), and the
    jump renders **one** — that dictionary's;
  - a Cyrillic headword survives the round trip
    (`а вместе с ним и` → `Zimmerman (Ru-En)`, 1 result);
  - Back from the article returns to the word list with its page
    (`/browse?dict=…&p=1`), because the jump records itself with `replaceState`
    and the word list's own page turn pushed.
- **Not verified on the phone, and the reason is a rule, not an oversight**: the
  installed APK (`wudict2-v0.1.0-25-g653c36a-dirty`, i.e. built from this tree,
  installed 2026-09-21 22:12) is a **release** build, so `setWebContentsDebuggingEnabled(BuildConfig.DEBUG)`
  leaves no `webview_devtools_remote_*` socket to attach to, and installing a
  debug build needs the user's word. The phone's server also answers 401
  without its access key, which is not ours to read. So the device pass is owed,
  and it is listed in `docs/ANDROID-UI-HANDOFF.md`.
- **Two observations left alone on purpose** (both written up in O11): the
  browse page's magnifier is `<a href="/">` — it loads the app with no query and
  leaves the word list on the history stack, the shape the user rejected on
  `/setup` and `/lemmas`, but here Back is the returning path and the magnifier
  says "Back to search"; and `browse.html` is the one page that does not wear
  the shell background (its own palette, its own dark-mode handling), which the
  other two pages do. Neither is a gap in the flow that was asked for.
- **One defect found in code that was already there, and then FIXED on the
  user's word** (all of it in `internal/server/web/index.html`): `livePickerRows()`
  filtered the answered dictionaries by the picker's own group
  (`new Set(activeUserGroupIds())`), so a view scoped to a dictionary that is
  **not a member of that group** rendered its article while the native picker
  received `rows: []` and `empty: "No results in this group"`. Reproduced on the
  throwaway server with a one-dictionary user group ("Russian only", holding
  Zimmerman) and `localStorage.wudict_picker_group` set to it: the jump to
  `/?q=act&mode=exact&dict=56c232b0aaa1` (Asperger, not a member) rendered **one**
  section, `#dict`/`#dictLbl` named Asperger, and `livePickerRows()` returned `[]`.
  The filter was redundant wherever the scope is chosen from the page (a scope of
  `all` or of a group is already resolved to that group's members *before* the
  search is sent), so all it could do was hide a row that was on screen — the
  filter is gone, and the comment in `livePickerRows` says why it must not come
  back.
- **The picker's dropdown now names the dictionary being searched** (the user's
  second ask in the same message): `scopedDictionary()` contributes
  `{id:"d:<id>", name:<dict label>}` to the payload's `groups`, spliced under
  "All dictionaries", and it is the payload's current `group` whenever the
  standing scope is one dictionary. **Derived, never stored** — no `state.json`,
  no `/api/groups`, no group editor, nothing in localStorage — so "when do we
  take it out" needs no mechanism: it is recomputed from the scope on every open
  and is gone the moment the scope is anything else. `wudictPickerGroupChanged`
  had to change with it: its "already current, do nothing" guard compared the tap
  against `pickerGroup`, which is a *stored fallback* in this state, so picking
  the reader's own standing group out of a scoped view would have been swallowed
  and the next payload would have put the spinner back on the dictionary — it now
  compares against what the spinner SHOWS (`scopedDictionary()`). `empty` also
  branches: "No results in this dictionary" when scoped, "No results in this
  group" otherwise. A `d:` id is unreachable as a selection (it is only ever the
  current entry) and fails the membership guard, so no `doSearch` resolver was
  added — `docs/OPEN.md` O11 carries that correction to its original sketch.
- **The caret no longer lands in the search field when a page arrives with `?q=`**
  (the user's follow-up: `history.js` opens its dropdown on the field's `focus`,
  so a word opened from the word list came up with the history drawn over the
  article — "this history must show only on manual input"). The cause was the
  boot focus rule inside `setPhase` (`index.html`), and neither of the two
  obvious suspects was involved: the `autofocus` attribute is dropped by the
  browser because the input is `disabled` while the page parses (measured:
  `activeElement` is BODY at `domcontentloaded`), and the shell's
  `wantAutoFocus` is armed only for a cold start with no query. The rule ran
  when the dictionary list became usable, where a deep link's field is still
  EMPTY — `applyURL`, which fills it and searches, is chained after that — so
  "an empty field means nothing to read" handed the caret to the word the reader
  had come to READ. It now has a third condition, checked on the URL rather than
  on the field: `!new URLSearchParams(location.search).get("q")`.
- **Verified after the fix**, read off the live page: `?q=act&mode=exact&dict=…`
  leaves `activeElement` at BODY with the article rendered and no dropdown —
  for a fresh load, for a click on a word in the word list, and for a Cyrillic
  word; an empty start and a `?dict=`-only start still take the caret (that is
  the "just type" intent); a hand-typed word still opens the dropdown with its
  matches; and `searchFor` — the path a double-clicked word takes, which the
  user pointed at as the model — leaves the caret alone, which is now what a
  `?q=` arrival does too.
- **The picker changes were verified in the desktop browser** on the same
  throwaway server, read off the live page: the scoped payload is
  `group:"d:…"`, `groups:[All Dictionaries, the dictionary, Russian only]`,
  `rows:[that dictionary]`, `empty:"No results in this
  dictionary"`, with one section rendered; picking the standing group out of that
  state resets the scope to all (`#dict`="all", the URL's `dict` back to `all`,
  the chip cleared, pickerGroup saved, search re-run — the tap the old guard would
  have eaten); picking "All dictionaries" from the same state does the same and
  the unscoped answer comes back (2 sections); the entry disappears from `groups`
  in both; choosing a dictionary in the `#dict` select (the desktop route) yields
  the same derived entry; a one-shot cross-dictionary-link scope (`searchFor` with
  a scope) leaves the standing scope at "all" — no entry in the dropdown, but the
  answered dictionary IS the one row, which is the fix above; `/api/groups` still
  returns only the two real groups, localStorage holds no `d:` value, and reading
  the payload changes no scope. `git diff --check`, `go build ./...` and
  `go test ./internal/server -run 'TestScriptsAreContentAddressed|TestAssetCacheHeaders|TestIndexTracksTheUserStylesheet' -count=1`
  all pass.
- **The Java side needed no change at all** — `DictionaryPicker.Live` renders
  whatever `groups`/`group`/`rows`/`empty` the payload carries, so the new entry
  is just another spinner row to it. That also means the phone has to confirm it:
  the spinner should read the dictionary while a word view is open, and the two
  ways out of it (the group, "All dictionaries") should work from there. No APK
  was built or installed.
- **No APK, nothing built or installed; `go build ./...` was run for the
  throwaway server only.**

## The Settings drawer has five sections now (2026-09-21, stage 1)

The user's plan for "bringing the panel into order": a `Dictionaries` section
at the top carrying the real counts and the doors, a section for how results
are shown, `Appearance`, `History`, `Info`. Four design questions were asked
and answered before the work (all four took the recommendation): the second
section is called **Results**, `Edit folders…` **and** `Rescan folders` both
moved into the panel, History got its own section, and the headings are
STATIC (nothing folds — the markup says why). Nothing was built for the phone;
the user builds and checks there.

- **What the drawer was**: one `<details>` called "Folders & setup" holding the
  folder paths, the search-history controls, `Browse A–Z…`, `Appearance…` and
  the About block, with the text-size stepper living inside its `<summary>` —
  so the machine's doors, the reading controls and the reference paths read as
  one undifferentiated column.
- **What it is now** (`internal/server/web/index.html`, one `<section
  class="sect">` per subject, in this order): `Dictionaries` (the
  `#folderSummary` line — `N folders · M dictionaries` from `/api/config`,
  unchanged code — then one `.mrow` per door: `#editDictSettings`,
  `#editGroups`, `#editFolders`, `#rescanBtn`, `#lemmaLink`); `Results`
  (`#hlBtn`, the Open-first and Sort segs, `#browseLink`); `Appearance`
  (`#fsCtl` with a `Font size` label, then `#stylerLink`); `History`
  (`#historyLength` + `#clearHistory`); `Info` (`#folderBody` — the paths —
  then the `.about` block). Every id, handler and href is unchanged — the doors
  are the same elements in new places, so no JS behaviour moved.
- **Then two corrections from the user's look at it** (same day, the count line
  and the paths): the **cog before the counts is gone**, so the line is text
  and nothing else — no disclosure, no target, nothing drawn as a control; and
  **the paths moved out of `Dictionaries` into `Info`**, with their group
  headings in **sentence case** ("Dictionary folders", "Library", "Config
  file" — they were lowercase in JS and uppercased by CSS; both are now normal
  case, `font-weight:600` is what marks them as headings). The `<details
  id="folders">` element is therefore gone entirely: no folding, and with it
  `localStorage.wudict_folders`, the `toggle` listener and the lazy-load path
  (`showPanel` already calls `loadFolders()` on every open, which is what fills
  the block). `#folders .grp/.frow/.rv/.warn` became `#folderBody …`; the
  count's rules became `.counts`.
- **Doors vs actions, as a rule**: a door is a full-width row with a chevron
  (`›`) on the right; the one row that ACTS where it stands (`Rescan folders`)
  has no chevron and keeps its ⟳ glyph instead. That is the whole reason
  `#rescanBtn` is the only row without the marker.
- **The settings window lost three of its four toolbar commands.** `Edit
  folders…`, `Rescan folders` and `Lemmatization…` are per-COLLECTION, not per
  card, so they moved to the panel's `Dictionaries` section — the user's own
  argument: adding a folder is how dictionaries arrive, so the thing that
  configures what appeared belongs one row away. `Full-text for every
  dictionary…` stays, because it is the bulk form of the per-card switches
  directly under it.
- **CSS**: `.sect` / `.sect-h` (heading + hairline, the last section without
  one), `.facts .mrow` / `.mrow.door` / `.facts .rowlabel` and `.counts` are new
  in `app.css`. The stepper's rules moved from `#folders .fs*` to `#fsCtl*`, the
  paths' from `#folders .grp/.frow/.rv/.warn` to `#folderBody …`, and the bare
  `.about` rules became `.sect .about` — **load-bearing**, because `.about` is
  also the class of a card's "About this dictionary" disclosure further down,
  whose links a bare `.about a` would have restyled.
- **The stepper's click-suppression handler is gone** (`$("fsCtl")…
  preventDefault/stopPropagation`): it existed only because the stepper sat
  inside a `<summary>`, whose toggle is a click's default action. The row is a
  plain div now. The dimming at the bounds (`.lim`) stayed, and its comment now
  says what actually holds the range (clamping in `applyFS`).
- **Comments were rewritten, not just moved**: this file's markup comments are
  the design record, so the ones that justified the old single-`details`
  layout, the "no Font size label" rule (the label fits now), the glyph choices
  for `Edit folders…`/`Lemmatization…` (their rows are labelled doors now, so
  the folder and stem glyphs are gone; the ⟳ on Rescan is the only glyph left in
  the panel) and the reading strip's "no heading over them" all had to say
  something true about the new shape.
- **Verified** (throwaway server on 127.0.0.1:6899, temp config + db dir,
  `test_data/`, desktop Chromium; server killed and the temp dir deleted
  afterwards): five sections in order with the right headings, the last one
  without a hairline; seven `.mrow` rows, all one line tall at 320px with **no**
  horizontal overflow and no row overflowing its box; the chevrons present on
  doors and `none` on `#rescanBtn` (computed `::after`); `#folderSummary`
  reading "1 folder · 3 dictionaries" as a **plain** line (`.counts`, no svg, no
  `<details>` anywhere in the panel) with the paths drawn in `Info` — the three
  group headings in sentence case (`text-transform: none`, weight 600) and the
  three path rows visible without unfolding; `#editDictSettings` opening the
  modal window (3 cards, `:modal`, toolbar holding `#ftsAllBtn` only) and
  `#ftsAllBtn` opening its box ("Index 3 dictionaries" / Cancel);
  `#editGroups` opening the group editor; the stepper's two presses taking
  `--wd-fs` 15px → 17px, updating `#fsVal` and persisting `ui.fontSize=17`
  server-side; no duplicate ids anywhere in the page; the inline script still
  parsing (the panel and the picker functions all present). `go build ./...`,
  `git diff --check` and
  `go test ./internal/server -run 'TestScriptsAreContentAddressed|TestAssetCacheHeaders|TestIndexTracksTheUserStylesheet'`
  pass. No APK built or installed — the phone pass is owed.
- **Known cosmetic question, left for the user**: the `Appearance` heading and
  the `Appearance…` row inside it now say the same word. Renaming either (the
  row could name what the sheet holds — the screen edges, the window
  background, the user's CSS) is a one-line change if they want it.

## The picker has one mode: Found (2026-09-21)

The user's instruction: the Dictionary picker All/Found toggle goes away,
the app works in Found mode only, and no saved state or code path may switch
it back. Nothing was built for this session (the user builds and checks on
the phone); the verification is below.

- **Removed from `internal/server/web/index.html`**: the panel row
  (`<span class="seg hidden" id="dictionaryPickerMode">` with `#pickerAll` /
  `#pickerFound`), its two `app.css` rules (`#dictionaryPickerMode{flex-basis:100%}`
  and `#dictionaryPickerMode.hidden{display:none}`), `window.wudictSetDictionaryMode`
  together with the click wiring that kept its pairs of `aria-pressed`, and
  `window.wudictFoundDictionaryPicker` — the `window.prompt` twin of the
  native picker, which the shell could never reach anyway (see the retired
  note below). The mode variable `window.wudictFoundDictionaryMode` is gone
  from the page.
- **What the single list is**: `livePickerRows()` now always returns the
  dictionaries that ANSWERED the current search, in the order their sections
  appear, filtered to the picker's group; `livePickerPayload()` always sends
  `found:true` and always the found wording of `empty` ("Searching…" / "No
  results in this group"), and no longer sends `selected` (that field was the
  All-mode "chosen dictionary"); `wudictPickerDictionarySelected` always opens
  the section and scrolls to it. The Java side is untouched on purpose:
  `found:true` only chooses the presentation (plain rows, no radio circles)
  in `DictionaryPicker.Live`, and `show()` still needs its `found` flag false
  for `mode` / `stylerPreset` / `groupSelect`.
- **Removed from the shell**: the mode injection in `Shell.applyBackground`
  (it read `ShellPrefs.foundDictionaries`), the `wudict:dictionary-mode`
  prompt handler in `Shell.windows().onJsPrompt`, the unreachable found
  branch in `DICTIONARY_PICKER_JS`, and `ShellPrefs.FOUND_DICTIONARIES` +
  `foundDictionaries()`. The stored `found_dictionaries` boolean is simply no
  longer read — a value written by an older build cannot change anything, and
  nothing has to be migrated or erased.
- **Consequence worth knowing**: a single-dictionary search scope can no
  longer be set from the picker (that was the All-mode radio list). Groups
  still scope the search, cross-dictionary links and `?dict=` URLs still scope
  a view, and picking a group in the picker resets the scope to all. The user
  then raised how a single-dictionary scope should be chosen at all (their
  sketch: a group holding one dictionary, added on demand, driven from a
  word-list window) — that design question went to `docs/OPEN.md` as **O11**,
  which recorded the fact that matters most: the entry point already exists
  (`Browse A–Z…` → `browse.html` → `/?q=<word>&dict=<id>`), so nothing needs
  to be built for "show this word in this dictionary". **O11 is now CLOSED**
  (see the section below): the user answered with the jump, and it was verified
  rather than built.
- **Verified** (throwaway server on 127.0.0.1:6899, temp config + db dir,
  `test_data/`'s three .dsl.dz, desktop Chromium; server killed and its temp
  dir deleted afterwards): the page's inline script parses and runs —
  `wudictNativeDictionaryPicker`, `livePickerRows`, `livePickerPayload` and
  `wudictPickerDictionarySelected` are all functions, while
  `wudictFoundDictionaryPicker`, `wudictSetDictionaryMode` and
  `wudictFoundDictionaryMode` are all `undefined`; `#pickerAll`,
  `#pickerFound` and `#dictionaryPickerMode` do not exist; the panel's only
  segments are **Open first** and **Sort dictionaries** (its button ids are
  `openOrder`, `openFast`, `sortAZ`, `sortOwn`); after a real search for
  "time" the live payload reads `{found:true, group:"all", empty:"No results
  in this group", rows:[Asperger…, Oxford…]}` — the two dictionaries that
  answered, with no `selected` key. Also: `go build ./...`, `git diff --check`,
  `go test ./internal/server -run 'TestScriptsAreContentAddressed|TestAssetCacheHeaders|TestIndexTracksTheUserStylesheet'`
  and `:app:compileFossDebugJavaWithJavac --offline` all pass. No APK was
  built or installed; the device pass (panel rows, the picker's list and the
  jump) is still owed — it is listed in `docs/ANDROID-UI-HANDOFF.md`.

## Retired: picker scroll speed note (2026-09-21, earlier the same session)

The user asked whether the picker's jump to a dictionary can be instant
instead of animated. It can, and the lever is the system's, not the app's.

- Three call sites in `internal/server/web/index.html` are written
  `behavior:lessMotion.matches?"auto":"smooth"`: 1094 (the picker's jump,
  the one a tap now runs), 1112 (tap on a section's summary) and 3385
  (`revealAt`). No `scroll-behavior` rule exists anywhere in `web/` (only
  `overscroll-behavior`), so `"auto"` really is an instant jump rather than a
  CSS-requested glide.
- **On Android, `prefers-reduced-motion: reduce` IS "animator duration scale
  == 0"** — verified in Chromium source, not from memory:
  `ui/accessibility/android/java/src/org/chromium/ui/accessibility/AccessibilityState.java`
  has `prefersReducedMotion() { return getAnimatorDurationScale() == 0.0; }`,
  and `AccessibilityStateDelegateImpl` reads
  `Settings.Global.ANIMATOR_DURATION_SCALE` (default 1f) with a
  ContentObserver on it. That impl is also what `getDelegate()` constructs
  when no embedder installs one, so WebView is covered without any shell
  code of ours. So Developer options → "Animator duration scale: off" (the
  same scale Android's Accessibility → "Remove animations" zeroes) makes
  these jumps instant in the shipped build. The user's phone reported 1.0,
  i.e. smooth today; the setting was READ only, never changed.
- If it is ever made instant in code, the one thing to watch on the device is
  a jump
  measured before the opened section settles (the clamp described at
  index.html:3197) — the codebase's answer to that class of bug is
  `revealFirstMark`'s ResizeObserver settle loop.

## Appearance sheet: collapsible groups + the Screen rows (2026-09-21, this session)

Same branch (`Dictionary-Settings2`), uncommitted, on top of the dictionary-settings
window. The user asked for two things, then sent three rounds of corrections from phone
looks (the notes say which is which). The user builds, so this session built no APK — only
the routine checks, all listed at the end of this section.

- **All three groups have one shape, and the whole sheet is one scroll.** Each section is
  a wrapper holding a `.group-bar` and a body (`#appearanceBackground`, `#appearanceScreen`,
  `#stylerBody`); folding a bar hides its body, and every bar travels with what it folds.
  All three live inside `#appearanceGroups`, which is now the sheet's whole scrolling area
  (`flex:1 1 auto`), so the sections behave identically instead of two scrolling while the
  third was pinned below them. Order: `Screen`, `Window background`, `Custom CSS`. This is
  the third correction of the same area from phone looks (first the bar was pinned with its
  contents and read as a section that never moved; then it was pinned above them and the
  bar scrolled away from the contents it speaks for; now everything scrolls together, which
  is what "behaves like the two sections above" means). Open/closed is `appearanceOpen` and
  is never read back off the DOM, because `appearanceRender` re-runs on every bridge round
  trip. Folding Custom CSS also puts `no-css` on the sheet: `height:auto`, and the page's
  bottom padding follows the MEASURED sheet height (`--styler-shown`,
  `appearanceSheetHeight`), so a folded editor gives the screen back rather than leaving a
  full-height sheet with an empty lower half. The editor is now a fixed 16em box and the
  Files/Presets panes are plain blocks (their own scrollbars are gone — two scrollers
  fighting for one thumb on a phone).
- **The caret's box is brought back into view** (`stylerBoxIntoView`): on the transition
  into editing (`stylerEditing`) and whenever the scroller's height changes while the caret
  is in the box (`stylerFit` compares `clientHeight` — the keyboard shrinking the WebView is
  a height change, and a reader scrolling by hand is not). It scrolls the caret's box to
  just under the scroller's top edge, which is the one thing that must be on screen while
  typing.
- **Both colour fields gained a pipette** (`#appearanceColorPick`, `#edgeColorPick`) that
  opens the platform's own chooser through one hidden `<input type="color">`; which field
  the dialog fills is remembered while it is open (the ＋ tile's bargain for uploads), and
  the value is applied on `change`, never on every drag frame. The Screen group's `Colour`
  row is still shown ONLY for "a colour you pick" (`appearanceRender`: `edgeColorRow.hidden
  = edge !== EDGE_CUSTOM`).
- `go build ./...`, `git diff --check` and `:app:compileFossDebugJavaWithJavac` (from
  `android/`, `ANDROID_HOME` passed explicitly) all pass for the first pass. **The three
  correction rounds that followed are NOT browser- or build-verified** (the user asked for
  no build): the Screen/Window-background swap, the Custom CSS bar leaving the pinned spot,
  and now the one-scroller layout, the box-into-view and the pipette buttons. Static checks
  only for those: markup tag-balanced with the intended nesting (`#styler` = head +
  `#appearanceGroups` + note; the scroller holds the three group wrappers; each wrapper
  holds its bar and its body), app.css braces balanced, no JS reference to an id that no
  longer exists. No APK built, nothing installed.
- **The automatic collapse stayed, and is now overrulable.** The caret in the CSS box
  still stands `Window background` down and returns it on blur — the behaviour the user
  described from the phone — but it is EDGE-triggered (`stylerEditing`): a bar tap during
  editing clears the pending restore, so the 300ms poll cannot undo the reader's tap
  (verified: held for 670ms, more than two poll ticks).
- **The `Screen` group is the shell screen's two rows, moved.** `Edges of the screen`
  and `Hide while you read` became page-drawn dropdowns with the explanation under each
  (wording and option order taken verbatim from the strings the shell screen used), plus
  the margin-colour field, which is shown ONLY while the mode is "a colour you pick"
  (`appearanceRender`: `edgeColorRow.hidden = edge !== EDGE_CUSTOM`).
  `SettingsActivity` lost the whole section, `edgeRow`, `edgeColorDialog`, `choiceRow`
  and the strings; the bridge grew `edgeMode`/`edgeColor`/`bars` (validated both ways)
  and `MainActivity.refreshScreen()` applies them to the live window — the edge mode
  decides who wears the insets, so it re-asks for those as well. The floating lookup
  window only stores them; the app window picks them up on its next focus gain.
- **The dropdowns are the page's own menus, not `<select>`s** — the user's rule: the
  popup must wear the app's background and wallpaper, and the platform's own popup window
  cannot. They are `position:fixed` and placed from their anchor's rect (the sheet
  scrolls, and an absolute menu would be cut off at the scroller's edge), flip above the
  anchor when they would run off the bottom, follow it on scroll, and close on a tap
  outside. `.menu-card` is the shared shell-aware surface (the recipe `#styler` and the
  history dropdown already used), so they are windows like everything else.
- **The caret's box is kept in view, and the tab row with it** (`stylerBoxIntoView`): on the
  transition into editing (`stylerEditing`) and whenever the scroller's height changes while
  the caret is in the box (`stylerFit` compares `clientHeight` — the keyboard shrinking the
  WebView is a height change, a reader scrolling by hand is not). What it pins is the
  SECTION's top, not the box: the `Custom CSS` bar and the row of `App / Article / Files /
  Presets` tabs sit 8px under the scroller's top, because a row of tabs scrolled up under
  the sheet's head is a row of tabs nobody can press (the user's report). The box then takes
  the rest of the scroller and scrolls internally for its caret, as a textarea does. The
  `placed`/`fits` test is load-bearing: a section taller than the scroller can never satisfy
  "box bottom visible", and asking for the same scroll on every poll tick would jitter the
  sheet by the 8px gap for as long as the keyboard is up.
- **`hidden` is now authoritative inside the sheet** (`#styler [hidden]{display:none}` in
  app.css, replacing the per-id list). An AUTHOR rule such as `.appearance-row{display:flex}`
  outranks the user-agent's `[hidden]{display:none}` whatever the specificity, so an element
  the page hides by setting `.hidden` stays on screen: that is why the Screen group's colour
  row showed for EVERY mode (reported from the phone) — it is an `.appearance-row`, and the
  row was hidden by the property alone. The strip arrows already carried a guard of their own
  for the same reason (`.stripArrow[hidden]`, HANDOFF-worthy since the fourth appearance
  round). The whole area's elements are inside `#styler`, so one rule covers them; the check
  script that found this lists every element whose `hidden` the page toggles against the
  author `display` rules on that element itself (descendant rules are not the subject).
- **What the first browser pass MISSED, and why**: it asserted `!document.getElementById(id)
  .hidden` — the property — instead of the computed `display`, so an element that was
  "hidden" in every assertion was still painted on the phone. When a check is about what the
  reader sees, read `getComputedStyle(...).display` (or a rect), never the attribute.
- **The pipette IS the swatch, and it opens a window of the app** (`#colorDialog`,
  `dialog.group-dialog.panel-card` like the other windows): three R/G/B sliders with the
  channel's own gradient on the track, a live preview + hex, `Apply` and a ✕. The field's
  own recent-colours menu is unchanged; the window replaces the PLATFORM chooser
  (`<input type="color">`), because a picker reached from a field that already holds a
  colour has to open AT that colour and the platform's dialog is not ours to set — on the
  phone it came up at black while its own swatch showed the colour. `Apply` goes through
  `colorValueApply` (the same door a typed hex uses, so the shell applies and remembers it);
  the ✕ closes with nothing changed. `colorPickSync` paints the pipette with the colour it
  would open at, with the glyph flipped to dark on a light swatch; a field with no colour
  yet keeps the plain button. `stylerCloseSheet` closes the window with the sheet — a modal
  left standing over the page with nothing to apply it to is the bug that would otherwise
  follow.
  **The library question, answered with facts** (the user asked about
  jaredrummler/ColorPicker before this was written): it is real (`com.jaredrummler:
  colorpicker:1.1.0`, Maven Central, Apache-2.0, minSdk 14, published 2019-01, repo last
  pushed 2024-07) and one line in app/build.gradle would fetch it, but its POM depends on
  `androidx.appcompat:1.0.2` + `androidx.preference:1.0.0`, i.e. AppCompat and its
  transitive set inside an app whose build.gradle says outright that it has no dependencies;
  its dialogs are AppCompat widgets, which want an AppCompat theme our plain activities do
  not use. Against that, `tools/notices.sh` walks the GO module graph only, so a Gradle
  dependency would have to be written into the notices by hand (and into
  `tools/notices.head.md`, or the next `make notices` drops it). Three sliders in the page
  cost none of that, and the page already owns every other appearance surface.
- **A flexbox lesson from the middle of these rounds** (the rule itself is gone with the
  one-scroller layout, the lesson is not): `flex-shrink` is weighted by base size AND a
  later `flex:1 1 auto` shorthand wins over an earlier `flex-shrink` — so a rule that tries
  to make the work area give way has to be stated after the panes' own shorthands, or it
  silently does nothing. Measured then at 390×780: settings 88px with it wrong, 298px with
  it right.
- **Verification status.** `go build ./...`, `git diff --check` and
  `:app:compileFossDebugJavaWithJavac` (from `android/`, `ANDROID_HOME` passed explicitly)
  pass for the FIRST pass, which is also the last thing actually driven in a browser
  (throwaway server, temp config, one stub `.dsl`, the shell bridge stubbed in the page
  exactly as `Shell.windows().onJsPrompt` answers it, and the host's background hook called
  with `#f4ecd8` + `paper_01.jpg`): every bar toggles, the caret collapse and its override
  behave, both menus open at the anchor's width with the current choice ticked and flip
  above when there is no room below, picking a mode round-trips through the bridge
  (`edgeMode:3` → the colour row appears; `#204060` → remembered; `4` → the EDGE_NONE
  explanation), all three menus compute the paper tint AND the wallpaper URL, a collapsed
  group survives a later `appearanceRead()`, and 320×640 has no horizontal overflow. That
  browser session could not inject clicks (`click` times out, `cua.click` inert) and
  `screenshot` timed out, so interactions went through the real handlers and layout was read
  from rects and computed styles — nothing was LOOKED at. **The three correction rounds
  after it are static-checked only** (the user asked for no build): the Screen/Window
  background swap, the Custom CSS bar leaving the pinned spot, and the one-scroller layout
  with the box-into-view and the pipettes. Static checks: markup tag-balanced with the
  intended nesting (`#styler` = head + `#appearanceGroups` + note; the scroller holds the
  three group wrappers; each wrapper holds its bar and its body), app.css braces balanced,
  no JS reference to an id that no longer exists. No APK built, nothing installed; phone
  checks are listed in `docs/ANDROID-UI-HANDOFF.md`.

## Configuration and Lemmatization pages (2026-09-20, this session)

Same branch, after the settings window. The two pages the window's toolbar opens
were given the closer they lacked, and lost the two navigation buttons they had.

- **The ✕ that "closes the window" is `history.back()`, not a link to "/"**
  (checked first, on the user's question: neither page has any special handling
  — both carried a plain `<a href="/">`, and `Shell.openExternal` lets
  same-origin URLs load in the WebView, so there is no shell channel involved).
  A link to "/" loads the app from scratch AND leaves the page's own entry in
  the history, so the phone's back gesture walks straight back into the page
  that was just closed. Back consumes that entry. Verified: ✕ on /setup →
  "/" again, and the next back gesture lands on an earlier entry, never on
  /setup. A first run (the server serves /setup for "/", so `history.length`
  is 1) has nothing behind it and hides the ✕.
- `web/setup.css`: `.card` gained `position:relative` and a shared `button.x`
  rule — the closer both pages wear, in the card's top-right where the app's
  windows put theirs. `web/setup.html` and `web/lemmas.html`: the ✕ markup
  (`#closePage`) and its handler; removed "Back to dictionaries" (and the JS
  line that un-hid `#cancel`, plus the now-dead `#save~.btn` rule) and the
  "🔤 Lemmatization" link with its one-line footnote; on the lemmas page,
  "Back to dictionaries" and "Folders…".
- `web/lemmas.html` also gained setup.html's shell-background hook
  (`wudictShellBackground`, `data-shell-image`/`-custom`/`-tone` and the
  transparent-body rules): the Android host calls that hook on every
  `onPageFinished` (Shell.applyBackground), so the page was being told about the
  wallpaper and ignoring it. Both pages now tint identically over the shell
  background — verified by calling the hook with `#f4ecd8`, image on: same
  attributes (`custom`+`image`+`tone=light`), same `rgba(0,0,0,0)` body and
  `rgba(255,255,255,.14)` card on both, and the same light-tone palette.
- Not verified on a phone; the ✕ position/behaviour and the wallpaper on the
  lemmas page still want a device pass.

## Dictionary settings window (2026-09-20, this session)

Branch `Dictionary-settings`, cut from `dev` (`adb5507`, plus the HANDOFF
commit); nothing committed yet. The per-dictionary cards left the Dictionaries
panel for a window of their own — `Dictionary settings`, opened by a new
`Edit dictionary settings` button beside `Edit dictionary groups`, the same
pattern as the group editor (modal `dialog.group-dialog.panel-card`,
`showModal()`). A second pass, on the user's instruction, took the enable/
disable feature out entirely and moved the machine's four actions in with the
cards.

- `web/index.html`: `<dialog id="dictSettings">` sits right after `#panel` and
  owns `#panelList` plus a fixed `.facts.configbar` holding `#editFolders`,
  `#rescanBtn`, `#lemmaLink`, `#ftsAllBtn`/`#ftsAllBox` (moved out of the panel's
  folder drawer, which keeps the history controls, Browse A–Z…, Appearance…, the
  paths and About). New JS: `showDictSettings`, a `dictListTop` scroll listener
  on `#dictSettingsScroll` (a closed dialog's scroller loses its offset, and a
  hundred cards is a lot of place to lose), and `reissueIfCorpusMoved`, which
  took over the re-issue `hidePanel` used to do. ONE snapshot (`panelSnap`)
  serves both closers: the first of them to close searches once if the ORDER
  moved, the second finds the snapshot level and searches nothing.
- **No "disabled dictionary" any more** — both switches are gone from the UI and
  the client no longer honours the stored `off` flag (a dictionary switched off
  before would otherwise stay invisible with nothing left to switch it on).
  `savePrefs` omits `off`, so the server's omitempty field goes false and
  `state.json` converges; `prefs.Off` (warm-up skip only) is untouched. Deleted:
  `disabled`, `enabledOrderedIds`, `toggleDict`, `setAllEnabled`,
  `syncPanelHeader`, the card's `.en` input and its `change` listener; the
  empty-scope message now names an empty group or says "no dictionaries to
  search". CSS: `.allrow`, `.allrow label`, `.pd.off` removed.
- `web/group-editor.css`: `#dictSettings` height/overscroll plus `.configbar`
  and `#ftsAllBox` pinned (`flex:none`). The earlier `:not(.en)` exclusion on the
  checkbox rules was reverted — with the switches gone, no `.en` element lives
  inside a `.group-dialog` any more.
- **Overlays must survive a trip to another page** (reported from the phone three
  times: first the window came back broken behind the drawer, then — after a fix
  that closed everything on the way out — the reader landed on the word cards,
  and the same again on a build where the page was not restored at all).
  `Lemmatization…` / `Edit folders…` are ordinary navigations, and the page can
  come back in two shapes, which is why there are two mechanisms in index.html:
  - **restored document** (`pageshow` with `persisted`): the DOM comes back with
    the drawer still `.show` and the window still carrying `open`, but NOT the
    TOP LAYER a modal `<dialog>` lives in — so the window arrives non-modal and
    painted under the panel (half of it hidden on a phone, ✕ behind the drawer).
    The handler closes and re-shows it, which re-enters the layer.
  - **reloaded document** (what the phone actually does): no overlays at all, so
    `rememberOverlays` writes them to `sessionStorage.wudict_overlays` on
    `pagehide` and `reopenOverlays` puts them back on a `back_forward` load
    (navigation type; verified end-to-end in the desktop browser, where the
    debugger disables the page cache and the back navigation is therefore a real
    reload — the window came back open and modal over the drawer). The note is
    rewritten on every pagehide, so a window closed after the return cannot come
    back, and `reload`/`navigate` loads reopen nothing.
  - `dialog.group-dialog.panel-card` keeps `z-index:300`, so a window is on top
    even before either mechanism runs.
  An earlier attempt closed everything on `pagehide`; the user rejected it.
- **The dialog is a framed card, and the two pages were renamed** (the user's
  screenshots settled what three rounds of wording had muddled: the window that
  went edge to edge was the Dictionary settings DIALOG, and the reference is the
  frame the Edit Folders/Lemmatization pages draw). So the
  `@media (max-width:600px)` block in group-editor.css that made the dialogs
  full screen on phones is REVERTED — the dialog is the centred
  `min(560px,94vw) × min(650px,85dvh)` card with a 12px radius at every width
  (measured at 360×780: 322×650, 19px margins — the pages' cards have 1em, i.e.
  16px). The ☰ drawer went back to its side sheet the same day: for one round it
  had been turned into a card too, on a previous message whose window names the
  user then corrected ("это мой косяк") — a `sheet vs card` question that no one
  asked, and one `git checkout` away if it is wanted after all.
  `setup.html`: `<h1>` and `<title>` are "Edit Folders" (were "wuDict
  Configuration"/"wudict Setup"); `lemmas.html`: "Lemmatization" (was "wuDict
  Lemmatization"). No test asserted either string.
- **Around a window: the background, not the app** (the last visible difference
  the user named: beside the pages there is only the wallpaper, beside the
  window the app showed through the 40% backdrop). `group-editor.css`:
  `.group-dialog::backdrop` is `var(--paper-bg,var(--bg))` — the colour the
  host sends — and `html[data-shell-image] .group-dialog::backdrop` paints
  `var(--paper-bk-image)` stretched `100% 100%`, the recipe `#styler` and
  `.color-history` already use. Both vars come from `wudictShellBackground`, so
  the modal now sits on exactly what the pages sit on and nothing of the app
  shows beside it (verified with the hook called as the host calls it: backdrop
  `rgb(244,236,216)` plain, plus `url(...)` stretched with an image, and the
  drawer + results no longer visible around the card). The ☰ drawer keeps its
  own dimmed overlay — `#panel` is chrome, not one of the windows the user
  compared; say the word if it should wear the background too.
- **On a phone the window IS the pages' card** (three rounds of phone reports
  settled it: bands too big → flush to the edges and the title too far in →
  finally "look at Edit Folders": a 1em margin on every side and 1.618em of
  padding inside, which is exactly what `.card` inside a `body{padding:1em}`
  wears). `group-editor.css`, `@media (max-width:600px)`:
  `inset:calc(1em + var(--wd-inset-top,0px)) 1em calc(1em + var(--wd-inset-bottom,0px))`,
  `width/height:auto`, `max-width/max-height:none`, `padding:1.618em`, plus
  `#groupEditor,#dictSettings{height:auto}` to lift the desktop heights.
  Measured at 360×780 against `/setup`: both cards start at 16px, both titles at
  43px, card 328 wide — the window 328×748 (16px bands top and bottom), the page's
  card running off the bottom. Desktop keeps the centred 560×650 window
  (85px margins at 1100×820).
- **The box stretches via `inset`, never a viewport unit** — and that is
  load-bearing, not style. With `height:100dvh` the reopened window came back
  small and centred after a trip to Edit Folders (the user's screenshot): the
  Android WebView reported a dynamic viewport ~145px SHORTER than the window at
  that moment, and `margin:auto` then centred the short box in the full
  viewport, i.e. exactly the bands the same user had complained about an hour
  earlier. `height:auto` under a four-sided `inset` cannot do that.
- **The window's height follows the WebView, and the shell was shrinking it**
  (the last phone report: the first open is perfect, but after a trip to Edit
  Folders and back the window is small again, with bands above and below).
  The box is viewport-driven by design (`inset` + `auto` sizes, no viewport
  unit), so a smaller window means a smaller viewport — and `MainActivity`
  padded the root with `Math.max(bars.bottom, ime.bottom)` **whenever the IME
  reported a height, visible or not**. A callback with a stale keyboard frame
  (or an IME going away while a page loads) therefore shrank the whole page:
  ~145px lost, which is the size the phone's screenshot showed the window
  losing. Fixed by gating both places on `insets.isVisible(Type.ime())`:
  `imeUp ? ime.bottom : 0` for the padding and `toPage && !imeUp` for the
  published inset. Java compiles (`:app:compileFossDebugJavaWithJavac`).
  NOT verified on a device — this is the diagnosis to test first, and if the
  window still shrinks the next step is a debug build
  (`setWebContentsDebuggingEnabled(BuildConfig.DEBUG)` is already there, so the
  debug APK can be inspected over CDP).
- **Diagnosed on the device, over CDP** (the user installed a debug build —
  `setWebContentsDebuggingEnabled(BuildConfig.DEBUG)` is already in both
  activities — and invited a look; `adb forward tcp:9222
  localabstract:webview_devtools_remote_<pid>` plus a ~60-line WebSocket client
  written over `node:net` in the node REPL, since that kernel has no WebSocket
  global). Findings, all measured on the live page:
  - The failing flow does NOT fail on a build carrying the `inset`+`auto` CSS:
    window open (viewport 763, window 731 = 763 − 2×16, modal, over the drawer) →
    `Edit folders…` → the ✕ → back → the window is reopened at 731 with the same
    viewport. The release build on the phone at the time was the OLDER
    `100dvh` one (its `.so` carries `2.618em`/`100dvh`, not `padding:1.618em`),
    which is why the user still saw it.
  - `--wd-inset-*` are `0px` in the default inset mode (the shell pads its root
    and publishes zeroes; only EDGE_NONE hands them to the page), so the CSS
    calc is a no-op there — the window follows the WebView, period.
  - Returning with NO overlay open leaves Chromium's restored focus on `#q` and
    the keyboard up (viewport 431), which is correct-but-surprising rather than
    a bug; with the drawer+window open the modal's `showModal()` takes the focus,
    so the keyboard stays down and the window comes back full size.
  - The `insets.isVisible(Type.ime())` guard in MainActivity is therefore
    unproven belt-and-braces: nothing reproduced a stale IME inset. It is kept
    because padding for a keyboard nobody can see is wrong on its face.
- **"Clear browser cache" in the app's Settings** (the user's answer to the
  stale-assets question: they asked for the control by name, so the label is
  theirs). `SettingsActivity` grew a hint + button under the Advanced block;
  `Shell.clearWebCache(Context)` empties the WebView's resource cache (a
  throwaway WebView's `clearCache(true)` — per-APPLICATION despite being an
  instance method) and `Shell.EXTRA_RELOAD` makes the window that shows the page
  load it again (MainActivity.onNewIntent). It touches no dictionary file, no
  prepared index and no localStorage: the hint says so, which is why the tap is
  not confirmed.
  Verified: the strings are in the built APK (`aapt2 dump strings`), the Java
  compiles, and the reload half was driven live on the phone (`am start … --ez
  wudict.reload true` → the page's navigation type went `navigate` → `reload`).
  The cache-emptying half could NOT be tapped through adb: this MIUI phone
  refuses input injection (`SecurityException: … INJECT_EVENTS`), and the button
  is native UI, so it awaits the user's first tap.
  The button itself sits BELOW the Close button, on the user's instruction: the
  last thing on the screen, since it is a last resort and not one of the
  settings.
- **The ☰ drawer is now titled "Settings"** (the user's rename): `<h2>`, the
  panel's `aria-label`, and the button's `title`/`aria-label` — the button had
  `aria-label="Manage dictionaries"`, both now say Settings. Verified live on
  the phone over CDP (`#panel h2`, getAttribute on `#panelBtn`) and in a
  screenshot of the running app; `docs/DICTIONARY-GROUPS.md` ("☰ → Settings →
  Edit dictionary groups") and the picker-mode note in the UI handoff were
  updated with it, and the ☰ glyph survived the edit (checked — the first
  attempt at the button line dropped it).
- Verified in the desktop browser against a throwaway server (temp config, two
  stub `.dsl`s, port 6899): panel holds the two buttons and none of the four
  actions; window modal at 560×650, toolbar above the cards, zero checkboxes in
  it; card reorder persists and does not search while the window is open; after
  closing window + panel the query re-issued exactly once with the new order;
  scroll offset restored on reopen; Rescan redraws the cards; the bulk FTS box
  opens inside the window (list 540px → 410px); empty-group message correct;
  Remove…/About/group editor intact; 380×700 and 320×640 fit. `go build ./...`
  passes. No APK, nothing on a phone, dark and paper themes unchecked.
- **This machine's in-app browser does not fire `<dialog>`'s `close` event at
  all** (Electron 41 / Chrome 146: a bare probe dialog closed with `close()` is
  silent, trusted or not). The focus return and the re-issue that hang off that
  event therefore cannot be exercised there — verified instead with a synthetic
  `dispatchEvent(new Event("close"))` and end-to-end through `hidePanel`'s own
  closer. Real Chromium and the Android WebView fire it; the group editor has
  relied on the same event all along.

## GitHub-facing identity (2026-09-20, this session)

README.md reworked on `dev` (commit `80a8392`): H1 is now
"wuDict2 — an Android fork of WuWeiDict" with a what-differs block (app ID,
port 6889, Android UI work, license) at the very top; the mid-file
"wuDict2 for Android" section was folded into it. Repo settings changed via
the GitHub API (no `gh` CLI on this machine; token came from
`git credential fill`): default branch `master` → `dev` (so the landing page
shows the fork README), About description now names wuDict2, topics set
(android, dictionary, golang, mdx, stardict, slob, dsl, bgl, zim,
offline-dictionary). Homepage still points at the upstream docs site —
deliberately kept. `master` stays upstream-sync-only per AGENTS.md; it was
not touched.

Repository renamed `DmShAl/wudict` → `DmShAl/wudict2` on the user's request,
same session: same repo, all settings/issues/fork relation kept, GitHub
redirects the old slug permanently (until a new repo takes that name).
Local `origin` updated to the new URL. No CI, badge or script hardcodes the
old slug; the only in-repo mention (historical handoff note in
`docs/ANDROID-FORK.md`) is annotated. User preference, given twice
(release notes, then README): do NOT name the application ID
(`com.dmshepeta.wudict2`) in public-facing texts — "installs beside the
upstream wuDict app, default port 6889" is the approved wording.

## Release wudict2-v0.1.0 (2026-09-20, this session)

Tagged `wudict2-v0.1.0` (annotated, on dev `4b082e1`), pushed, then
`build-android.cmd release` rebuilt the APK so `git describe` supplied the
versionName (aapt2 confirms `versionName='wudict2-v0.1.0'`, versionCode 291,
package `com.dmshepeta.wudict2`; apksigner: V2, CN=Dmitry Shepeta). Release
created via API and the signed APK uploaded as
`wudict2-android-arm64-foss.apk` (7,241,264 bytes; download URL verified
200 with matching length):
https://github.com/DmShAl/wudict2/releases/tag/wudict2-v0.1.0
Fork-tag convention going forward: prefix release tags with `wudict2-…` so
upstream's `vX.Y.Z` tags never clash when syncing `master`.

Windows gotcha: in a background `cmd //c` from this shell,
`LOCALAPPDATA` may be undefined, so build-android.cmd's default SDK path
stays the literal `%LOCALAPPDATA%\Android\Sdk` and fails. Fix: pass
`set ANDROID_HOME=C:\Users\shepe\AppData\Local\Android\Sdk&&` in front of
the script call (no space before `&&`).

CHANGELOG.md added on `dev` (2026-09-20): one `wudict2-…` section per
release, newest first, relative to the upstream fork point (v0.1.0's
baseline: upstream `223b990`, between v3.7.4 and v3.7.5-alpha.1). Update
it with every future release; the release body mirrors the same text
under `## Changes`.

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

- Real dictionaries for manual checks: `test_data/` (three `.dsl.dz` — Asperger
  En-En 6.8k entries, Oxford En-Ru 35.8k, Zimmerman Ru-En 15.9k, ~5.4 MB, now
  git-ignored). Point a throwaway `DICT_DIR` at it to see the UI with real
  sizes, chips and index estimates instead of stub dictionaries.
- **Kill EVERY `wudict.exe` before starting a preview server** —
  `taskkill /F /IM wudict.exe`. A stale instance keeps port 6899 and the new one
  exits with "stop the running instance first", so the browser goes on being
  served by the OLD binary's embedded assets (the log looks fine: the config
  summary is printed before the bind). Cost an hour of chasing a CSS change that
  was never served; `tasklist //FI "IMAGENAME eq wudict.exe"` should show one
  process after a restart. The page also caches its CSS by the `?v=` hash, so
  the browser must be reloaded after the server is really new.
- Go: `go build ./...`; targeted `go test ./internal/<pkg> -run '...' -count=1`.
- Android Java: from `android/`,
  `ANDROID_HOME="$LOCALAPPDATA/Android/Sdk" ./gradlew.bat :app:compileFossDebugJavaWithJavac --offline`.
  No ANDROID_HOME in the bash environment by default.
- No `node`, no `gcc` in this shell: JS syntax checks by hand/`vm`, and
  `-race` cannot run (cgo). Do not burn time on either.
- `gofmt -l` flags nearly every tracked Go file — CRLF working-copy noise,
  not real. `git diff --check` is the meaningful check.
- **The Android emulator (AVD `Small`) is x86_64 and cannot run the arm64 Go
  binary** — measured 2026-09-24 on `sdk_gphone16k_x86_64`, Android 17/API 37,
  16 KiB pages. ARM translation IS present (`ro.dalvik.vm.native.bridge =
  libndk_translation.so`, and a trivial arm64 C binary execs and returns its
  exit code), but EVERY Go binary — down to a `CGO_ENABLED=0` hello-world —
  dies with SIGSEGV inside the translated code (tombstone:
  `ndk_translation_program_runner_binfmt_misc_arm64`, guest arch arm64, `pc`
  in the guest image). That is why the app's Java half starts and the exec'd
  server never does. Arm64-v8a system images are not the answer either: on an
  x86 host they run under full QEMU emulation, slower than the translation
  they would replace.
- **`build-android.cmd debug intel` builds for the emulator.** It cross-builds
  the server for `GOARCH=amd64` too and ships both ABIs in
  `wudict2-android-arm64-x86_64-foss-debug.apk`. The token may be written in
  either position (`intel debug` too, a bare `intel` means debug) and `release
  intel` is refused. The x86_64 lib lands in `android/app/src/emuX86/jniLibs/`
  — a default source dir for NO source set — and only `-PemuX86=1` (which
  build.gradle wires to the DEBUG source set alone) makes AGP read it, so a
  release APK is arm64 whatever is on disk. Verified 2026-09-24: both ABIs in
  the APK, the x86_64 one taken back OUT of the APK runs on the emulator and
  prints its version stamp, plain `debug`/`release` stay arm64-only, and both
  refusals fire.
- **The NDK recipe for a second ABI**: the Windows NDK has no Unix-style
  `x86_64-linux-android26-clang` wrapper, so name the target on `clang.exe`
  itself — `CC="$NDK_BIN/clang.exe --target=x86_64-linux-android26"`, `CXX`
  likewise, `CGO_ENABLED=1 GOOS=android GOARCH=amd64`, same `-tags sqlite_fts5
  -trimpath` and the same ldflags including
  `-extldflags=-Wl,-z,max-page-size=16384`.
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
- Merge workflow aid (NOT in the repo; excluded via `.git/info/exclude`):
  `merge-work/index.full.html` — the fork's index.html with all extracted
  assets (app.css, history, pick.js, group editor) inlined back, regenerated
  by `go run merge-work/inline.go`. On an upstream sync, diff the new
  upstream index.html against it: hunks outside fork-customized regions
  port mechanically, hunks inside them are manual ports (to index.html or
  to the external asset that feature now lives in). Procedure in
  `merge-work/README.txt`; regenerate before every merge — a stale full
  page hides exactly the changes being merged.

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

## Presets as toggleable layers (2026-09-20, evening)

Uncommitted, on top of the appearance rounds. First shipped BROKEN — the user
reported "articles gone, top-bar buttons dead" — then debugged live on a
throwaway local server (built the binary, ran it, drove the page in the
browser; the standing "don't build/run" rule was suspended for that
diagnosis). Two real bugs were found and fixed, and the whole feature is now
verified end-to-end in the browser against a temp config and a one-entry
.dsl:

- **TDZ crash killed the whole page.** `frameCSS()` reads
  `presetArticleCSS`, but the variable was declared in the styler let-chain
  ~2800 lines below the line where `applyTheme()` runs and calls
  `pushFrameCSS()` → `frameCSS()`. Every handler after that point never
  bound — hence no articles, dead buttons. Fix: `presetArticleCSS` is
  declared beside the article plumbing (`let userArticleCSS`), where the
  comment explains WHY the placement is load-bearing.
- **`ContainsAny(name, "..\\")` 404'd every preset file.** The dot in
  "file.css" is a member of that rune set, so
  `GET /assets/presets/<g>/<f>` 404'd everything while the injected links
  looked fine. Fix: two `strings.Contains` checks (".." and `\`) plus the
  existing `//` check. Verified: files 200, manifest.json and traversal 404.

Also fixed along the way: the two `s.pageFor("")` call sites in
assets_test.go (signature grew a second parameter); go vet passes for the
whole module. E2E verified: search renders articles; presets pane renders
17 rows in 13 groups with radio groups marked "pick one"; Background hidden
without a shell image; Sepia on → its link injected before the user's, its
article half composed under the user text, state written to
style/presets.json; True black switches Sepia off (radio); reload → server
injects the enabled preset's link and the stylesheet parses (`sheet !==
null`); Insert-as-text lands both halves with the view following; toggling
off cleans links and the state file.

The Examples menu is gone. Presets were text pasted into the user's two
stylesheets, and un-applying one meant hand-deleting lines out of two boxes.
Now each preset is a file pair under `internal/server/web/presets/<group>/`
and a LAYER the page attaches and detaches around the user's own CSS; the
editor's App/Article boxes hold only what the user owns.

- Layout on disk IS the conflict model (the user's design): each
  subdirectory is a group of presets that exclude each other —
  `background/` holds Background image, Sepia, True black, Warm dark
  (one or none), `fonts/` holds the font choices Serif, Condensed, Light
  (one or none — a face is picked, not stacked); every other preset got a
  directory of its own and behaves as a free toggle. `manifest.json` in the
  same folder carries order, titles, descriptions and each preset's
  app/article file names.
- Server (`internal/server/presets.go`): embedded registry parsed once;
  `GET /api/presets` (full list with inline contents + enabled set),
  `PUT /api/presets` ({id,on}; radio enforced per group), state in
  `<StyleDir>/presets.json` (temp+rename), and `GET /assets/presets/<g>/<f>`
  serving the app halves immutably (content-hashed URLs; `manifest.json`
  itself answers 404; backslash/.. rejected — the FS is forward-slash even
  on Windows). Routes registered in routes.go; `/api/presets` documented in
  openapi.yaml (Spec field set, so TestOpenAPICoversEveryRoute stays honest).
- Page injection: `pageFor(tag, presetLinks)` — the preset `<link
  data-preset>`s are spliced at `{{USERCSS}}` BEFORE the user's link, so the
  user wins every tie; the page cache key is both parts, and `?style=off`
  omits presets with everything else.
- Client: fourth tab "Presets" in the sheet (pane reuses the Files pane's
  look; switches are the panel's `.en`). `presetApplyAll()` syncs app links
  + `presetArticleCSS`, and `articleRefresh()` is now the one place the
  article sheet is built (presets under user text); `frameCSS()` composes
  the same way. `stylerInsertPreset` = the old apply-as-text (Files→Insert
  uses it too; handles both payload and legacy shapes), `presetRemovePasted`
  + exact-text detection migrate old pastes (commit BEFORE mutate — a
  textarea still holding old text would reinstall the block via
  stylerShowTab's commit). `?style=off` blocks the whole preset path.
- Startup: `presetsLoad()` is called fire-and-forget from group-editor.js's
  boot line — which is ALSO where the boot chain lives now; `loadUserCSS`
  had been left uncalled there since the "Search history" commit dropped the
  old `Promise.all` line (pre-existing bug: user article CSS never applied
  until the sheet was opened — fixed by wiring presetsLoad alongside it).
- Removed: STYLER_PRESETS, ART_THROUGH, stylerFillPresets, stylerApplyPreset,
  the `stylerPreset` select (and its dead CSS), the four server embeds and
  the `{{BACKGROUND_PRESET}}/{{SEPIA_PRESET}}` substitutions. Shell.java's
  picker list still names `stylerPreset` but null-guards it — no Java change.

Verified in the desktop browser as listed above; NOT verified on a phone.
On-device checks: preset toggling feels instant on a real dictionary set,
radio switching in background/, cold-start injection order with several
presets enabled, migration of old pastes, `?style=off` interplay, and the
fonts group (Condensed/Light) against the system font-size setting.

Two user-review changes on top (same evening, verified in the desktop
browser only): the sheet now OPENS on the Presets tab (stylerOpen ends with
stylerShowPresets, not stylerShowTab — the switches are the entry point, and
no textarea focus means no keyboard popping over the pane), and the
keyboard-fit handler `stylerFit` + `#styler{bottom:var(--styler-bottom,0)}`
exist because a soft keyboard shrinks the layout viewport, `vh` with it, and
the fixed-height sheet squeezed the editor to one line. First attempt
relied on visualViewport resize/scroll events — on the phone the keyboard
overlap happened anyway (the events do not reliably fire for a keyboard in
every WebView mode), so while the sheet is open the fit now runs on a 300ms
POLL (`stylerFitStart`/`stylerFitStop` around open/close): covered window =
innerHeight − vv.height − vv.offsetTop; when covered, `--styler-h` is ~62%
of the VISIBLE height (floored at 240px) and `--styler-bottom` lifts the
sheet above the keyboard; keyboard down → overrides clear. `#stylerCSS`
has `min-height:5em` as the belt — at the 240px floor the editor keeps
~94px. The sheet also wears the shell background like the other windows:
`html[data-shell-sepia/image] #styler` applies the paper tint + wallpaper
recipe from history.css; solid `--bg` is the no-shell answer.

Follow-up from the phone run: the panel DID lift, but the fixed rows above
the editor (head, Background block with the strip) had grown so much since
the pre-thumbnail sheet that the lifted height left the editor one line.
The first `.editing` trigger (covered window AND caret in the textarea)
never fired on the phone, and the reason matters: **on the phone the
fork's inset padding shrinks the WebView together with the keyboard, so
`window.innerHeight` and `visualViewport.height` stay in step and the
overlap difference is ALWAYS ~0 there.** "The panel rises" on the phone is
the fork's own inset mechanism, not the page. The trigger is now the CARET
IN THE TEXTAREA alone (`stylerFit` toggles `#styler.editing` from the poll
and from focus/blur on the box; `#styler.editing` stands the Background
block and the note down). Measured: the block is 147px on a desktop
viewport; collapsing it took the editor from 219px to 409px. The color
input lives INSIDE the hidden block, but the trigger being the textarea's
caret means typing a hex keeps the block — the caret is elsewhere. The
overlap-based LIFT stays in stylerFit for window modes where the keyboard
draws over the page instead of shrinking it.
