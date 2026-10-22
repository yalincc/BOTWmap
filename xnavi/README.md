# xnavi · BOTW 定位导航（纯 BOTW 版）

只读内存定位导航程序：扫描 Cemu / Ryujinx 进程内存，解析玩家坐标，经本地 HTTP（默认 :8766 `/pos`）推送到网页互动地图实时跟随。**只读内存，不写入。**

- BOTW（旷野之息）：Cemu + Ryujinx 双平台 —— 本程序只服务 BOTW
- TOTK（王国之泪）：**已剥离**（2026-10-02 架构决策，v2.3.0 落地），由
  `E:\WorkSpace\TOTKmap\live-go` 独立承载
- GUI 壳（wails）`xnavi.exe` 启动时 exec 拉起核心 `xnavi-core.exe`
  （TOTK 侧同一套壳在 `TOTKmap\live-gui\`，复用文件通信，未重编译）

## 文档指针（按需读，别猜）

| 要做什么 | 读 |
|---|---|
| 改定位逻辑 / 判断定位是否正常 | `docs/spec/LOCATE-RULES.md`（游戏行为 ↔ 定位逻辑对照 + 阈值表 + 日志词典） |
| 排查调试、查日志词典、发版回归清单 | `docs/spec/DEBUG-HANDBOOK.md` |
| 用户报问题的速查 | `docs/spec/TROUBLESHOOTING.md` |
| 当前架构与历次踩坑台账 | `docs/releases/xnavi-v3.2.0.md`、`docs/releases/xnavi-v3.2.1.md` |
| **发布包里该放什么 / 不该放什么** | `RELEASE/README.md` §「发布包目录规范」+ `docs/spec/DEV-HANDBOOK.md` §7.1 |
| 同步定位规则的"宪法" | `docs/spec/LOCATE-RULES.md`（改代码前先看 §禁区） |

> 历史交接（v2.2.x ~ v2.3.0，已归档，路径为历史快照）：
> `..\归档\开发计划\docs-过程文档-20261003\xnavi V2.3.0 TOTK剥离收尾与知识点总结-20261003.md`
> `..\归档\开发计划\docs-过程文档-20261002\Xnavi交接文档-20261002-BOTW与TOTK分离及TOTK定位稳定性.md`
> `..\归档\开发计划\docs-过程文档-20260929\Xnavi V2.2.1交接文档-指针扫描与TOTK定位稳定性.md`
> `..\归档\开发计划\docs-过程文档-20260929\Xnavi V2.2.0交接文档-开发进度与遗留问题.md`
> v3.1.0 调试上下文已于 2026-10-23 移至 `..\归档\弃用\xnavi-debug上下文-v3.1.0.md`（v3.2.0 架构已重写，看 DEBUG-HANDBOOK）。

## 快速命令（PowerShell）

```powershell
# core 构建 + 单测
Set-Location E:\WorkSpace\BOTWmap\xnavi
& D:\tools\go\bin\go.exe build -ldflags "-s -w" -o xnavi-core.exe .
& D:\tools\go\bin\go.exe test ./...

# 部署（先杀进程再覆盖 build\bin\xnavi-core.exe）

# GUI 壳（勿加 -clean，会清空 build\bin）
$env:Path = 'D:\tools\go\bin;' + $env:Path
Set-Location ..\xnavi-gui; & 'D:\tools\go\gopath\bin\wails.exe' build -skipbindings
```

> **换程序图标**：完整流程（icon.ico/appicon.png 生成 → 重建 → `tools\check-exe-icon.py` 校验 → release 同步 + 留底）见 `docs/spec/DEV-HANDBOOK.md §7.1`。

## 运行

`xnavi-gui\build\bin\xnavi.exe`（或命令行 `xnavi-core.exe [port] [--game=auto|botw] [--no-open]`）
