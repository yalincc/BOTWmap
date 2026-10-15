# V1.6.6 重做交接文档（怪物层重建 · 官方缩放链）

> **读者**：接手 V1.6.6 重做的 AI/开发者。
> **前置阅读**：`docs/releases/UNPUBLISHED-v1.6.6.md`（版本报告，含回档原因与 9 坑表）、`E:\WorkSpace\BOTWroms\raw\romfs\DATA-README.md` §12（数据口径权威）。
> **状态**：数据管线与算法已验证可复用；待重做 = 前端接线（约 1 天）。废弃实现可 `git show f4f158a` 全量参考（未丢失）。

---

## 1. 目标与验收标准

**目标**：怪物层换源官方缩放体系，实现"地图显示你这个存档下真实存在的怪"。

**验收（§12 三用例 + 回归）**：
1. 台地/城堡西（gx -1250~-480, gz 1480~2320）默认 0 任务怪；开 DLC2 开关后出现 16 只蜥蜴（quest 节 92 总量中台地部分）
2. 小怪层按链档改名：mock chainTier 全 3 → `Bokoblin_Junior` 布点显示 `Bokoblin_Dark` 的中文名
3. 人马卡讨伐/阶段行走官方积分口径（26 杀 ≈ 积分 2220+，链档 3 = 白银）
4. verify --all 全绿 + e2e 13/13 + 专项 ≥9

## 2. 数据件（已提取，源在 romfs）

| 源件 | 路径 | 内容 |
|---|---|---|
| enemy-scaling.json | `E:\WorkSpace\BOTWroms\raw\romfs\` | 官方 LevelSensor：`enemyChains`（5 链，actors[{actor,threshold}]）+ `killScores`（50 条 {flag,point}，flag 即 `Defeated_Enemy_XXX_Num` 形式）+ `weaponTables` |
| enemy-distribution.json | 同上 | `chains`（同 enemyChains）+ `hardModeGold` + `instances[]`（4710：id/actor/nameCn/pos[x,y,z]/cell/src/spawn/sensorMode/chain/tierIdx/equip/arrow/sharpJudge/attackPower/drops） |
| enemy-distribution-by-tier/tier-0~3.json | 同上 | 按档预计算世界：`monsters`（3853，四档同序同长，index 对齐）+ `camps`（1416 物理营地，60 单位网格聚类，weapons 已按档解析）+ `quest`（92 ichigeki）+ `hardmode`（765） |

**权威口径**：DATA-README.md §12（含算法标准与已知边界）。**已知边界**：神庙/神兽内部布点不在 radar 源；素材 drops 为 Item_ id 未接中文名。

## 3. 生成管线（已验证，可直接跑）

```bash
source/build_botwmap_tiers.py   # venv python 直跑（纯标准库）
# 输出 app/data/enemy-tiers/：tier-0~3.js（~1.35MB/件）+ quest.js + hardmode.js + scaling.js
# 格式：window.BOTW_TIERS[N] = {categories, markers, camps:{hot,all}}；window.BOTW_SCALING = {chains, killScores, tierNames, hardModeGold}
```

**脚本内已固化的关键口径（重做时逐条核对）**：
1. `px = x*2+12000, py = z*2+10000`；pos 异常（缺 z/None）**跳过**（NaN→JSON null→图钉全灭，本轮回档根因之一）
2. **loc 必须嵌套**：`loc: [[px, py]]`（刷点数组口径，clusterLocs/campsNearMarker 期望；给 [px,py] 单点会让 pt=[null,null] 全灭）
3. **武器 key 清洗**：`k.rstrip('-')`（radar equip 原值带 `-` 后缀，不洗则 DEX miss 显示英文代码）；过滤 `*Pipe`（号角不可拾取）
4. **cat 映射**（拼写以 romfs 原文为准）：Bokoblin/Moriblin/Moblin/Lizalfos/Keese/Chuchu/AirOctarock/Octarock/Wizzrobe/Assassin/Guardian/Lynel/Giant(hinox)/Golem(talus)/Sandworm(molduga)/Dragon/Pebblit；**排除** Enemy_Ganon*/Enemy_SiteBoss*
5. camps 条目：`species = 主怪 chain`（前端按链档混装）；hot = 武器含 weapon_dex 的 strong/element
6. marker 结构：`{id:'em<N>', cat, name(nameCn), actor, px:[px,py], link, region/tower(最近希卡塔), spawn, chain, tierIdx, w, arrow, _tm:1}`

## 4. 前端接线清单（重做施工顺序）

### 4.1 index.html
- 移除 `js/enemies.js`；按序加 `data/enemy-tiers/scaling.js → tier-0..3.js → quest.js → hardmode.js → js/enemy-tiers.js`（数据件在 data.js 之后、main.js 之前；模块不自动 apply）

### 4.2 新模块 js/enemy-tiers.js（全逻辑）
- `buildIndex()`：`chainArr[tier][species] = [marker...]`（**各 tier 同序同长，index 对齐——id 不唯一（2836 唯一/3853 条），禁止按 id 对齐**）
- `composeMarkers()`：tier-0 中 `!chain` 的实例 + 每 chain 按 `chainTier[sp]` 从 `chainArr[t][sp]` 取同游标条目；叠加 quest/hardmode（dlcState 开关）
- `composeCamps()`：同构（按 `c.species` 游标混装）
- `apply()`：swap `BOTW_DATA.markers`（filter `!_tm && !SMALL_CATS[cat]`，SMALL = 小怪 10 类 + dragon；**boss 5 类保留旧标记**——讨伐绿勾 liveIds/#N 编号/enemy_save 92 条全依赖旧 id）+ **categories 注入 + 只补缺失行**（append 缺失的 lrow——**禁止 buildLayerList 全量重建**，会清掉 camp 子行等手术行）+ 徽标刷新 + `CampLayer.reload(BOTW_TIER_CAMPS)` + `_buildIndex` + draw

### 4.3 camp.js
- `reload(data)`：**SUB 整体重赋值** `SUB = { hot: nHot, all: nAll }`（属性清空有残留风险）+ 防御（loc 无效跳过，注意嵌套口径 `c.loc[0]` 是 [x,y]）+ 子行计数更新 + `CAMP_ORDER/CAMP_IDX` 重建
- `campsNearMarker(px, cat)`：**some 嵌套遍历** `(c.loc||[]).some(p => p[0]-px[0]...)`（嵌套口径）
- `enhanceLynelCard`：掉落段直读营地 w（已按档解析）；`LYNEL_OVERRIDES` 人工映射保留（Lynel23 mod 点位）
- 初始化时序：**camp boot 尾部兜底 `EnemyTiers.apply()`**（save_upload.init 的 apply 早于 boot，CampLayer 未就绪会被跳过）

### 4.4 save_upload.js
- 解析循环加分支：`ksHash[crc32(k.flag)]` 命中 → `ksCnt[k.flag] = v`（50 条计分）
- 解析后：`points = Σ point × cnt`；每链 `t = 门槛≤points 最大档`，**cap 3**（Moriblin 链 5 档含 Gold，普通模式上限 Dark）；`worldTier = 最高链档`
- 持久化：saveCounts 加 `chainTier/worldTier/points`；恢复段恢复 + apply

### 4.5 ui.js
- 敌人组标题旁【LvN】徽标（`Lv = worldTier+1`，hover title 含积分与档位说明）+ DLC2/大师模式 toggle（dlcState localStorage `botwmap.dlc.v1`）
- `window.UI = UI` 显式挂载（顶层 const 不自动挂 window）
- buildLayerList 的敌人组分支：含 DLC2/大师 toggle + 徽标（重做时按 docs/releases/UNPUBLISHED-v1.6.6.md 的设计）

## 5. 已验证的算法快照（2026-10-15 实测）

- 老大槽 5 存档：43/50 计分怪命中、**积分 10065**、链档 Bokoblin=3/Moriblin=3(cap)/Lynel=3/Lizalfos=3/Assassin=1、worldTier=3（Lv4 白银世界）——与游戏内白鬃/白银实测吻合
- 数据件量级：markers 3847/档、camps 1416/档（hot 96~105）、quest 92、hardmode 765

## 6. 九坑速查（详见 docs/releases/UNPUBLISHED-v1.6.6.md §4）

1. loc 嵌套口径（根因级）2. cat 拼写 Moriblin/Octarock 3. killScores 在 enemy-scaling.json 4. camps.weapons 是 {item,count} 数组 5. index 对齐非 id 6. camp hit 坐标口径=地图像素 7. 小怪标记与营地刷点同源重合（click 被抢→测试用 openCard 编程路径）8. 初始化时序（camp boot 兜底）9. apply 异常被 save_upload 恢复段 try 吞（pageerror 空 ≠ 没跑）

## 7. 测试基线

- 专项 ≥9（§12 三用例 + 混装 + id 稳定 + override）；verify 适配点：营地计数鲁棒断言（all≥hot>0）、莱尼尔并入数 ≥5、点击测试 openCard 编程路径、tgt 换 BOTW_TIER_CAMPS、camp v 断言同步
- 已知 flaky：camp 子行计数在 apply 时序下偶发旧值（reload 的 lprog 更新覆盖）
