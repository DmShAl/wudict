# wuDict2 Android identity

wuDict2 is an Android fork of [WuWeiDict](https://github.com/wuweidict/wudict).
The upstream copyright notices and GPL-3.0-or-later license remain in place.
The Android brand and application ID are distinct. The Java package and Gradle
namespace stay `com.legbehindneck.wudict` to reduce merge conflicts with
upstream; a small activity in `com.dmshepeta.wudict2` provides a distinct lookup
component. The Go module path, server API, and internal `wudict` configuration
names remain compatible with upstream.

| Build | Application ID | Launcher name | Lookup activity |
| --- | --- | --- | --- |
| FOSS/Play release | `com.dmshepeta.wudict2` | wuDict2 | `com.dmshepeta.wudict2.LookupActivity` |
| FOSS/Play debug | `com.dmshepeta.wudict2.debug` | wuDict2 Debug | `com.dmshepeta.wudict2.LookupActivity` |

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
