@echo off
set "PATH=D:\tools\go\bin;%PATH%"
go build -trimpath -ldflags "-s -w" -o xnavi-pc.exe .
if errorlevel 1 (
  echo BUILD FAILED
  pause
  exit /b 1
)
echo BUILD OK: xnavi-pc.exe
pause
