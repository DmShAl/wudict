# Android UI continuation

This document covers UI behavior. For the current wuDict2 Android identity,
server startup, build commands, and coexistence checks, see
[ANDROID-FORK.md](ANDROID-FORK.md).

Work directly in `D:\Projects\Android\wudict`, as the user requested. Recheck branch/status before editing; old stashes exist and must be inspected before use.

## Existing documentation

General architecture/conventions: `CLAUDE.md`, `docs/SPEC.md`. Android usage: `pages/docs/apps/android.md`. Storage distinctions: `docs/ANDROID-PLAY-STORAGE.md`; LAN behavior: `docs/ANDROID-LAN-SHARING.md`. This note covers local UI additions and Windows workflow, not the whole project.

## Build and checks

Use the current Windows commands in [ANDROID-FORK.md](ANDROID-FORK.md). The
script rebuilds the embedded Go server, including HTML/CSS; Gradle alone may
package an older `libwudict.so`.

- Prerequisites checked by the script: Go, Git, Java; SDK defaults to `%LOCALAPPDATA%\Android\Sdk`, NDK pinned in script to `30.0.16248370`. `adb` is optional for building.
- Optional ignored `build-android.local.bat` provides local environment/signing. Without a keystore, release is unsigned. Do not read or print that file or private signing settings.
- Gradle's FOSS and Play tasks remain available. For application IDs, components, and artifact names, use [ANDROID-FORK.md](ANDROID-FORK.md).
- Routine check: `git diff --check`; `go build ./...`. Existing project-wide checks are in Makefile (`make help`, `make check`, `make test`). Android Java check from `android/`: `.\gradlew.bat :app:compileFossDebugJavaWithJavac --offline` (passed for the current Java changes). This does **not** rebuild embedded Go/HTML or verify an APK on a device.
- Search UI checks on Windows: `node --check internal/server/web/history.js`; extract the inline `<script>` from `web/index.html` to a temporary `.js` file and run `node --check` on it; `go test ./internal/server -run 'TestScriptsAreContentAddressed|TestAssetCacheHeaders|TestIndexTracksTheUserStylesheet' -count=1`. The inline script check is syntax only. These commands and `go build ./...` passed at HEAD `bab59c8`.
- The memory-limit corpus sweep is excluded from Windows builds because `syscall.Rusage.Maxrss` is unavailable there. Targeted server tests now run with ordinary `go test ./internal/server -run 'Test.*Group' -count=1`. The full server suite still has unrelated Windows failures, including Android path alias expectations and temporary ZIP files held open during cleanup.
- Inline JavaScript syntax was checked with Node `vm.Script` after replacing the server's `{{BACKGROUND_PRESET}}` and `{{SEPIA_PRESET}}` placeholders with `{}`. This does not verify Android touch behavior, native dialog rendering, or streaming search on a phone. APKs have since been built, but this UI behavior has not been verified on a phone.

## Where to work

Java paths below are relative to `android/app/src/main/java/com/legbehindneck/wudict/`.

| Area | Entry points |
| --- | --- |
| Settings reached from launcher icon menu | `SettingsActivity.java`: lookup behavior, screen edges and bars, access and server controls; background controls now live in the web Appearance sheet |
| Persisted Android preferences | `ShellPrefs.java`: legacy keys `sepia`, `sepia_color`, `found_dictionaries`; retain keys for compatibility |
| Native backgrounds and built-in images | `WindowBackground.java`: `directory`, `images`, `bitmap`, `drawable`, `dialogDrawable`, `withMargins` |
| WebView settings and native select bridge | `Shell.java`: `applyBackground`, `wudict:appearance` prompt bridge, `DICTIONARY_PICKER_JS`, `windows().onJsPrompt` |
| Themed picker/list rendering | `DictionaryPicker.java`; shared dialog surface: `BackgroundDialogBuilder.java` |
| Main and floating lookup windows | `MainActivity.java`, `LookupActivity.java`; the fork's exported wrapper is `android/app/src/main/java/com/dmshepeta/wudict2/LookupActivity.java` |
| Search, panels, Appearance and style editor | `internal/server/web/index.html`: `wudictShellBackground`, `appearanceRequest`, `appearanceRender`, `articleTokens`, `wudictSetDictionaryMode`, `wudictFoundDictionaryPicker`, `doSearch`, `clearStatusForResults`, `presetsLoad`, `presetApplyAll`, `stylerInsertPreset`; page styles in `web/app.css`; preset files and manifest in `web/presets/` (server side: `internal/server/presets.go`) |
| Configuration page | `internal/server/web/setup.html` has its own background hook; shared base styling is `setup.css` |
| Article iframe bridge | `internal/server/web/frame.js`; shadow articles inherit CSS variables, iframe articles receive resolved tokens through `frameCSS`/`pushFrameCSS` in index.html |
| Embedded example CSS | `internal/server/web/presets/background/`: `background_image_app.css`, `background_image_article.css`, `sepia_app.css`, `sepia_article.css` |
| Packaging examples into page | `internal/server/server.go`: go:embed strings, JSON-encoded `BACKGROUND_PRESET` / `SEPIA_PRESET` substitutions in `basePage` |

### Dictionary groups: current work

| Concern | Entry points |
| --- | --- |
| Stored membership and independent order | `internal/server/prefs.go`: `DictPref.Groups`, `DictionaryGroup.Order` persistence in `state.json`, identity repair in `Prefs.heal`; `internal/server/groups.go`: `handleGroups`, `handleGroupMember`, `handleGroupOrder`; routes in `routes.go`, API description in `web/openapi.yaml`, coverage in `groups_test.go` |
| Search scope and native picker data | `internal/server/web/index.html`: `orderedGroupDicts`, `activeUserGroupIds`, `doSearch`, `livePickerRows`, `livePickerPayload`, `wudictPickerGroupChanged`, `wudictPickerDictionarySelected` |
| Group editor and touch reorder | `web/group-editor.css` and `web/group-editor.js`: `renderGroupRows`, `saveGroupOrder`, `groupDragTick`, pointer handlers on `#groupRows`; markup in `index.html`; `#groupSelect` uses the native select bridge |
| Android dialog bridge | `Shell.java`: `DICTIONARY_PICKER_JS`, `windows().onJsPrompt`; `DictionaryPicker.java`: `showLive`, `updateLive`, `Live` and the older blocking picker for other selects |

Membership and group order belong to Go/state.json. The automatic All Dictionaries group follows the global preference order; each user group stores its own order. The selected picker group is a device-local `wudict_picker_group` value, and only globally enabled members are searched. `GET /api/groups` returns visible members in group order; `PUT /api/groups/order` saves an order without changing membership or the global order. The editor's Show All for a user group shows members first in group order, then a divider and nonmembers in All Dictionaries order. Adding a member appends it to the group's end; removing one returns it to its global-order place. When Show All is off, drag handles reorder only that group. All Dictionaries instead shows drag handles for the global order, with Show All checked and disabled.

The dictionary picker at the word field has a group dropdown above the existing All/Found list. Changing group reruns the current word, resets a single-dictionary selection, and clears old results; an empty group never falls through to a whole-library search. The live Android picker confirms its opening `window.prompt` immediately, then receives short update prompts while results stream. This avoids holding JavaScript blocked for the life of the dialog. Main and floating lookup windows both use the `Shell` bridge. Keep these flows in sync when editing picker behavior.

### Search field, history, and article lookup

| Concern | Entry points |
| --- | --- |
| Search request and article rendering | `web/index.html`: `doSearch`, `fetchStream`, `renderSlot`, `searchFor`, form submit and `q` blur handlers; mode and dictionary selectors also rerun searches |
| History and suggestions | `web/history.js`: localStorage, deduplication, maximum history length, Clear state, dropdown rendering, suggestion debounce/abort; `web/history.css`: dropdown surface/position |
| Suggestion request setup | `web/index.html`: `wuSearchHistory.setSuggest` builds `/api/search` using the current starts/exact/contains mode and selected dictionary/group; full-text suggestions are excluded |
| Article word gestures | `web/index.html`: page-level `dblclick`; `web/frame.js`: iframe `dblclick` and touch handling; both lead to `searchFor` in the main page |
| Asset embedding | `internal/server/server.go` embeds split CSS/JS; `routes.go` serves `/assets/*` with content-addressed versions; `assets_test.go` covers references and cache headers |

Typing in `q` changes only the dropdown suggestions. Articles update when a suggestion is selected, Search/Enter is pressed, or the changed field loses focus; leaving an empty field clears old articles. `history.js` requests suggestions independently of `doSearch`, with a 300 ms delay, aborting stale requests. The dropdown combines saved history and streamed headwords without duplicates. The history limit defaults to 20 and is capped at 100; Clear is enabled only when saved history exists. The old DoubleTaptoTest control is hidden, while its diagnostic route remains available. The user explicitly confirmed that double-tap lookup works well; do not reopen that issue without a new symptom. The dropdown appearance was also confirmed before the last search-trigger change.

## Decisions to preserve

- Android fork package, label, lookup component, and server port are documented in [ANDROID-FORK.md](ANDROID-FORK.md). The separate exported lookup class matters to external readers that distinguish components by class name.
- Appearance uses the existing Android `sepia`, `sepia_color`, and `background_image` preferences through `wudict:appearance`; no duplicate page storage. **Background color** defaults to `#f4ecd8`, unchecked by default. Its field displays six hex digits without `#`; accepts either form and either case. Existing saved colors remain unchanged. CSS-facing `sepiaColorText` still includes `#`. Android Settings retains edges and system bars.
- Background Image uses the same files as the CSS Files panel: `AppDirs.home(context)/.wudict/style/assets`. None disables the image. Built-ins `paper_01.jpg` and `paper_02.jpg` live in `android/app/src/main/assets/backgrounds/` and are copied when absent; never overwrite same-name user files. Backgrounds stretch independently to window width and height, not aspect-ratio cover. Respect edge margins in the main window. In the Appearance sheet the picker is a thumbnail strip (`#appearanceStrip`, `appearanceRender`): a tap on a tile applies that image, the ＋ tile opens the system file chooser directly (`stylerUpload`, with `stylerPickForBackground` promoting the last uploaded image) and never switches the pane to Files; browser-only use hides the native background controls.
- `dialogDrawable` adds a light wash and rounded border over current background. Native Examples uses this same surface; width is 260dp capped at 90% screen, group headings 18sp bold, items indented 32dp. Dictionary lists retain their own width.
- Dictionary picker mode lives under Sort dictionaries in the web Dictionaries panel, not Android Settings. Labels are **All / Found**. Native preferences remain authoritative via the local-origin prompt bridge. Entering Found clears any single-dictionary search scope and searches the selected group's enabled dictionaries; Found items scroll to displayed result sections and have no radio circles.
- `--paper-bg` and `--paper-bk-image` are set together by the page background hook from Android Settings. Frame tokens propagate them into iframe articles. No fixed file URL belongs in Background CSS. Background example is offered only when a valid image is active (`data-shell-image`); hiding the menu item does not remove already inserted CSS.
- Sepia example is independent CSS with its original fixed palette (`#f4ecd8`, `#faf3e3`); do not inject Settings color into these two files. Other examples still include inline CSS in `STYLER_PRESETS`.
- Presets are toggleable layers, not pasted text (as of the presets rework). Each preset is a file pair under `internal/server/web/presets/<group>/`; a subdirectory is a CONFLICT GROUP — presets inside it exclude each other (background/ holds Background image, Sepia, True black, Warm dark), so enabling one switches the group-mates off; the server enforces this and remembers the enabled ids in `style/presets.json`. Enabled app halves are server-injected `<link data-preset>` layers that sit UNDER the user's app.css; the article halves are composed client-side under the user's article text (`presetArticleCSS` + `articleRefresh`). The editor's App/Article boxes hold ONLY the user's own CSS. Updating a bundled preset file now reaches the user on the next load, with no rewrite of user CSS. A preset can still be copied into the user's boxes ("Insert" in the Presets pane) for customisation; while both the switch and the copy exist, they apply twice and the copy wins. Legacy pastes are detected by exact text and removable from the pane.
- Active App/Article/Files/Presets tab uses `--styler-active-bg`, falling back to a mix of `--accent` and `--bg`, so App CSS controls its appearance.
- Search status is cleared synchronously before results appear, canceling delayed messages and hiding the row without collapse animation (`clearStatusForResults`). Do not restore the delayed upward shift.
- Setup page 📁 folder button: on click the page checks `window.wudictNativeShell` (set by `Shell.applyBackground`) and, when present, asks through `window.prompt('wudict:folder-picker', '')`; the shell's `onJsPrompt` opens the system folder dialog (`ACTION_OPEN_DOCUMENT_TREE`, `Shell.REQ_DIR`) and answers the same prompt from `onActivityResult` with the real path from `Shell.treePath` (externalstorage provider only: primary volume via `Environment`, other volume ids as `/storage/<volume>/...`, tested as a readable directory). The channel is chosen at click time, not when the row is built, because the flag's injection can still be in flight when `/api/config` fills the first row. No persistable URI grant is taken — only the path string is used. Desktop Chromium keeps its File System Access API branch; a browser with neither channel hides the button on first click. TestSetupFlow/TestSetupMultipleFolders fail identically on clean HEAD under Windows (known suite issue, unrelated).

## Unfinished work

- No specific code change is pending. The user paused this work to start another task. Continue from the user's next instruction; do not add speculative controls (visible move-to-top arrows were discussed but not chosen).
- Do not apply old stashes or an earlier dictionary-groups patch over the current implementation.

## Unverified assumptions

- **Setup folder picker (new):** Go build, targeted server tests, and `:app:compileFossDebugJavaWithJavac` pass, but nothing has run on a device. Verify that 📁 opens the system folder dialog in both main and floating windows' host activity, that a picked folder fills the row with a real path and the row then validates ("✓ N found"), that Cancel leaves the field untouched, and that a microSD volume still resolves to a readable path. In the Play flavour an unreadable location answers as a cancel (no path conversion possible) — confirm the row simply keeps its old value rather than appearing broken.
- **Latest search-trigger change:** `bab59c8` passed Go build, JavaScript syntax checks, and targeted server tests; an APK has since been built but this behavior has not been used on a phone. Check that typing changes only suggestions, that Search/Enter, a list choice, and blur each update articles once, and that blur after clearing removes old articles. In particular, verify touch ordering when tapping a suggestion while `q` has focus, and mode/dictionary changes while editing. The code assumes `pointerdown` on a suggestion suppresses the blur search until its click chooses the word.
- **Suggestion semantics:** the independent request uses `/api/search` with the current mode and dictionary/group, `hl=0`, and the same per-dictionary result limits as article search. Confirm headwords and ordering against real dictionaries, especially contains mode and empty groups; no device check has been done. Full-text intentionally has no live headword suggestions.
- **Android touch and layout:** an APK has been built but not installed or exercised on a phone for these checks. Verify drag by ≡ (including auto-scroll at list edges), stable scroll after moving membership between the two Show All sections, the fixed editor size, checkbox colors, native group dropdown background, and both main and floating picker windows. Source checks and Java compilation do not establish these behaviors.
- **Streaming picker:** verify that changing groups while Found is open updates the native list during a search, preserves All/Found mode, and handles empty groups without stale results or a whole-library query. Its prompt bridge is designed to unblock JavaScript immediately, but that timing has not been checked on device.
- **Cancel/reopen regression:** the shared `DICTIONARY_PICKER_JS` now handles `dict`, `mode`, `stylerPreset`, and `groupSelect`. Prior user reports described a picker reopening after Cancel and tapping elsewhere. Reproduce with a fresh APK before changing gesture logic; the root cause was not confirmed. The separate WebView inertia issue was confirmed fixed by the user and need not be reopened.
