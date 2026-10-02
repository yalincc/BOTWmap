# xnavi · 双游戏定位导航（BOTW / TOTK）

只读内存定位导航程序：扫描 Cemu / Ryujinx 进程内存，解析玩家坐标，经本地 HTTP（默认 :8766 `/pos`）推送到网页互动地图实时跟随。**只读内存，不写入。**

- BOTW（旷野之息）：Cemu + Ryujinx 双平台 —— **本仓库当前只负责 BOTW**
- TOTK（王国之泪）：Ryujinx —— **已剥离**（2026-10-02 决策），由
  `E:\WorkSpace\TOTKmap\live-go` 独立承载，不再走本仓库。
  xnavi 内残留的 TOTK 代码待清理，见交接文档遗留清单。
- GUI 壳（wails）`xnavi.exe` 启动时 exec 拉起核心 `xnavi-core.exe`
  （TOTK 侧同一套壳已在 `TOTKmap\live-gui\`，复用文件通信，未重编译）

## 交接必读

**当前开发状态、遗留问题清单、架构说明见（最新）：**
`..\归档\docs-过程文档-20261002\Xnavi交接文档-20261002-BOTW与TOTK分离及TOTK定位稳定性.md`
（BOTW/TOTK 分离决策、主循环 sleep 修复、TOTK 五处修复、指针链实证证伪、遗留清单、工作规则）

历史背景：
`..\归档\docs-过程文档-20260929\Xnavi V2.2.1交接文档-指针扫描与TOTK定位稳定性.md`
`..\归档\docs-过程文档-20260929\Xnavi V2.2.0交接文档-开发进度与遗留问题.md`

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
