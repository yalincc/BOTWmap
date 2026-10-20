@echo off
chcp 65001 >nul
cd /d "%~dp0"

if not exist build\enavi-probe.exe (
  echo [enavi] first run: building probe, please wait...
  go build -o build\enavi-probe.exe .\cmd\enavi-probe
  if errorlevel 1 (
    echo [enavi] BUILD FAILED - is Go installed?
    pause
    exit /b 1
  )
)

build\enavi-probe.exe -cmd m0

echo.
echo [enavi] done. Log file is in the report folder.
pause
