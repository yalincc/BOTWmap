# 灵墟地图追踪助手（BOTW Live · Go 版）

为《旷野之息》互动地图 [https://botw.yalin.site/](https://botw.yalin.site/) 提供**实时角色追踪**的本地小工具（单 exe，约 7MB，零依赖）。

## 这是什么

- 你在电脑上玩（Ryujinx 模拟器 + BOTW）时，本工具读取模拟器内存定位玩家坐标；
- 打开在线地图页，地图上就会出现你的**红点 + 移动轨迹 + 金色导航线**；
- 地图页在浏览器里正常打开即可，本工具只负责"追踪"这一件事，**不需要下载 42MB 地图瓦片**。

## 使用方法（三步）

1. 启动 Ryujinx 并进入游戏（进到可操作角色）；
2. 双击 `灵墟地图追踪助手.exe`；
3. 等窗口显示 `serving` 后，打开（或自动打开）https://botw.yalin.site/ —— 地图左上角状态胶囊从「离线」变为「◐ 定位中」再变为绿色坐标即已连接。

> 第一次运行会花几秒扫描内存定位（窗口里有日志），之后秒连（记忆了偏移）。

## 命令行参数

```
灵墟地图追踪助手.exe [端口] [--no-open] [--no-save]
```

| 参数 | 说明 |
|---|---|
| `端口` | 默认 8766（与网页约定一致，一般不用改） |
| `--no-open` | 启动后不自动打开浏览器 |
| `--no-save` | 跳过存档锚点，直接用"走动几步"方式定位（兜底） |

## 原理简述

- **定位**：读 Ryujinx 进程内存。先用存档（`%APPDATA%\Ryujinx\bis\user\save`）里的玩家最后位置做锚点，在 guest RAM 里窗口扫描，再用"旋转矩阵结构特征 + 数值变动"确认坐标槽；
- **快路径**：坐标槽相对 guest RAM 块的偏移记忆在 `known_addrs.json`，重启游戏后秒连；
- **看门狗**：模拟器重启 / 地址失效时自动重新定位，无需重启本工具；
- **网页**：http://127.0.0.1:8766 提供 `/pos` `/target` `/config` API，带 CORS + Private Network Access 头，在线 HTTPS 页面可直接跨域连接。

## 兼容性

- 目标模拟器：**Ryujinx**（进程名前缀 `Ryujinx`）。其他模拟器（如 Cemu）未适配；
- 存档必须存在（用于锚点）；首次扫描约 2~8 秒，取决于机器；
- 若安全软件拦截，请将本 exe 加入白名单（读取其他进程内存属正常行为，Go 编译的程序体积小、报毒率低）。

## 开发者：重新构建

需要 Go 1.21+（Windows amd64）：

```bat
:: 一键构建（产出 灵墟地图追踪助手.exe）
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
  known.go           known_addrs.json 快路径
  watch.go           状态机（轮询/看门狗/校验）
  server.go          HTTP API（CORS/PNA）
  main_test.go       单元测试
  build.bat          构建脚本
  known_addrs.json   已记忆偏移（随包分发，首次也能快连）
```
