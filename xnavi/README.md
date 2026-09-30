# xnavi · 双游戏定位导航（BOTW / TOTK）

只读内存定位导航程序：扫描 Cemu / Ryujinx 进程内存，解析玩家坐标，经本地 HTTP（默认 :8766 `/pos`）推送到网页互动地图实时跟随。**只读内存，不写入。**

- BOTW（旷野之息）：Cemu + Ryujinx 双平台
- TOTK（王国之泪）：Ryujinx（V2.2.0 并入，蓝本 `E:\WorkSpace\TOTKmap\live-go\`）
- GUI 壳（wails）`xnavi.exe` 启动时 exec 拉起核心 `xnavi-core.exe`

## 交接必读

**当前开发状态、遗留问题清单、架构说明见：**
`..\归档\docs-过程文档-20260929\Xnavi V2.2.0交接文档-开发进度与遗留问题.md`
（含构建/部署命令、坐标体系、偏移表、PickOffset 共识、Q1-Q6 遗留问题与接手顺序）

## 快速命令（PowerShell，详见交接文档第 3 节）

```powershell
# core 构建 + 单测
Set-Location E:\WorkSpace\BOTWmap\xnavi
& D:\tools\go\bin\go.exe build -ldflags "-s -w" -o xnavi-core-new.exe .
& D:\tools\go\bin\go.exe test ./...

# 部署（先杀进程再覆盖 build\bin\xnavi-core.exe）

# GUI 壳（勿加 -clean，会清空 build\bin）
$env:Path = 'D:\tools\go\bin;' + $env:Path
Set-Location ..\xnavi-gui; & 'D:\tools\go\gopath\bin\wails.exe' build -skipbindings
```

## 运行

`xnavi-gui\build\bin\xnavi.exe`（或命令行 `xnavi-core.exe [port] [--game=auto|botw|totk] [--no-open]`）
