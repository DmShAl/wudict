@echo off
setlocal EnableExtensions DisableDelayedExpansion

rem ============================================================
rem wuDict2 Android FOSS build: [debug|release] [intel]
rem
rem   intel  also ship the x86_64 server, so the same APK runs on the
rem          x86_64 emulator as well as on arm64 hardware. Debug only:
rem          a release APK is arm64 and nothing else (D52).
rem ============================================================

cd /d "%~dp0"

rem Optional private settings; kept out of Git. Do not echo signing passwords.
if exist "%~dp0build-android.local.bat" (
    call "%~dp0build-android.local.bat"
    if errorlevel 1 (
        echo ERROR: Could not load build-android.local.bat.
        exit /b 1
    )
)

set "BUILD_TYPE=%~1"
set "ABI_TOKEN=%~2"

rem The abi token is accepted in either position: `debug intel` and
rem `intel debug` mean the same thing, and a bare `intel` means a debug build.
if /i "%BUILD_TYPE%"=="intel" (
    set "ABI_TOKEN=intel"
    set "BUILD_TYPE=%~2"
)

if not defined BUILD_TYPE set "BUILD_TYPE=debug"
if /i "%BUILD_TYPE%"=="debug" (
    set "BUILD_TYPE=debug"
    set "GRADLE_TASK=assembleFossDebug"
    set "APK_SUFFIX=-debug"
) else if /i "%BUILD_TYPE%"=="release" (
    set "BUILD_TYPE=release"
    set "GRADLE_TASK=assembleFossRelease"
    set "APK_SUFFIX=-unsigned"
    if defined KEYSTORE (
        set "APK_SUFFIX="
    ) else (
        echo No KEYSTORE configured: building an unsigned release APK.
    )
) else (
    echo ERROR: unknown build type "%BUILD_TYPE%".
    echo Usage: build-android.cmd [debug^|release] [intel]
    exit /b 1
)

if /i "%ABI_TOKEN%"=="x86_64" set "ABI_TOKEN=intel"

set "EMU_X86="
if defined ABI_TOKEN (
    if /i not "%ABI_TOKEN%"=="intel" (
        echo ERROR: unknown option "%ABI_TOKEN%".
        echo Usage: build-android.cmd [debug^|release] [intel]
        exit /b 1
    )
    if /i "%BUILD_TYPE%"=="release" (
        echo ERROR: intel is debug-only. The x86_64 emulator ABI must not reach a
        echo   release APK - build one with: build-android.cmd release
        exit /b 1
    )
    set "EMU_X86=1"
)

set "APK_ABI=arm64"
if defined EMU_X86 set "APK_ABI=arm64-x86_64"

echo.
echo ============================================================
echo wuDict2 Android %APK_ABI% build
echo ============================================================
echo.

rem ------------------------------------------------------------
rem Check required tools
rem ------------------------------------------------------------

where go >nul 2>&1
if errorlevel 1 (
    echo ERROR: go.exe was not found in PATH.
    exit /b 1
)

where git >nul 2>&1
if errorlevel 1 (
    echo ERROR: git.exe was not found in PATH.
    exit /b 1
)

where java >nul 2>&1
if errorlevel 1 (
    echo ERROR: java.exe was not found in PATH.
    exit /b 1
)

where adb >nul 2>&1
if errorlevel 1 (
    echo WARNING: adb.exe was not found in PATH.
    echo APK will still be built, but install command will not work.
    echo.
)

rem ------------------------------------------------------------
rem Android SDK
rem ------------------------------------------------------------

if not defined ANDROID_HOME (
    set "ANDROID_HOME=%LOCALAPPDATA%\Android\Sdk"
)

if not exist "%ANDROID_HOME%" (
    echo ERROR: Android SDK directory does not exist:
    echo   %ANDROID_HOME%
    exit /b 1
)

echo Android SDK:
echo   %ANDROID_HOME%
echo.

rem ------------------------------------------------------------
rem Android NDK
rem ------------------------------------------------------------

set "NDK=%ANDROID_HOME%\ndk\30.0.16248370"

if not exist "%NDK%" (
    echo ERROR: Android NDK directory does not exist:
    echo   %NDK%
    exit /b 1
)

echo Android NDK:
echo   %NDK%
echo.

rem ------------------------------------------------------------
rem NDK LLVM toolchain
rem ------------------------------------------------------------

set "ANDROID_API=26"
set "NDK_BIN=%NDK%\toolchains\llvm\prebuilt\windows-x86_64\bin"

if not exist "%NDK_BIN%\clang.exe" (
    echo ERROR: clang.exe was not found:
    echo   %NDK_BIN%\clang.exe
    exit /b 1
)

if not exist "%NDK_BIN%\clang++.exe" (
    echo ERROR: clang++.exe was not found:
    echo   %NDK_BIN%\clang++.exe
    exit /b 1
)

echo NDK toolchain:
echo   %NDK_BIN%
echo.

rem ------------------------------------------------------------
rem wuDict version
rem Same idea as Makefile:
rem   git describe --tags --always --dirty
rem ------------------------------------------------------------

set "VERSION="

for /f "delims=" %%V in ('git describe --tags --always --dirty 2^>nul') do (
    set "VERSION=%%V"
)

if not defined VERSION (
    set "VERSION=dev"
)

echo wuDict version:
echo   %VERSION%
echo.

rem ------------------------------------------------------------
rem Output path for native Android binary
rem ------------------------------------------------------------

set "JNI_DIR=android\app\src\main\jniLibs\arm64-v8a"
set "ANDROID_LIB=%JNI_DIR%\libwudict.so"

rem The emulator ABI goes to a source dir that is a default for NO source set.
rem build.gradle looks at it only under -PemuX86=1, so however long this file
rem sits here it can never reach a release APK.
set "EMU_JNI_DIR=android\app\src\emuX86\jniLibs\x86_64"
set "ANDROID_LIB_X86=%EMU_JNI_DIR%\libwudict.so"

if not exist "%JNI_DIR%" (
    mkdir "%JNI_DIR%"
)

if errorlevel 1 (
    echo ERROR: Could not create:
    echo   %JNI_DIR%
    exit /b 1
)

if defined EMU_X86 (
    if not exist "%EMU_JNI_DIR%" mkdir "%EMU_JNI_DIR%"
    if errorlevel 1 (
        echo ERROR: Could not create:
        echo   %EMU_JNI_DIR%
        exit /b 1
    )
)

rem ------------------------------------------------------------
rem Build native server
rem
rem Equivalent to Makefile android-go:
rem
rem GOFLAGS:
rem   -tags sqlite_fts5 -trimpath
rem
rem LDFLAGS:
rem   -s -w
rem   -X github.com/wuweidict/wudict/internal/cli.Version=...
rem   -extldflags '-Wl,-z,max-page-size=16384'
rem ------------------------------------------------------------

call :build_go arm64 aarch64 "%ANDROID_LIB%"
if errorlevel 1 exit /b 1

if defined EMU_X86 (
    call :build_go amd64 x86_64 "%ANDROID_LIB_X86%"
    if errorlevel 1 exit /b 1
)

rem ------------------------------------------------------------
rem Build Android APK
rem ------------------------------------------------------------

echo ============================================================
echo Building wuDict2 FOSS %BUILD_TYPE% APK
echo ============================================================
echo.

rem -PemuX86=1 is what makes build.gradle look at src/emuX86/jniLibs; without
rem it that directory is invisible to every variant.
set "GRADLE_EXTRA="
if defined EMU_X86 set "GRADLE_EXTRA=-PemuX86=1"

pushd android

call gradlew.bat %GRADLE_TASK% %GRADLE_EXTRA%

if errorlevel 1 (
    popd
    echo.
    echo ============================================================
    echo ERROR: Gradle build failed.
    echo ============================================================
    exit /b 1
)

popd

rem ------------------------------------------------------------
rem Expected APK
rem ------------------------------------------------------------

set "APK=android\app\build\outputs\apk\foss\%BUILD_TYPE%\wudict2-android-%APK_ABI%-foss%APK_SUFFIX%.apk"

if not exist "%APK%" (
    echo ERROR: Gradle finished but APK was not found:
    echo   %APK%
    exit /b 1
)

rem ------------------------------------------------------------
rem Verify libwudict.so inside APK
rem ------------------------------------------------------------

echo.
echo ============================================================
echo Verifying APK
echo ============================================================
echo.

where jar >nul 2>&1

if errorlevel 1 (
    echo WARNING: jar.exe was not found in PATH.
    echo APK content verification skipped.
) else (
    jar tf "%APK%" | findstr /x /c:"lib/arm64-v8a/libwudict.so" >nul

    if errorlevel 1 (
        echo ERROR: APK does not contain:
        echo   lib/arm64-v8a/libwudict.so
        exit /b 1
    )

    echo OK: APK contains lib/arm64-v8a/libwudict.so

    if defined EMU_X86 (
        jar tf "%APK%" | findstr /x /c:"lib/x86_64/libwudict.so" >nul

        if errorlevel 1 (
            echo ERROR: APK does not contain:
            echo   lib/x86_64/libwudict.so
            exit /b 1
        )

        echo OK: APK contains lib/x86_64/libwudict.so
    )
)

echo.
echo ============================================================
echo BUILD SUCCESSFUL
echo ============================================================
echo.

echo APK:
echo   %CD%\%APK%
echo.

if /i "%APK_SUFFIX%"=="-unsigned" (
    echo Sign this release APK with your release key before installing.
) else (
    echo Install:
    echo   adb install -r "%APK%"
)

if defined EMU_X86 (
    echo.
    echo This APK carries arm64-v8a AND x86_64, so it installs on the phone and on
    echo   the x86_64 emulator alike. It is a debug build, which is also what lets
    echo   chrome://inspect attach to its WebView.
)
echo.

endlocal
exit /b 0

rem ============================================================
rem :build_go ^<GOARCH^> ^<ndk-triple-prefix^> ^<output .so^>
rem
rem Windows NDK does not provide the Unix-style wrapper executables
rem aarch64-linux-android26-clang / x86_64-linux-android26-clang as native
rem binaries the way Linux does, so call clang.exe directly and name the target.
rem ============================================================

:build_go
set "GOOS=android"
set "GOARCH=%~1"
set "CGO_ENABLED=1"
set "CC=%NDK_BIN%\clang.exe --target=%~2-linux-android%ANDROID_API%"
set "CXX=%NDK_BIN%\clang++.exe --target=%~2-linux-android%ANDROID_API%"

echo ============================================================
echo Building libwudict.so for android/%~1
echo ============================================================
echo.

echo GOOS=%GOOS%
echo GOARCH=%GOARCH%
echo CGO_ENABLED=%CGO_ENABLED%
echo CC=%CC%
echo.

go build ^
  -tags sqlite_fts5 ^
  -trimpath ^
  -ldflags "-s -w -X github.com/wuweidict/wudict/internal/cli.Version=%VERSION% -extldflags=-Wl,-z,max-page-size=16384" ^
  -o "%~3" ^
  .

if errorlevel 1 (
    echo.
    echo ============================================================
    echo ERROR: Go Android build failed for %~1.
    echo ============================================================
    exit /b 1
)

if not exist "%~3" (
    echo ERROR: Go build finished but libwudict.so is missing:
    echo   %~3
    exit /b 1
)

echo.
echo Native library created:
dir "%~3"
echo.
exit /b 0
