# live/ — BOTW 角色追踪（Python 旧版，已废弃）

> **状态：已废弃，仅作参考。** 已被 `live-go/`（Go 版 **BotwNavi**，单 exe 零依赖）替代，请使用 Go 版。
> 本目录保留 Python 版源码与算法，作为协议/定位逻辑的参考实现，不再维护。

## 这是什么

BOTW 实时角色追踪的最早实现（Python 版）：
- `start.py` — HTTP 服务（127.0.0.1:8766，/pos /target /config /progress 等）
- `locate.py` — 读 Ryujinx 进程内存定位玩家坐标
- `progress.py` — 解析 BOTW 存档（game_data.sav）flag 表，选游戏时间最长的槽
- `overlay.py` — 旧版透明浮窗（Tkinter + Pillow）
- `build_progress.py` — 合并存档 + 收集数据 → `data/progress_points.json`
- `build_save_flags.py` — 生成网页存档解析查找表 `app/data/save_flags.json`
- `start-all.bat` — 一键启动（服务 + 进度重建 + 浮窗）

## 为什么不用它

| 问题 | 说明 |
|---|---|
| 分发体积 | 打包成 exe 约 **20MB**（需打入 Python 解释器 + numpy + Pillow） |
| 依赖环境 | 用户机器需装 Python 3 + numpy + Pillow，`start-all.bat` 里写死了 `F:\Python\python.exe` |
| 维护 | 已被 Go 版替代，不再修 bug |

Go 版（`live-go/BotwNavi.exe`）单文件 7MB、零依赖、任意目录双击即用，功能相同且更稳定。

## 运行本版（仅限自用参考）

```bat
:: 前提：本机装有 Python 3 + numpy + Pillow，且 start-all.bat 中的 PY 路径正确
start-all.bat
```

或手动：
```bat
python start.py <项目根> 8766 --no-open
python build_progress.py
pythonw overlay.py <项目根> 8766
```

## 数据依赖

`build_progress.py` 重建进度需要 `_research/` 下 5 个数据集（随 git 分发，约 2.1MB）：
`koroks.json` / `shrines.json` / `dlc_shrines.json` / `towers.json` / `zelda-botw.hashes.csv`

其余调研素材（数据集源码、调试脚本、截图日志，约 228MB）已移入 `../归档/live-调研素材-20260916/`，见 `../归档/README.md`。
