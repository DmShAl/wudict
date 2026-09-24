# wuDict2 Android identity

wuDict2 is an Android fork of [WuWeiDict](https://github.com/wuweidict/wudict).
The upstream copyright notices and GPL-3.0-or-later license remain in place.
The fork's repository on GitHub is [DmShAl/wudict2](https://github.com/DmShAl/wudict2)
(renamed from `DmShAl/wudict` on 2026-09-20; the old URL redirects).
The Android brand and application ID are distinct. The Java package and Gradle
namespace stay `com.legbehindneck.wudict` to reduce merge conflicts with
upstream; a small activity in `com.dmshepeta.wudict2` provides a distinct lookup
component. The Go module path, server API, and internal `wudict` configuration
names remain compatible with upstream.

| Build | Application ID | Launcher name | Lookup activity |
| --- | --- | --- | --- |
| FOSS/Play release | `com.dmshepeta.wudict2` | wuDict2 | `com.dmshepeta.wudict2.LookupActivity` |
| FOSS/Play debug | `com.dmshepeta.wudict2.debug` | wuDict2 Debug | `com.dmshepeta.wudict2.LookupActivity` |

The launcher icon is the fork's own too: upstream's mark with a "2" drawn in it,
so the two apps are told apart on a home screen. The phone's copy is
`android/app/src/main/res/drawable/ic_launcher_foreground.xml` (adaptive and
monochrome), and `tools/make-icons.sh` renders the Play listing's `icon.png` and
`featureGraphic.png` from that same mark with that same digit. The mark itself
stays upstream's `internal/server/web/favicon.svg`, untouched, and so do the
renditions that are only ever a few pixels tall — the tray PNGs, the served
favicon, the `.icns`/`.ico`: a digit at 16px is a smudge, and no desktop build
ships from this fork.

wuDict2 uses `127.0.0.1:6889` by default; upstream wuDict uses port 6888.
The server's `wudict` protocol identity stays upstream-compatible. The Android
shell checks the responding server's app-specific library directory before
adopting it, so another installation on a shared loopback port is not mistaken
for this app. An explicit port override in Android Settings still takes priority;
change an existing override of 6888 if both apps must run together. Changing
the port changes the WebView origin, so page preferences stored under an older
port may need to be set again. Android shell preferences and app-owned files
remain in the wuDict2 app's own storage.

From Windows cmd, `build-android.cmd release` makes the FOSS release APK and
`build-android.cmd debug` makes the FOSS debug APK. The second argument
(`original` or `sh`) is retired and produces an error. Gradle's FOSS and Play
tasks still work, including `assemblePlayRelease` and `bundlePlayRelease`.
The APKs are named `wudict2-android-arm64-<flavour>[-debug|-unsigned].apk`.
The Play bundle is `wudict2-play-release.aab`. Release builds without a
configured keystore produce an unsigned APK; do not treat that as installable.

`build-android.cmd debug intel` cross-compiles the server for x86_64 as well
and ships both ABIs in one debug APK
(`wudict2-android-arm64-x86_64-foss-debug.apk`), so it installs on an x86_64
emulator and on arm64 hardware alike. This is not a convenience: the emulator's
ARM translation runs ordinary arm64 binaries but crashes every Go binary, a
hello-world included, so without an x86_64 server there is no way to run the
app on one. The flag is debug-only and the x86_64 library lives in
`android/app/src/emuX86/jniLibs/`, a source directory no variant reads unless
the build passes `-PemuX86=1` — which only the debug source set does — so a
release APK stays arm64 and nothing else. Arm64-v8a emulator images are not an
alternative: on an x86 host they run under full emulation, slower than the
translation they would be replacing.

The old `_sh` application ID and this ID are separate Android apps. Android
does not move preferences, prepared dictionaries, or app-owned files between
them automatically. Do not remove the old installation before independently
copying any data you want to keep. External readers may need to select the new
`LookupActivity` component again. FOSS and Play releases share an application
ID, so they replace rather than coexist with one another when signed compatibly.

The package ID is intended for Google Play, but its availability and the app's
eligibility must be checked in Play Console. The Play flavour targets API 36,
uses SAF rather than all-files access, and has a bundle build task; these source
settings alone do not establish store approval. No signing key is created here.

Keep `master` synchronized only with upstream. Develop on `dev` and branch
from `dev`; review and merge upstream changes into `dev` when desired. Avoid
renaming the Go module or broad internal identifiers solely for Android branding,
so upstream changes stay easier to apply.

Handoff snapshot (2026-09-19): checkout `D:\Projects\Android\wudict` was
clean on `dev` at `b23318a` before this documentation update. Only `origin`
(`DmShAl/wudict`) was configured as a remote; local `master` exists for upstream
sync. Recheck status and remotes before any Git operation. Do not switch or
merge merely to resume this work.

## Where to continue

| Concern | Code |
| --- | --- |
| Android IDs, FOSS/Play flavors, debug suffix, artifact names | `android/app/build.gradle` |
| Exported launcher/lookup activities and shortcut resource | `android/app/src/main/AndroidManifest.xml`; `src/main/res/xml/shortcuts.xml` and `src/debug/res/xml/shortcuts.xml` |
| Distinct external-reader component | `android/app/src/main/java/com/dmshepeta/wudict2/LookupActivity.java`, extending the unchanged upstream-package implementation |
| Launcher icon, Play listing images | `android/app/src/main/res/drawable/ic_launcher_foreground.xml` (the icon the phone shows); `tools/make-icons.sh` renders both Play images from `internal/server/web/favicon.svg` with the same digit |
| Port and saved override | `android/app/src/main/java/com/legbehindneck/wudict/ShellPrefs.java`: `DEFAULT_PORT`, `SERVER_PORT`, `port(Context)`; the settings hint is in `src/main/res/values/strings.xml` |
| Child Go process, adoption, readiness | `android/app/src/main/java/com/legbehindneck/wudict/ServerProcess.java`: `run`, `adoptRunningServer`, `awaitPort`; `AppDirs.java` provides the app-specific library directory |
| Shared Go protocol | `internal/server/server.go` defines the `wudict` Server header; `internal/server/folders.go` provides `/api/config.libDir`; `internal/cli/running.go` probes an occupied port. No Go identity rename is needed for separate ports. |
| Build entry points | `build-android.cmd` (Windows, rebuilds Go and FOSS APK), `Makefile`, `.github/workflows/build-android.yml`, `fastlane/Appfile` |

The device's loopback interface is shared across apps. The old shell accepted
any listening socket as startup success and could adopt another wuDict server.
The current shell checks `/api/config.libDir` against its own `AppDirs.dbDir`
both when adopting a surviving child and while waiting for a new one. The
different default port is the primary coexistence fix; the directory check
prevents a wrong-server success when a user override creates a collision.

## Build and verification record

Run in Windows cmd from the repository root:

```bat
build-android.cmd release
build-android.cmd debug
```

The script checks Go, Git, Java, SDK, and NDK; `adb` is optional. It reads an
optional ignored `build-android.local.bat` for local settings. Do not read or
print signing secrets. Without a configured release keystore, the release APK
is named `-unsigned` and must be signed before installation. Direct Gradle
tasks do not rebuild the embedded Go binary. For Play, rebuild Go first, then
use `android\gradlew.bat :app:assemblePlayDebug` or
`android\gradlew.bat :app:bundlePlayRelease` as appropriate.

At the last check on 2026-09-19, `build-android.cmd release` rebuilt the Go
binary and FOSS release APK successfully; `assembleFossDebug` and
`assemblePlayDebug` also passed after the port change. The release APK's
signature verified locally. The APK is under
`android/app/build/outputs/apk/foss/release/`. A Play AAB exists under
`android/app/build/outputs/bundle/playRelease/`, but it predates the port fix
and needs rebuilding before use. `git diff --check` passed for the port edit.
For artifact identity, inspect the merged manifest or APK with SDK `aapt2`:
release package `com.dmshepeta.wudict2`, upstream-package `MainActivity`, and
fork-package exported `LookupActivity`; inspect the debug shortcut target
package separately. These source and artifact checks do not establish device
behavior.

## Unfinished work

- Install the current wuDict2 release on a device and test simultaneous wuDict
  and wuDict2 operation in both launch orders. Confirm each displays its own
  library and that closing one does not stop the other. No device test has been
  run for the port fix.
- Test external-reader selection of
  `com.dmshepeta.wudict2.LookupActivity`, including the case where both apps
  are installed. The component exists in the built manifest, but reader choice
  has not been exercised.
- Rebuild and inspect the Play release AAB after the port change if Play work
  resumes. Publication, package-name availability, and store eligibility
  require separate Play Console checks; nothing was uploaded.

## Unverified assumptions

- `/api/config.libDir` is expected to resolve to the same canonical path as
  `AppDirs.dbDir` for the app's own Go child. Source inspection supports this;
  adoption and startup timing have not been exercised on a device.
- An existing explicit `SERVER_PORT=6888` override in wuDict2 takes precedence
  over the new default and still collides with upstream until changed or
  cleared in Android Settings. No installed preference state was inspected.
- An older, reparented Go child could survive an app update on port 6888. The
  new app does not adopt it on port 6889; whether one exists on the user's
  device is unknown.
- Moving wuDict2's WebView from port 6888 to 6889 changes its origin. Page
  preferences tied to the old origin may need to be set again; Android shell
  preferences and app-owned files remain under the same application ID.
