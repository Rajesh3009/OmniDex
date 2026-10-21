@echo off
setlocal
cd /d "%~dp0"

set OUTPUT_DIR=dist
set LDFLAGS=-s -w
set CGO_ENABLED=0
set GO111MODULE=on

set TARGET=%1
if "%TARGET%"=="" set TARGET=current

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

echo ==========================================
echo   OmniDex Multi-Platform Build System
echo ==========================================
echo Output Directory : %OUTPUT_DIR%
echo Target Platform  : %TARGET%
echo.

if /I "%TARGET%"=="current" goto build_current
if /I "%TARGET%"=="windows" goto build_windows
if /I "%TARGET%"=="linux" goto build_linux
if /I "%TARGET%"=="mac" goto build_mac
if /I "%TARGET%"=="darwin" goto build_mac
if /I "%TARGET%"=="all" goto build_all

echo Unknown target: %TARGET%
echo Usage: build.bat [current^|windows^|linux^|mac^|all]
exit /b 1

:build_all
call :compile windows amd64 .exe
call :compile windows arm64 .exe
call :compile linux amd64 ""
call :compile linux arm64 ""
call :compile darwin amd64 ""
call :compile darwin arm64 ""
goto finish

:build_windows
call :compile windows amd64 .exe
call :compile windows arm64 .exe
goto finish

:build_linux
call :compile linux amd64 ""
call :compile linux arm64 ""
goto finish

:build_mac
call :compile darwin amd64 ""
call :compile darwin arm64 ""
goto finish

:build_current
set GOOS=windows
set GOARCH=amd64
if /I "%PROCESSOR_ARCHITECTURE%"=="ARM64" set GOARCH=arm64
call :compile %GOOS% %GOARCH% .exe
goto finish

:compile
set OS_NAME=%1
set ARCH_NAME=%2
set EXT=%~3
set OUT_FILE=%OUTPUT_DIR%\omnidex-%OS_NAME%-%ARCH_NAME%%EXT%
<nul set /p "=Building for %OS_NAME%/%ARCH_NAME%... "
set GOOS=%OS_NAME%
set GOARCH=%ARCH_NAME%
go build -ldflags="%LDFLAGS%" -o "%OUT_FILE%" .
if %ERRORLEVEL% equ 0 (
    echo SUCCESS -^> omnidex-%OS_NAME%-%ARCH_NAME%%EXT%
) else (
    echo FAILED
)
exit /b 0

:finish
if exist "%OUTPUT_DIR%\omnidex-windows-amd64.exe" (
    copy /Y "%OUTPUT_DIR%\omnidex-windows-amd64.exe" "%OUTPUT_DIR%\omnidex.exe" >nul
    copy /Y "%OUTPUT_DIR%\omnidex-windows-amd64.exe" "omnidex.exe" >nul
)
echo.
echo Build completed! Binaries saved in %OUTPUT_DIR%\
endlocal
