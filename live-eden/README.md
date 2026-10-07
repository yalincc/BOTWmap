# BOTWNavi-Eden（已废弃，并入 xnavi v3.0.0）

> ⚠️ **已废弃（2026-10-08）**：Eden 定位已并入 **xnavi v3.0.0**（Cemu / Ryujinx / Eden 三平台合一）。
> 本目录代码保留作历史参考（git 历史承载），**不再发布新版本**。
> 新用户请下载 xnavi v3.0.0+：https://github.com/yalincc/BOTWmap/releases

## 历史

- **BOTWNavi-Eden v1.0.0**（2026-10-07 发布）：BOTW × Eden 实时定位引擎 + Wails GUI。
- 定位方案：存档锚点窗口扫描 + 分组打分 + 半死槽快速换锁 + 绝对地址缓存。
- 并入 xnavi v3.0.0 后的演进（真机验证收敛）：
  - 窗口扫描升级为**全量结构扫描**（玩家走离存档点也能定位）；
  - 占位池三层过滤（alt≈0 / 整数三轴 / 三轴相等）；
  - 移动检测（两次采样）优先活动槽 + 锁定读二次采样（跑快不卡顿）；
  - known 绝对地址缓存秒锁 + 单实例锁防多开。

## 目录说明

- `engine/`：定位引擎源码（watch / known / locate，xnavi `platform_eden.go` 的移植源头）
- `probe/`：探针工具（金手指链验证 probe_botw.exe / xci_parse.py，阶段 0 证据）
- `gui/`：旧 Wails GUI 壳（已并入 xnavi-gui）

## 相关文档

- 归档：`归档/docs-过程文档-20261007/开发计划-xnavi三合一.md`（含阶段 0 金手指链验证结论）
