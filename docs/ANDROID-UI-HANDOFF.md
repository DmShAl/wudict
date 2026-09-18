# Android UI continuation

Snapshot: 2026-09-18, branch `dev`, HEAD at preparation `5ed9eb8` (`CSS editor polishing`). Before this documentation edit, tracked files were clean; only an empty, untracked `AGENTS.md` existed. Recheck status rather than assuming this snapshot still holds.

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
- Routine check: `git diff --check`. Existing project-wide checks are in Makefile (`make help`, `make check`, `make test`). Targeted server tests: `go test ./internal/server -run 'Test.*(Asset|Frame)' -count=1` (PowerShell/Unix quoting shown).
- Earlier agent runs could not execute SDK tools and hit access denied for `%LOCALAPPDATA%\go-build`; the user builds successfully in external cmd. These are environment limitations, not demonstrated source failures. Check current permissions once; do not repeatedly retry the same blocked build or claim an APK/device test passed.
- Prior small JS checks used Node VM with DOM/prompt mocks. They verify event/state logic, not real Android touch/focus timing. No persistent regression suite was added for those mocks.

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
