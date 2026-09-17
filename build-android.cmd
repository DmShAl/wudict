@echo off
setlocal EnableExtensions EnableDelayedExpansion

rem ============================================================
rem wuDict Android ARM64 FOSS debug build for Windows
rem ============================================================

cd /d "%~dp0"

echo.
echo ============================================================
echo wuDict Android ARM64 build
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

if not exist "%JNI_DIR%" (
    mkdir "%JNI_DIR%"
)

if errorlevel 1 (
    echo ERROR: Could not create:
    echo   %JNI_DIR%
    exit /b 1
)

rem ------------------------------------------------------------
rem Go / Android cross compilation settings
rem ------------------------------------------------------------

set "GOOS=android"
set "GOARCH=arm64"
set "CGO_ENABLED=1"

rem Windows NDK does not provide the Unix-style wrapper executable
rem aarch64-linux-android26-clang as a native binary in the same way
rem Linux does, so call clang.exe directly and specify the target.

set "CC=%NDK_BIN%\clang.exe --target=aarch64-linux-android%ANDROID_API%"
set "CXX=%NDK_BIN%\clang++.exe --target=aarch64-linux-android%ANDROID_API%"

echo ============================================================
echo Building libwudict.so
echo ============================================================
echo.

echo GOOS=%GOOS%
echo GOARCH=%GOARCH%
echo CGO_ENABLED=%CGO_ENABLED%
echo CC=%CC%
echo.

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

go build ^
  -tags sqlite_fts5 ^
  -trimpath ^
  -ldflags "-s -w -X github.com/wuweidict/wudict/internal/cli.Version=%VERSION% -extldflags=-Wl,-z,max-page-size=16384" ^
  -o "%ANDROID_LIB%" ^
  .

if errorlevel 1 (
    echo.
    echo ============================================================
    echo ERROR: Go Android build failed.
    echo ============================================================
    exit /b 1
)

if not exist "%ANDROID_LIB%" (
    echo ERROR: Go build finished but libwudict.so is missing:
    echo   %ANDROID_LIB%
    exit /b 1
)

echo.
echo Native library created:
dir "%ANDROID_LIB%"
echo.

rem ------------------------------------------------------------
rem Build Android APK
rem ------------------------------------------------------------

echo ============================================================
echo Building FOSS debug APK
echo ============================================================
echo.

pushd android

call gradlew.bat assembleFossDebug

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

set "APK=android\app\build\outputs\apk\foss\debug\wudict-android-arm64-foss-debug.apk"

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
)

echo.
echo ============================================================
echo BUILD SUCCESSFUL
echo ============================================================
echo.

echo APK:
echo   %CD%\%APK%
echo.

echo Install:
echo   adb install -r "%APK%"
echo.

endlocal
exit /b 0