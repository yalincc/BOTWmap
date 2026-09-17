# 更新日志

本项目遵循语义化版本（SemVer）：`主版本.次版本.修订号`。修订号（x.y.**z**）即 bugfix 版本。

## v1.0.1（2026-09-18）

Bugfix 版本。修复回忆收集统计 bug，并修复随之暴露的位置追踪死槽/坏槽问题。

### 修复

- **回忆收集统计 bug**：23 个回忆改为逐点权威判定（`IsPlayed_Demo126~138` 影片已播放 flag），照片记忆 3/12、最终 0/1、主线自动 5/5、DLC EX 5/5 分层统计；
  - 修复英杰回忆与神兽共用 `Clear_Remains*` hash 导致的后写覆盖（同 hash 多标记合并为多值）；
  - `PictureMemory_Spot_Int`（s32 倒计时）正确解读为「还剩 9 处未造访」；
  - EX 标记改用 `tier='dlc'` + `ex_no` 独立标识，不再占用照片编号空间（收集模式按 `extra.tier` 判定）。
- **位置追踪死槽/坏槽自动恢复**：BotwNavi 重启后快速路径（known_addrs 偏移）可能锁到陈旧坐标槽——地址可读但永不更新（死槽）或跳到无关数据（坏槽），导致位置冻结、导航/收集/到达判定全部按错误坐标失效。
  - verifyKnown 改为「平滑移动（单次位移 0.2<Δ<50 且连续 3 次采样）才确认 verified」；
  - 未确认且 20 秒内无平滑移动 → 自动按存档锚点重新扫描定位，失败 2 分钟后重试；
  - 修复 lockAddr() 二次调用竞态。

### 变更文件（13）

`tools/build_data.py`、`app/js/data.js`、`live/build_progress.py`、`data/progress_points.json`、`live/build_save_flags.py`、`app/data/save_flags.json`、`app/js/ui.js`、`app/js/live.js`、`app/js/save_upload.js`、`app/css/style.css`、`live-go/progress.go`、`live-go/watch.go`、`live-go/main.go`

## v1.0.0（2026-09-16）

功能基线（此前未打版本标签）。

- 旷野之息互动地图：多图层同显、搜索、测量、标记、分享、全图、设置（响应式适配手机/PC）
- 角色追踪：本地 Go 工具 BotwNavi 读取 Ryujinx 内存实时定位（存档锚点扫描 + 偏移记忆秒连）
- 导航/收集模式：回忆/克洛格连续自动导航、到达提示、标记完成按钮统一、信息卡可拖动
- 收集进度：存档同步、进度统计面板、本地进度持久化
