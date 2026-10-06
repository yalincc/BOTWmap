@echo off
rem Build botwnavi-eden.exe (Go 1.21+, Windows amd64)
setlocal
set GO=D:\tools\go\bin\go.exe
if not exist "%GO%" set GO=go
%GO% build -ldflags "-s -w" -o "botwnavi-eden.exe" .
if %errorlevel%==0 (
  echo build ok: botwnavi-eden.exe
) else (
  echo build failed
)
