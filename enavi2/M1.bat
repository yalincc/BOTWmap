@echo off
rem Enavi2 M1：特征码唯一性验证（实机）
rem 前置：Eden 已启动并进入游戏（能读到存档、人物能走动）
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo [M1] 未找到 go 工具链，请先安装 Go 或确认 PATH
  pause
  exit /b 1
)

echo [M1] 构建 enavi2-probe ...
go build -o build\enavi2-probe.exe .\cmd\enavi2-probe
if errorlevel 1 (
  echo [M1] 构建失败，看上面错误
  pause
  exit /b 1
)

echo [M1] 运行探针（NSO 来自 enavi 实测资产，只读不写）...
build\enavi2-probe.exe -cmd m1 -nso "..\enavi\report\nso\main_uncompressed.nso"

echo.
echo [M1] 完成。把上面输出整段发回给 AI 即可。
pause
