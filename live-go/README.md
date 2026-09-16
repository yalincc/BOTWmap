# BotwNavi（BOTW Live · Go 版）

为《旷野之息》互动地图 [https://botw.yalin.site/](https://botw.yalin.site/) 提供**实时角色追踪**的本地小工具（单 exe，约 7MB，零依赖）。

## 这是什么

- 你在电脑上用 **Ryujinx 模拟器**玩 BOTW 时，本工具读取模拟器内存定位玩家坐标；
- 打开在线地图页，地图上就会出现你的**红点 + 移动轨迹 + 金色导航线**；
- 地图页在浏览器里正常打开即可，本工具只负责"追踪"这一件事，**不需要下载 42MB 地图瓦片**。

## 下载与安装

- 从 GitHub Releases 下载 `BotwNavi-分享版.zip`，解压后得到：
  ```
  BotwNavi.exe
  data/
    progress_points.json   ← 收集进度参考表（必需，与 exe 保持同目录结构）
  README.md
  ```
- **放任意目录**都能运行（程序按 exe 所在位置找文件，不写死路径）；
- 运行只需 `BotwNavi.exe` + `data/` 文件夹，**无需安装任何东西**（不需要 Python / Go / 运行库）。

## 使用方法（三步）

1. 启动 Ryujinx 并进入游戏（进到可操作角色）；
2. 双击 `BotwNavi.exe`；
3. 等窗口显示 `serving` 后，打开（或自动打开）https://botw.yalin.site/ —— 地图左上角状态胶囊从「离线」变为「◐ 定位中」再变为绿色坐标即已连接。

> 第一次运行会花几秒扫描内存定位（窗口里有日志），之后秒连（记忆了偏移）。

## 收集进度自动同步

除了角色位置，本工具还会**自动同步存档收集进度**（神庙 / 希卡塔 / 克洛格 / 回忆 / 神兽 已收集项在地图上自动变灰、统计面板实时计数）：

- 每 2 秒检查 Ryujinx 存档目录，**游戏内保存后约 1~2 秒**地图自动更新，无需重启、无需重读档；
- 注意游戏机制：BOTW 拿到果实 / 神庙后**不会立即写存档文件**，需要游戏内触发一次保存（菜单存档、传送、睡觉、自动存档等），保存后即自动同步；
- 原理：服务端解析存档 flag 表（对照 `data/progress_points.json` 参考表，1068 条），通过 `/progress` 接口实时输出；前端检测到进度代次变化立即刷新；
- 若缺少 `data/progress_points.json`，角色追踪仍可用，但收集进度同步会停用（日志会提示）。

## 兼容性（重要）

**仅支持 Ryujinx 模拟器**（进程名 `Ryujinx*`，存档目录 `%APPDATA%\Ryujinx\bis\user\save`）。

| 场景 | 是否可用 |
|---|---|
| Ryujinx 1.3.x（实测 1.3.351） | ✅ 可用 |
| yuzu / Cemu 等其他模拟器 | ❌ **不支持**（找不到 Ryujinx 进程，日志显示 "Ryujinx not running yet"，需 Ryujinx） |
| BOTW 1.6.0（实测版本） | ✅ 可用 |
| BOTW 其他版本 / 其他语言 | ✅ 大概率可用（定位逻辑自适应，不依赖固定版本号；未逐一实测） |
| 不同存档槽位 | ✅ 自动选择游戏时间最长的存档 |

> 定位原理：先读存档锚点，再全内存特征扫描（首次约 2~8 秒），成功后记忆偏移（`known_addrs.json`，重启秒连）。不依赖模拟器/游戏版本写死的偏移，因此版本变化一般不影响。

## 命令行参数

```
BotwNavi.exe [端口] [--no-open] [--no-save]
```

| 参数 | 说明 |
|---|---|
| `端口` | 默认 8766（与网页约定一致，一般不用改） |
| `--no-open` | 启动后不自动打开浏览器 |
| `--no-save` | 跳过存档锚点，直接用"走动几步"方式定位（兜底） |

## 常见问题（FAQ）

- **双击没反应 / 被拦截**：读取其他进程内存属正常行为，请将本 exe 加入杀软白名单（Go 编译程序体积小、报毒率低）。
- **显示 "Ryujinx not running yet"**：确认你用的是 Ryujinx 并已进入游戏（模拟器主窗口开着即可），程序会在模拟器出现后自动连接。
- **一直"定位中"**：控制台看日志；若首次扫描失败，在游戏里走几步再等几秒；确认游戏可操作（不是加载画面）。
- **进度不变**：BOTW 需要游戏内**主动保存一次**（菜单存档/传送/睡觉/自动存档）才写存档文件，之后 1~2 秒自动同步。
- **网页显示离线**：确认 `BotwNavi.exe` 还在运行；浏览器首次访问在线站连接本机服务时会询问权限（Private Network Access），允许即可。

## 原理简述

- **定位**：读 Ryujinx 进程内存。先用存档（`%APPDATA%\Ryujinx\bis\user\save`）里的玩家最后位置做锚点，在 guest RAM 里窗口扫描，再用"旋转矩阵结构特征 + 数值变动"确认坐标槽；
- **快路径**：坐标槽相对 guest RAM 块的偏移记忆在 `known_addrs.json`（exe 旁自动生成），重启游戏后秒连；
- **看门狗**：模拟器重启 / 地址失效时自动重新定位，无需重启本工具；
- **网页**：http://127.0.0.1:8766 提供 `/pos` `/target` `/config` API，带 CORS + Private Network Access 头，在线 HTTPS 页面可直接跨域连接。

## 开发者：重新构建

需要 Go 1.21+（Windows amd64）：

```bat
:: 一键构建（产出 BotwNavi.exe）
build.bat
```

```bash
go test ./...      # 单元测试（含真实存档解析对照）
go vet ./...
```

## 目录

```
live-go/
  main.go            入口（启动流程、参数）
  winapi.go          Windows API（进程/内存）
  locate.go          内存扫描定位
  savefile.go        存档解析（锚点）
  progress.go        收集进度同步（存档监听 + /progress）
  known.go           known_addrs.json 快路径
  watch.go           状态机（轮询/看门狗/校验）
  server.go          HTTP API（CORS/PNA + /progress）
  main_test.go       单元测试
  build.bat          构建脚本
  BotwNavi.exe       本地构建产物（不入库，build.bat 生成）
  known_addrs.json   运行时生成的偏移记忆（不入库）
  data/progress_points.json  收集进度参考表（入库，分发必需）
```

## 分发 Release

打 zip 时包含：`BotwNavi.exe` + `data/progress_points.json` + 本 README。压缩后约 2.8MB。上传到 GitHub Releases 即可，用户解压后任意目录双击运行。
