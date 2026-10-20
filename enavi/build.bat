@echo off
setlocal
cd /d "%~dp0"

echo [enavi] building probe...
go build -trimpath -ldflags "-s -w" -o enavi-probe.exe .\cmd\enavi-probe
if errorlevel 1 goto fail

echo [enavi] building service...
go build -trimpath -ldflags "-s -w" -o enavi.exe .\cmd\enavi
if errorlevel 1 goto fail

echo [enavi] OK: enavi.exe  enavi-probe.exe
goto end

:fail
echo [enavi] BUILD FAILED
exit /b 1

:end
endlocal
