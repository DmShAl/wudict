# Android UI continuation

Snapshot: 2026-09-19, branch `dictionary-groups`, HEAD `9a6055e`. The group picker, group-specific order, and recent UI refinements are **uncommitted**; check `git status` before any change. `android/gradle.properties` also has a user change that was not made or edited by the agent. Older stashes exist, including one made to preserve `.gitignore` before switching branches; do not pop them blindly. The user requested direct work in this checkout, with no new worktree or patch-file transfer.

## Existing documentation

General architecture/conventions: `CLAUDE.md`, `docs/SPEC.md`. Android usage: `pages/docs/apps/android.md`. Storage distinctions: `docs/ANDROID-PLAY-STORAGE.md`; LAN behavior: `docs/ANDROID-LAN-SHARING.md`. This note covers local UI additions and Windows workflow, not the whole project.

## Build and checks

Run from repository root in Windows cmd:

```bat
build-android.cmd release
build-android.cmd release sh
build-android.cmd debug
build-android.cmd debug sh
```

`original` is the optional explicit second argument for the ordinary build. The script builds the embedded Go server and then the ARM64 FOSS APK. HTML/CSS embedded in Go require this full rebuild; Gradle alone may package an old `libwudict.so`.

- Prerequisites checked by the script: Go, Git, Java; SDK defaults to `%LOCALAPPDATA%\Android\Sdk`, NDK pinned in script to `30.0.16248370`. `adb` is optional for building.
- Optional ignored `build-android.local.bat` provides local environment/signing: `KEYSTORE`, `KEY_ALIAS`, `STORE_PASSWORD`, `KEY_PASSWORD`. Without a keystore, release is unsigned. Never print secrets.
- Gradle property: `-PappVariant=original` or `-PappVariant=sh`; tasks remain `assembleFossRelease` / `assembleFossDebug`.
- Ordinary outputs: `android/app/build/outputs/apk/foss/<type>/`; sh outputs: `android/app/build-sh/outputs/apk/foss/<type>/`. Names: `wudict-android-arm64-foss[_sh][-debug|-unsigned].apk`; signed release has neither debug nor unsigned suffix. The script prints the exact path.
- Routine check: `git diff --check`; `go build ./...`. Existing project-wide checks are in Makefile (`make help`, `make check`, `make test`). Android Java check from `android/`: `.\gradlew.bat :app:compileFossDebugJavaWithJavac --offline` (passed for the current Java changes). This does **not** rebuild embedded Go/HTML or verify an APK on a device.
- On Windows, `go test ./internal/server -run 'Test.*Group' -count=1` currently fails at compile time in the existing `memlimit_test.go` (`syscall.Rusage.Maxrss` is unavailable). To run the targeted group tests without that file, from the repository root in PowerShell: `$files = Get-ChildItem internal/server -Filter '*.go' | Where-Object Name -notin @('memlimit_test.go','foreground_other.go','hidewindow_other.go') | ForEach-Object FullName; go test $files -run 'Test.*Group' -count=1`. This and `go build ./...` passed at handoff.
- Inline JavaScript syntax was checked with Node `vm.Script` after replacing the server's `{{BACKGROUND_PRESET}}` and `{{SEPIA_PRESET}}` placeholders with `{}`. This does not verify Android touch behavior, native dialog rendering, or streaming search on a phone. No APK was built or installed by the agent.

## Where to work

Java paths below are relative to `android/app/src/main/java/com/legbehindneck/wudict/`.

| Area | Entry points |
| --- | --- |
| Settings reached from launcher icon menu | `SettingsActivity.java`: `sepiaRow`, `commitSepia`, background image chooser, button styling; labels in `android/app/src/main/res/values/strings.xml` |
| Persisted Android preferences | `ShellPrefs.java`: legacy keys `sepia`, `sepia_color`, `found_dictionaries`; retain keys for compatibility |
| Native backgrounds and built-in images | `WindowBackground.java`: `directory`, `images`, `bitmap`, `drawable`, `dialogDrawable`, `withMargins` |
| WebView settings and native select bridge | `Shell.java`: `applyBackground`, `DICTIONARY_PICKER_JS`, `windows().onJsPrompt` |
| Themed picker/list rendering | `DictionaryPicker.java`; shared dialog surface: `BackgroundDialogBuilder.java` |
| Main and floating lookup windows | `MainActivity.java`, `LookupActivity.java`; `LookupActivity_sh.java` is a thin subclass selected by build |
| Search, panels, style editor | `internal/server/web/index.html`: `wudictShellBackground`, `articleTokens`, `wudictSetDictionaryMode`, `wudictFoundDictionaryPicker`, `doSearch`, `clearStatusForResults`, `STYLER_PRESETS`, `stylerFillPresets`, `stylerApplyPreset` |
| Configuration page | `internal/server/web/setup.html` has its own background hook; shared base styling is `setup.css` |
| Article iframe bridge | `internal/server/web/frame.js`; shadow articles inherit CSS variables, iframe articles receive resolved tokens through `frameCSS`/`pushFrameCSS` in index.html |
| Embedded example CSS | `internal/server/web/presets/background/`: `background_image_app.css`, `background_image_article.css`, `sepia_app.css`, `sepia_article.css` |
| Packaging examples into page | `internal/server/server.go`: go:embed strings, JSON-encoded `BACKGROUND_PRESET` / `SEPIA_PRESET` substitutions in `basePage` |

### Dictionary groups: current work

| Concern | Entry points |
| --- | --- |
| Stored membership and independent order | `internal/server/prefs.go`: `DictPref.Groups`, `DictionaryGroup.Order` persistence in `state.json`, identity repair in `Prefs.heal`; `internal/server/groups.go`: `handleGroups`, `handleGroupMember`, `handleGroupOrder`; routes in `routes.go`, API description in `web/openapi.yaml`, coverage in `groups_test.go` |
| Search scope and native picker data | `internal/server/web/index.html`: `orderedGroupDicts`, `activeUserGroupIds`, `doSearch`, `livePickerRows`, `livePickerPayload`, `wudictPickerGroupChanged`, `wudictPickerDictionarySelected` |
| Group editor and touch reorder | `index.html`: `#groupEditor` CSS, `renderGroupRows`, `saveGroupOrder`, `groupDragTick`, pointer handlers on `#groupRows`; `#groupSelect` uses the native select bridge |
| Android dialog bridge | `Shell.java`: `DICTIONARY_PICKER_JS`, `windows().onJsPrompt`; `DictionaryPicker.java`: `showLive`, `updateLive`, `Live` and the older blocking picker for other selects |

Membership and group order belong to Go/state.json. The automatic All Dictionaries group follows the global preference order; each user group stores its own order. The selected picker group is a device-local `wudict_picker_group` value, and only globally enabled members are searched. `GET /api/groups` returns visible members in group order; `PUT /api/groups/order` saves an order without changing membership or the global order. The editor's Show All for a user group shows members first in group order, then a divider and nonmembers in All Dictionaries order. Adding a member appends it to the group's end; removing one returns it to its global-order place. When Show All is off, drag handles reorder only that group. All Dictionaries instead shows drag handles for the global order, with Show All checked and disabled.

The dictionary picker at the word field has a group dropdown above the existing All/Found list. Changing group reruns the current word, resets a single-dictionary selection, and clears old results; an empty group never falls through to a whole-library search. The live Android picker confirms its opening `window.prompt` immediately, then receives short update prompts while results stream. This avoids holding JavaScript blocked for the life of the dialog. Main and floating lookup windows both use the `Shell` bridge. Keep these flows in sync when editing picker behavior.

## Decisions to preserve

- Ordinary release package `com.legbehindneck.wudict`, label `wuDict`, lookup component `LookupActivity`. Parallel release package `.sh`, label `wuDict_SH`, component `LookupActivity_sh`. Debug packages end in `.debug` and `.debug_sh`. GoldenDict distinguishes the Activity class suffix, so changing only applicationId is insufficient. Each variant registers only its selected lookup component. Gradle manifest placeholders also select labels and Settings shortcut resources.
- Settings label is **Background color** (formerly Sepia). Default `#f4ecd8`, unchecked by default. Field displays six hex digits without `#`; accepts either form and either case. Existing saved colors remain unchanged. CSS-facing `sepiaColorText` still includes `#`. Field width is measured to leave room for its label.
- Background Image uses the same files as the CSS Files panel: `AppDirs.home(context)/.wudict/style/assets`. None disables the image. Built-ins `paper_01.jpg` and `paper_02.jpg` live in `android/app/src/main/assets/backgrounds/` and are copied when absent; never overwrite same-name user files. Backgrounds stretch independently to window width and height, not aspect-ratio cover. Respect edge margins in the main window.
- `dialogDrawable` adds a light wash and rounded border over current background. Native Examples uses this same surface; width is 260dp capped at 90% screen, group headings 18sp bold, items indented 32dp. Dictionary lists retain their own width.
- Dictionary picker mode lives under Sort dictionaries in the web Dictionaries panel, not Android Settings. Labels are **All / Found**. Native preferences remain authoritative via the local-origin prompt bridge. Entering Found clears old dictionary search scope and searches all; Found items scroll to displayed result sections and have no radio circles.
- `--paper-bg` and `--paper-bk-image` are set together by the page background hook from Android Settings. Frame tokens propagate them into iframe articles. No fixed file URL belongs in Background CSS. Background example is offered only when a valid image is active (`data-shell-image`); hiding the menu item does not remove already inserted CSS.
- Sepia example is independent CSS with its original fixed palette (`#f4ecd8`, `#faf3e3`); do not inject Settings color into these two files. Other examples still include inline CSS in `STYLER_PRESETS`.
- Background and Sepia examples append to both editors; identical full snippets are not duplicated. Saving is explicit. Updating bundled examples does NOT rewrite previously saved user CSS. Preserve user edits and explain when a saved snippet needs updating.
- Active App/Article/Files tab uses `--styler-active-bg`, falling back to a mix of `--accent` and `--bg`, so App CSS controls its appearance.
- Search status is cleared synchronously before results appear, canceling delayed messages and hiding the row without collapse animation (`clearStatusForResults`). Do not restore the delayed upward shift.

## Unfinished work and unverified assumptions

1. **Dialog reopening after Cancel remains unconfirmed on device.** User saw Examples reopen after tapping an article/editor, then reported the same for search mode and dictionary list. Current shared `DICTIONARY_PICKER_JS` handles `dict`, `mode`, `stylerPreset`: prevents native select opening on pointerdown, opens after pointerup via zero-delay timer, releases pointer capture, ignores drag/cancel and duplicate click, and supports keyboard activation. Mock checks passed; user has not confirmed that the APK containing this fix resolves all three. Next reproduction: open → Cancel → tap elsewhere; repeat for mode, All/Found dictionaries, Examples, including floating lookup. Verify APK freshness before proposing another event workaround. Do not assume the guessed stale-touch cause is proven.
2. **Inertia issue is closed by user confirmation.** Native picker lists had inertia while the WebView panel/articles did not; an earlier `overflow-x:hidden` → `clip` suggestion did not solve it. User later said it was fixed. Do not reopen that investigation or claim this CSS change caused the fix.
3. Recent visual changes (Examples width/headings/indentation, Settings color field sizing, active-tab palette) were inspected in code and checked for syntax/state behavior, not rendered on a connected phone by the agent. Keep this distinction in reports.
4. External generated/reference files are not runtime dependencies. Current CSS sources and JPEG assets are in the repository paths above; old `D:/Projects/Android/App.css`, `Article.css` and `outputs/settings-css` are not the maintained sources.
