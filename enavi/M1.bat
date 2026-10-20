@echo off
chcp 65001 >nul
cd /d "%~dp0"

rem Enavi daily launcher: start read-only locator, then open the map.
rem Pure ASCII on purpose: cmd parses .bat as GBK, UTF-8 Chinese breaks it.
rem
rem Locator lookup order (first hit wins):
rem   1. enavi.exe          (root, hand-copied build)
rem   2. build\enavi.exe    (build.bat output)
rem   3. compile on the fly (needs Go on PATH)

set "EXE="
if exist enavi.exe set "EXE=enavi.exe"
if not defined EXE if exist build\enavi.exe set "EXE=build\enavi.exe"

if not defined EXE (
  echo [enavi] no exe found, building...
  go build -o enavi.exe .\cmd\enavi
  if errorlevel 1 (
    echo [enavi] BUILD FAILED - is Go installed and on PATH?
    pause
    exit /b 1
  )
  set "EXE=enavi.exe"
)

echo [enavi] using %EXE%
echo [enavi] starting locator (read-only). Keep this window open.
start "Enavi" "%EXE%"

echo [enavi] waiting 3s for the service...
timeout /t 3 >nul

echo [enavi] opening the map...
start "" "https://botw.yalin.site/?follow=1&game=botw"

echo.
echo [enavi] Tips:
echo   - The red dot should follow Link on the map.
echo   - If the map says "locating", make Link walk a few steps.
echo   - Close the "Enavi" window to stop the service.
echo   - First launch after restarting Eden needs a re-scan (about 10s).
pause
