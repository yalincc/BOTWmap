# live-npcs 逆向手册（BOTW × Ryujinx · NPC 定位）

> 速查用，不重复开发计划叙述。源码：`npcscan.go`（正式定位器）、`probeaf*.go`（探查模式）。
> 全部结论均为 2026-09-23/24 实机验证（Ryujinx Canary 1.3.351，BOTW 1.6.0，Hateno 村读档）。

---

## 1. 环境与进程

| 项 | 值 |
|---|---|
| 模拟器 | Ryujinx（进程名 `ryujinx.exe`），memory_manager_mode=`HostMapped` |
| 游戏 | 旷野之息 BOTW 1.6.0 |
| 相关服务 | :8778=live-npcs（本模块）、:8777=FloatNavi（浮窗）、:8766=BotwNavi（玩家定位） |
| 权限 | 读 Ryujinx 内存需管理员权限 |
| 存档锚点 | `%APPDATA%\Ryujinx\bis\user\save\...\game_data.sav`，PLAYER_POSITION hash `0xA40BA103` |

## 2. 内存映射（最关键公式）

```
host 地址 = guest 地址 + 0x016ADCA00000
```

- guest RAM 是 Ryujinx 内部分配的 host 段，NSO 已被 JIT 化，**zeldamods 的 0x7100000000 Switch 基址不可读**。
- 实机验证：guest 0x0B11B9BCA4（guest 区）→ host 0x016B15B9BCA4 读到 `"Animal_Deer_A"`。

## 3. 核心数据结构

### 3.1 ActorInfo（actor 原型 / 名字对象）

当前场景已加载的 actor 类型表。**字符串本身 + 4B hash**：

| 偏移 | 内容 |
|---|---|
| +0x00 | actor 名字字符串（`Npc_HatenoVillage003` 等） |
| -0x20 | 4B 类型指针（NPC→0x0AF75140 / Animal→0x0AF742C8，仅作特征，区不可读） |
| **+0x4C** | **4B actor hash**（**Npc_ 系列全部在 `0x34400000..0x34500000`**） |

⚠️ **伪 hash**：资源名（含 `:` 或 `/`，如 `Npc_HatenoVillage022:near01`、`.msbt`）hash=0x96000000 等垃圾值，**必须过滤**（`strings.ContainsAny(name, ":/")`）。

### 3.2 Actor 实例对象（我们要找的 NPC 本体）

内存中任意 **4B == hash** 的位置 = 实例引用点；其中带变换矩阵的才是实例：

| 偏移 | 内容 |
|---|---|
| +0x00 | 4B actor hash（= 名字对象 +0x4C 的值） |
| +0x40 | 4x4 变换矩阵起始（旋转 + 平移） |
| +0x50/+0x60/+0x70 | 矩阵行（3x3 正交旋转 + 平移列） |
| **+0x80/+0x84/+0x88** | **世界坐标 XYZ**（平移列重复，连续 3 float，直接读） |
| +0x0C | 缩放/包围半径（8~20 float） |

### 3.3 组件 / 伪引用对象（需过滤）

同一 hash 也出现在非实例对象里：

| 特征 | 示例 |
|---|---|
| +0x10..+0x30 单位矩阵对角 1.0、平移全 0 | Village002 @0x016B16D472B8 |
| 整段 0xFF | @0x016B16D4BD28 |
| 含设备字符串 `Weapon_R` / `Pod_A` / `ClerkAsk`（组件/对话对象） | @0x016B17DDA1B0、@0x016B16D81DC0 |

**判别 = 读引用点 +0x80/+0x84/+0x88，sane 且 |坐标|≤12000 且距玩家 ≤500m → 实例**。组件读出来是 0/1.0/指针 → 自动过滤。

## 4. 定位流水线（scanNpcInstances）

```
1. 玩家定位   findPlayerInRange(0x016B 区, window=200)   ← 只用于距离过滤
2. 收集 hash   扫已提交块内 Npc_ 名字对象 → +0x4C 取 hash（去重、滤资源名）
3. 反查        已提交块内 4B 扫描，收集 hash 引用点
4. 实例提取    引用点 +0x80/+0x84/+0x88 = 坐标；sane+距离过滤；按名字取最近
5. 输出        {name, x, y, z, moving}
```

## 5. 关键常量表

| 常量 | 值 | 说明 |
|---|---|---|
| `npcRegionLo` | `0x016B12000000` | 扫描下界（实测实例 0x016B16~1A、玩家 0x016B14） |
| `npcRegionHi` | `0x016B20000000` | 扫描上界（已提交 224MB，~2s/帧） |
| hash 区间 | `0x34400000..0x34500000` | ActorInfo hash 合法区间（Npc_） |
| 实例坐标偏移 | `+0x80/+0x84/+0x88` | 4x4 矩阵平移列 |
| 坐标上限 | `\|x\|, \|z\| ≤ 12000, \|y\| ≤ 12000` | 世界坐标 sane 边界 |
| 显示距离 | 500m | 只输出玩家 ±500m 内 NPC |
| 轮询 | 3s | NPC 多静止，够用且不卡 |
| 映射基数 | `0x016ADCA00000` | guest→host |

## 6. 已验证死路（勿重复踩）

1. 全内存 8B 反查名字字符串地址 → 0 引用
2. zeldamods 0x7100000000 基址 → Ryujinx 不可读（NSO JIT 化到 host x86 段）
3. 玩家坐标槽全内存引用扫描 → 0 命中；vtable 候选 = Ryujinx 内部 ID 表
4. 名字对象 ±0x1000 找坐标 → 全 NaN/0（实例坐标不在原型附近）
5. `Npc_xxx:near01` 资源名 hash → 0x96000000 伪值，全内存反查产出上千垃圾
6. 以玩家槽为锚 ±32MB 窗口 → **玩家 RepAddr 会漂移（0x016B14F6E06C ↔ 0x016B17DD9D9C）→ 9↔1 个 NPC 抖动**；必须固定扫 0x016B 区

## 7. 探查命令速查（live-npcs.exe）

| 命令 | 用途 |
|---|---|
| `--probeafc` | 一键定位当前场景 NPC（正式定位器，验证用） |
| `--probeafd` | dump 实例 vs 组件结构对比（判别依据） |
| `--probeaf9` | dump hash 引用点 ±0x40 结构（实例发现） |
| `--probeaf8` | 全内存 4B 反查单个 hash |
| `--npcscope` | 玩家附近扫 Npc_ 名字字符串 |
| （默认无参） | 服务模式 :8778，每 3s 刷新 |

编译：`& "D:\tools\go\bin\go.exe" build -ldflags "-s -w" -o "live-npcs.exe" .`（PATH 无 go，必须全路径）

## 8. 实机验证样本（Hateno 村，2026-09-23）

| NPC | 实例地址 | 世界坐标 (X, Y, Z) |
|---|---|---|
| Npc_HatenoVillage003 | 0x016B16D488A8 | (3357.63, 231.26, 2167.23) |
| Npc_HatenoVillage018 | 0x016B174A7228 | (3429.68, 236.62, 2093.74) |
| Npc_HatenoVillage018 | 0x016B17D60C58 | (3425.44, 226.78, 2163.10) |
| Npc_HatenoVillage004 | 0x016B16159070 | (3354.68, 223.72, 2153.10) |

服务运行实测：16 个 Npc_HatenoVillageXXX + 偶发 Npc_Road_008（路旅人，进出 500m 范围），坐标 5~74m，无假阳性。

## 9. 常见问题排查

| 症状 | 原因 | 处理 |
|---|---|---|
| NPC 列表空 | Ryujinx 未运行/未读档；或扫描区不含实例 | 检查 pid；跑 `--probeafc` 看输出 |
| 名单抖动 9↔1 | 玩家槽漂移（老 bug） | 已修复：固定扫 0x016B 区，勿改回锚点窗口 |
| 游戏卡 | 全量 6GB 扫描（旧启发式） | 停旧服务；当前 224MB + 3s 轮询不卡 |
| 浮窗地图无点 | localStorage 关过"显示实体" | 浮窗右键菜单勾选「显示实体」 |
| 名字标签不显示 | 缩放 < 22% | 滚轮放大地图（SC ≥ 0.22） |
| moving 全 false | NPC 静止（正常） | 移动中的 NPC 才标红点 |
| **NPC 位置跳动/跳到别人位置** | ① 未激活对象单位矩阵平移 (0,0,0) 混入；② hash 落在其他实例对象体内，+0x80 读出相邻对象坐标（如 Clavia 引用点落在 Toma 对象内 → 红点跑 Toma 位置） | 已修复：近零平移过滤（<5m 剔）+ 旋转矩阵判别（+0x40 3x3 正交）+ 坐标聚类取主簇 |
| **NPC 名字对不上（想核对）** | actor 名（Npc_HatenoVillage022）是内部 ID，非游戏显示名 | 已加 npcnames.json 映射 → API `label` 字段 → 浮窗显示显示名（Manny 等） |

## 10. 名字映射（M4）

- **数据**：
  - 英文 `live-npcs/npcnames.json`（441 条，社区全量 actor→显示名，源：GBAtemp PandaOnSmack Trainer 帖 Mizo/TheWord21 整理）
  - **中文 `live-npcs/npcnames_zh.json`（428 条，官方 romfs 权威）**：源 = `G:\YUZU\Switch Games\RomFS\Pack\Bootup_CNzh\Message\Msg_CNzh.product\ActorType\NPC.msbt`，已解析为 `BOTWmap\data\msbt_zh\NPC.json`（BOTWroms 归档产物），导出时只取 Npc_/Dm_Npc_ 前缀。
- **加载**：`loadNpcNames()` 启动时读 exe 同目录 JSON；中文优先（label=中文），英文对照存 `label_en`；`_Name` 后缀键自动注册去后缀别名（内存 actor 名是 `Npc_Road_008`，romfs/社区表是 `Npc_Road_008_Name`）。
- **API**：`/npcs` 每个 NPC 带 `label`（中文，如 万作）+ `label_en`（英文，如 Manny）。
- **Hateno 村对照**（中文=官方译名）：Village=科沙尤西(Reede)、001=瑟吉(Sayge)、002=阿喀恩佐(Pruce)、003=阿伊比(Ivee)、004=索特茨(Seldon)、005=纳茨宇基(Nack)、006=纳基珂(Nikki)、007=纳拉拉(Narah)、008=纳卜(Nebb)、009=森娜(Senna)、010=茨瓦布奇(Leop)、011=瓦太庚(Worten)、012=西默茨凯(Medda)、013=德利(Tokk)、014=茨琪米(Prima)、015=泰德(Thadd)、016=多当茨(Dantz)、017=罗丹特(Rhodes)、018=可莱维亚(Clavia)、019=索芙拉(Sophie)、020=阿玛莉丽(Amira)、021=瑟法罗(Sefaro)、022=万作(Manny)、023=乌梅(Uma)、024=萝莱尔(Ralera)、025=托可优(Koyin)、026=希恩(Aster)、027=阿俄塔(Azu)、028=泰奇波(Teebo)、029=库灵(Karin)、030=桂达(Karson)、031=松达(Hudson)、032=樱达(Bolson)、033=泰麦娜(Tamana)；Gate001=卡里尤、002=瓦图卢、003=普利拖司；Road_008=托托玛(Toma)、Road_020=贝黎斯(Teli)；Attacked_008=娜茨、009=玫古；MamonoShop=吉尔顿(Kilton)。

## 11. 稳定性修复（M4'，2026-09-24 实机验证）

**症状**：NPC 位置跳动、部分位置"奇怪"（红点跑到别人家）、名单时有时无。

**根因（probeafe 全引用点 dump 证实）**：每个 actor 名有 27~35 个 hash 引用点，其中：
1. **真位置副本**：活跃实例被多个组件持有者引用，同一坐标十几份（如 Nikki 主簇 17 点）；
2. **未激活/管理器对象**：单位矩阵平移 (0,0,0)/(1,0,0)/(0,0,1)，互相距离 <2m，贪心聚类会合并成**大垃圾簇**压过真位置簇（NPC 消失）；
3. **体内伪引用**：hash 落在其他实例对象内部字段，+0x80 读出相邻对象坐标（Clavia 引用点落在 Toma 对象内 → 红点跳 Toma 位置）。

**修复三连**（npcscan.go）：
1. **近零平移过滤**：坐标距原点 <5m 剔除（世界坐标原点在海域，真实 NPC 距原点 >100m）；
2. **旋转矩阵判别 `rotSane`**：+0x40 3x3 行范数 0.3~3 且列点积 <0.5（正交旋转）才算实例，体内伪引用读出非矩阵 → 剔除；
3. **坐标聚类 `majorityCluster`**：按名字收集全部合法坐标，2m 聚类取引用点数最多簇的质心（替代"取最近"，孤立伪点被压制）。

**实机结果**（Hateno 村，3 帧采样）：19 个 NPC 数量与坐标完全稳定；Clavia 稳定 (3434.5, 2093.5) 不再跳 Toma；浮窗显示 label（Aster/Manny 等）+ 状态栏"NPC 2/18 · Manny 3m"。

## 12. 探查命令速查（补充）

| 命令 | 用途 |
|---|---|
| `--probeafe=006,020,018` | 同一 actor 名的全部 hash 引用点 dump（跳动诊断；纯数字自动补 HatenoVillage 前缀） |

## 13. ActorInfo 全内存缓存（M5，2026-09-24 实机验证）

**症状**：部分 NPC 地图不显示（如茨琪米 014——玩家站她面前也缺失），其余 NPC 正常。

**根因**：NPC 名字对象（ActorInfo，字符串 +0x4C=hash）大多驻留在 **0x016A/0x020C 等资源区**（`--nameregions`：0x016A 桶 26305 命中、0x020C 桶 25080 命中），而 0x016B 运行时区只是**实例区**（hash 引用点）。旧代码只在 0x016B12~0x016B20 扫名字对象 → 名字对象不在该区的 NPC（014 等）hash 未知 → 无法反查 → 缺失。且曾出现 `refresh: 0 hashes` 的次生 bug：`seen` 去重在读 hash **之前**，第一个命中（普通内嵌字符串，+0x4C 非合法 hash）占坑后跳过真正的 ActorInfo 副本。

**修复（actorinfo.go）**：
1. **全内存收集**：`refreshActorInfo()` 扫全部已提交块（guestBlocks），收集 Npc_ 名字对象 hash（hash 校验通过才 `seen` 占坑）；
2. **低频缓存**：启动即后台 goroutine 收集（~7s），此后每 60s 自动刷新（玩家换区域最多 1 分钟延迟，可接受）；`actorHashSnapshot()` 读锁快照供每帧扫描用；
3. 每帧扫描仍只反查 0x016B 实例区（~2s/帧 不变，不卡）。

**实测**：`[actorinfo] refreshed: 46 Npc hashes in 7.2s`；玩家在旅店，茨琪米 (3466.2,2097.3) 距玩家 0.6m 命中，浮窗标签"茨琪米" + 状态栏"NPC 0/14 茨琪米 1m"。
