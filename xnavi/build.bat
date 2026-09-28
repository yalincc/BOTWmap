@echo off
REM xnavi 构建脚本（Go）
REM 用法: build.bat            -> 编译 xnavi.exe
REM       build.bat test       -> 跑单元测试

if "%1"=="test" (
    go vet .
    go test .
    exit /b %errorlevel%
)

go build -trimpath -o xnavi.exe .
if %errorlevel%==0 (
    echo.
    echo build ok: xnavi.exe
)
