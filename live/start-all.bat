@echo off
setlocal
set ROOT=E:\WorkSpace\BOTWmap
set PY=F:\Python\python.exe
set PYW=F:\Python\pythonw.exe
set LOG=%ROOT%\.workbuddy\bat.log
cd /d "%~dp0"

if not exist "%PYW%" set PYW=%PY%

echo ============================================
echo  BOTW live map  -  service + overlay
echo ============================================

netstat -ano | findstr ":8766" | findstr LISTENING >nul
if errorlevel 1 (
  echo [1/3] starting tracker service ...
  echo [%date% %time%] bat: starting service >> "%LOG%"
  start "botw-service" /min "%PY%" -u "%~dp0start.py" "%ROOT%" 8766 --no-open ^> "%ROOT%\.workbuddy\service.log" 2^>^&1
  timeout /t 5 /nobreak >nul
) else (
  echo [1/3] tracker service already running on port 8766
  echo [%date% %time%] bat: service already running >> "%LOG%"
)

rem --- read the save file for collection progress (shrines + towers + koroks) ---
echo [2/3] reading save file progress ...
echo [%date% %time%] bat: build_progress >> "%LOG%"
"%PY%" "%~dp0build_progress.py" > "%ROOT%\.workbuddy\progress_build.log" 2>&1

echo [3/3] starting transparent overlay ...
echo [%date% %time%] bat: starting overlay (%PYW%) >> "%LOG%"
start "" "%PYW%" "%~dp0overlay.py" "%ROOT%" 8766
timeout /t 3 /nobreak >nul

echo.
echo Done. Overlay: left-drag move / wheel zoom / right-click menu / Esc quit
echo Map page: http://127.0.0.1:8766/app/index.html
echo.
echo To refresh collection progress after saving in game, just rerun this file.
echo [%date% %time%] bat: done >> "%LOG%"
timeout /t 5 /nobreak >nul
endlocal
