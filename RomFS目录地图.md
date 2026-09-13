# 旷野之息 RomFS 目录地图

> 实测路径：`G:\YUZU\Switch Games\RomFS`｜版本 **1.6.0**（最终版，含全部 DLC）
> 权威参照：ZeldaMods wiki（BOTW 逆向工程官方文档）<https://zeldamods.org/wiki/Content>
> 文中"实测"= 本机扫描结果，"wiki"= 引自上方文档

---

## 0. 先记住这件事（最容易踩的坑）

**romfs 顶层只是一小部分，真正的游戏内容大多压在 `Pack/*.pack` 里。**

两者是**并列**关系，不是包含关系：

```
RomFS/
├── Pack/            ← 一堆 .pack（SARC 归档），塞了大部分内容
├── Map/ Model/ ...  ← 散装文件，没被打包进去的那部分
```

所以"我在顶层没找到 XX"通常不代表没有 —— **先去 `Pack/` 里翻**。

---

## 1. 速查：要找什么 → 去哪

| 想找 | 位置 | 格式 |
|---|---|---|
| **简体中文文本**（地名/道具/对话） | `Pack/Bootup_CNzh.pack` → 内部 `Message/*.msbt` | MSBT |
| 其他语言文本 | `Pack/Bootup_<语言>.pack` | MSBT |
| **大地图放置物体**（含坐标） | `Map/MainField/<格>/*.smubin` | smubin |
| 地图静态摆放 / LOD | `Map/MainField/<格>/*.sblwp` | sblwp |
| **大地图底图贴图** | `UI/MapTex/MainField/*.sbmaptex` | sbmaptex |
| 地形高度 / 材质 / 水面 | `Terrain/A/MainField/*.sstera` | sstera |
| 地图界面（打开地图的 UI） | `UI/MapOpen/*.sbmapopen` | sbmapopen |
| 模型 / 贴图 / 动画 | `Model/*.sbfres` | sbfres |
| 每种实体（怪、建筑、NPC）的定义 | `Actor/Pack/*.sbactorpack` | sbactorpack |
| 过场动画视频 | `Movie/*.webm` | webm |
| 音乐 / 音效 | `Sound/Resource/Stream/*.bfstm` | bfstm |
| 图鉴（怪物/动物图） | `UI/PictureBook/*.jpg` | jpg |
| 物品图标 | `UI/StockItem/*.sbitemico` | sbitemico |
| 字体 | `Font/*.sbfttf`、`Font_XX.sbfarc` | - |
| NPC 寻路网格 | `NavMesh/MainField/*.shknm2` | shknm2 |
| 远景模型剔除数据 | `Game/FarModelCullMgr/area/*.sfmc` | sfmc |
| **神庙 / 神兽场景** | `Pack/Dungeon000–119.pack`、`Pack/Remains*.pack` | pack |
| **大世界主体 / 主线剧情** | `Pack/TitleBG.pack`（147 MB，最大的包） | pack |
| 启动资源 / 系统数据 | `Pack/Bootup.pack`（31 MB） | pack |

---

## 2. romfs 顶层目录（实测 17 个）

| 目录 | 内容 | 实测规模 |
|---|---|---|
| `Actor/` | 实体定义（敌人/建筑/NPC）+ AI 配置 | 顶层 1 个 `ActorInfo.product.sbyml` + `Pack/` 7203 个包 |
| `Effect/` | 特效集合定义 | 866 个 `.sesetlist` |
| `Event/` | 事件（剧情触发器） | 997 个 `.sbeventpack` |
| `Font/` | 字体（含亚洲字库） | 19 个 |
| `Game/` | FarModelCullMgr、Stats | 各 320 个 |
| `Layout/` | UI 布局归档 | 2 个 `.sblarc` |
| `Map/` | **大地图分块数据** | `MainField/` 80 格 |
| `Model/` | 模型/贴图/动画 | 5876 个 `.sbfres` |
| `Movie/` | 预渲染过场视频 | 30 个 `.webm` |
| `NavMesh/` | NPC/AI 寻路 | 1280 个 |
| `Pack/` | **SARC 归档（重点）** | 153 个 `.pack` |
| `Physics/` | Havok 物理 | MainField 320 + TeraMesh 1280 |
| `Sound/` | 音频 | Resource 904（Stream 688） |
| `System/` | 版本/语言掩码/资源表/图形库 | 5 项 |
| `Terrain/` | 地形 | `A/MainField/` **8366 个** |
| `UI/` | 界面图片与地图图块 | MapTex 2344、StockItem 1559、PictureBook 408 |
| `Voice/` | 配音 | 10 个语言目录 |

> wiki 里还列了 `Local/`（Wii U 残留，Switch 版不存在）——本机确实没有，符合。

---

## 3. Pack 目录详解（153 个 .pack）

| 分组 | 数量 | 说明 |
|---|---|---|
| `Bootup.pack` | 1 | 31 MB。启动资源、系统配置 |
| `Bootup_<语言>.pack` | 14 | **文本在这一层**。每个 1.5–2 MB |
| `Bootup_Graphics.pack` | 1 | 20 MB 图形资源 |
| `TitleBG.pack` + `TitleBG_<语言>.pack` | 12 | TitleBG 本体 **147 MB**，大世界 + 剧情主包 |
| `Dungeon000–119.pack` | 120 | **120 个神庙**，一庙一包（编号对得上） |
| `RemainsElectric/Fire/Water/Wind.pack` | 4 | 四神兽内部 |
| `Title.pack` | 1 | 标题画面 |

**语言包对应关系**（14 种，实测齐全）：
`CNzh`(简中) `TWzh`(繁中) `JPja` `KRko` `USen` `USes` `USfr` `EUen` `EUes` `EUfr` `EUde` `EUit` `EUnl` `EUru`

> 注：`TitleBG` 只有 11 种语言，**没有中文/韩文** —— 文本集中在 Bootup 系列，别去 TitleBG 里找中文。

---

## 4. 地图分块规则

`Map/MainField/` 下是 **10 × 8 = 80 个格子**，命名 `A-1` … `J-8`：

```
A-1 A-2 ... A-8
B-1 ...
...
J-1 ... J-8          + _DistanceView/（远景）
```

每格 5 个文件，实测扩展名只有两种：

| 扩展名 | 数量 | 含义 |
|---|---|---|
| `.sblwp` | 321 | 静态物件放置（含 Clustering 分簇 LOD） |
| `.smubin` | 82 | 动态物件放置（x-1 这种命名，非每格都有） |

同一套格子编号在多个目录复用，找同一块地方时可以对号入座：
`Terrain/A/MainField/<块号>.hght.sstera`、`Game/Stats/archive/A-1.00.sstats`、`UI/MapTex/MainField/MapTex1_A-0.sbmaptex`

---

## 5. 扩展名速查

| 后缀 | 含义 |
|---|---|
| `.pack` / `.ssarc` / `.sarc` | SARC 归档容器 |
| `.smubin` | 地图放置数据（mutable binary） |
| `.sblwp` | 地图静态/LOD 放置 |
| `.sbmaptex` | 地图贴图 |
| `.sbmapopen` | 地图界面 |
| `.sstera` | 地形（hght=高度 / mate=材质 / grass / water） |
| `.sbfres` | 模型·贴图·动画（FRES） |
| `.sbactorpack` | 单个实体的定义包 |
| `.sbeventpack` | 事件/剧情 |
| `.shknm2` | 导航网格（Havok） |
| `.bfstm` | 音频流 |
| `.bfsar` / `.bfgrp` | 音频归档组 |
| `.msbt` | **消息表（文本）** |
| `.sbyml` | 二进制 YAML（配置） |
| `.sbfttf` / `.sbfotf` | 字体 |
| `.sblarc` | UI 布局归档 |
| `.sesetlist` | 特效集合 |
| `.sbitemico` | 物品图标 |
| `.sfmc` | 远景剔除 |
| `.webm` | 视频 |

---

## 6. 顺手的两个信息源

- `System/Version.txt` → `1.6.0`
- `System/RegionLangMask.txt` → 声明本机支持的语言列表（见第 3 节）

---

## 7. 解包怎么搞

`.pack` / `.sarc` / `.msbt` 的解包流程已沉淀成 skill：`skill-switch-unpack`
（内含 CLI：递归解 SARC/Yaz0/zstd，遇到 `.msbt` 自动导 `label → 文本` 的 TSV）。

现成工具（hactool / NSCB / SAK / sarc_tool）能解到 romfs 或解一层 SARC，**但读不了 msbt**。

本项目实际用到并保留的解包产物见 **《游戏文件数据记录.md》**（`source/LocationMarker.xmsbt`、`source/简体中文地名表.csv` 等）；早前解包残留目录（`Pack/Bootup_CNzh/`、`source/cnzh/`、`extracted/`）已完成清理，仅保留数据文件。

---

*本文档由实测扫描 + ZeldaMods wiki 对照生成；顶层与 Pack 结构以本机实测为准。*
