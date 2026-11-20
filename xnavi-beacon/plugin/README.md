# Xnavi 信标插件

适用于 Eden 模拟器上的《塞尔达传说 旷野之息》1.6.0（TitleID `01007EF00011E000`）。
插件以 exlaunch subsdk 形式注入游戏，周期性把玩家世界坐标写入一块内存信标，
Xnavi 宿主读取该信标即可实时定位，无需扫描模拟器内存。

## 构建依赖

- devkitA64（r28）+ libnx + switch-tools，即 devkitPro 工具链。
- exlaunch 框架（https://github.com/shadowninja108/exlaunch）。

## 构建步骤

在 devkitPro 的 MSYS2 环境中：

```sh
export DEVKITPRO=/opt/devkitpro
export PATH="$DEVKITPRO/devkitA64/bin:$DEVKITPRO/tools/bin:$PATH"
```

把本目录的 `main.cpp`、`beacon.h` 放入 exlaunch 工程的 `source/program/`，
`config.mk`、`config.json` 放到工程根目录（`PROGRAM_ID` 已设为 `01007ef00011e000`），
然后执行 `make`，产物在 `deploy/subsdk9` 与 `deploy/main.npdm`。

工程路径不要含空格或括号，否则 make/gcc 会出错。

## 部署

把 `deploy/subsdk9` 复制到模拟器的模组目录：

```
Eden/user/load/01007EF00011E000/<模组名>/exefs/subsdk9
```

`main.npdm` 默认不部署：若同时装有 BOTW Optimizer，沿用它的 npdm 即可共存；
单独使用本插件时再放入自己的 `main.npdm`。在 Eden 的附加内容里启用该模组，
重启游戏后自动加载。

## 实现说明

- **坐标来源**：玩家 MainBody 变换矩阵 `[[main+0x2CA1140]+0x60]+0x398`
  （12 个 float，行主序 3×4），取平移分量 `m[3]/m[7]/m[11]` 即世界坐标 x/y/z。
   Havok 物理坐标是相对当前区域的局部值，不能直接用作地图坐标。
- **信标布局**：见 `beacon.h`，64 字节，含魔数、序号、坐标、状态与 CRC32。
  序号每帧递增，宿主据此判断插件是否仍在运行。
- **线程**：`nn::os::CreateThread` 使用不带 ideal_core 的重载，优先级 16，
  栈 0x20000 字节并按页对齐。参数不当会触发系统断言导致游戏崩溃。
- **不要调用文件系统接口**：在插件里写 sdmc 文件会与游戏的文件系统服务争用，
  导致游戏卡在启动阶段。信标因此放在内存中，由宿主读取。

## 验证

- Eden 日志出现 `XNAVI_BEACON started main=... beacon=...` 表示插件已运行。
- Xnavi 选择"模组注入"定位方式后，状态显示 `beacon`，红点随玩家移动。