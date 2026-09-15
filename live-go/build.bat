@echo off
rem 一键构建：产出 灵墟地图追踪助手.exe（需要 Go 1.21+，Windows amd64）
setlocal
set GO=D:\tools\go\bin\go.exe
if not exist "%GO%" set GO=go
%GO% build -ldflags "-s -w" -o "灵墟地图追踪助手.exe" .
if %errorlevel%==0 (
  echo build ok: 灵墟地图追踪助手.exe
) else (
  echo build failed
)
