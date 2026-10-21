# 怪物层重建方案（分阶段交付）

> 文件名沿革：原名 `V1.6.6-REDO-PLAN.md`（2026-10-16 文档体系整理时去掉版本号 —— **计划类文档不领版本号**，版本号只在发布那一刻生成，见 `../spec/DEV-HANDBOOK.md` §3.2）。

> **状态**：**已全部完成并全量测试全绿（164 passed, 0 failed）**，P0~P5 全链路闭环。准备发布。
> **前置阅读**：`docs/releases/UNPUBLISHED-BOTWmap-V1.6.6.md`（回档报告 + 九坑表）、`docs/notes/HANDOFF-ENEMY-TIER.md`（数据件/管线/接线清单）。
> **数据口径权威**：`E:\WorkSpace\BOTWroms\raw\romfs\DATA-README.md` §12。

---

## 1. 背景

V1.6.6（怪物层换源官方缩放链，根治幽灵怪）于 2026-10-15 18:49 上线；用户实测暴露多处联动问题（侧栏小怪行消失、世界档位显示 0、营地图钉全灭、武器显示英文代码），三连热修后收敛速度不达预期，当日回档至 v1.6.5。
数据管线与算法**已验证正确**，问题集中在**前端接线与初始化时序**。

## 2. 回档后现状核查（2026-10-15 22:35 实测，只读）

| 项 | 状态 |
|---|---|
| 生产代码 | ✅ 回档干净：`app/` 下 `BOTW_TIER_CAMPS` / `EnemyTiers` / `BOTW_TIERS` **零引用**；`enemy-tiers.js` 与 `data/enemy-tiers/` 均不存在；`index.html` = `camp.js?v=15` |
| 数据管线 | ✅ 完好且已修：`source/build_botwmap_tiers.py`（21:56 版）固化九坑全部修复 |
| romfs 源件 | ✅ 齐全：`enemy-scaling.json` / `enemy-distribution.json` / `enemy-distribution-by-tier/` |
| 废弃实现 | ✅ git 可查：`f4f158a`（主干 13 文件 284 行）+ `968037b`（热修一）+ `f48ec2f`（热修二） |
| **测试基线** | ❌ **破损**：`tools/verify.js` 未随回档还原，呈"半 tier"混合态（p4c 段已 tier 化、smoke/p4 段仍旧口径，且断言 `camp.js?v=23` ≠ 现场 `v=15`；`tools/` 不在 git 追踪内，`git checkout` 还原不到） |

**实测基线**（`verify --all`，59s）：**92 passed / 1 failed** + p4c 第 7 项崩溃（`BOTW_TIER_CAMPS` undefined）→ 其后 8 项未执行。

## 3. 失败根因（比原报告更本质）

1. **交付粒度 = 大爆炸**：单 commit 同时上 6 件事（数据件换源 + 新模块 + 存档选档 + 营地 reload + UI 徽标 + 天地条开关），13 文件 284 行、四层耦合 —— 任一层错都表现为同一个现象「地图全灭」，无法从现象反推层级。
2. **异常不可见 + 时序脆弱**：`save_upload.js` 恢复段 `try-catch` 吞掉 apply 异常（「无报错 ≠ 没跑」）；camp boot 与 save_upload.init 靠「兜底调用」硬拼时序。
3. **测试断言与数据量级耦合**：verify 硬编码数据量（54/555），`?v=` bump 后漏改 → 断言漂移。

## 4. 技术方案

### 4.1 关键红利：loc 口径已天然对齐

生成脚本产出的 `camps.loc = [[px,py]]`（**嵌套刷点数组**），与现场 v1.6.5 `camp.js` 的 `campsNearMarker` / `clusterLocs` 既有口径（`(c.loc||[]).some(p => p[0]-px[0]...)`）**完全一致**。
→ **重做时营地 hit 逻辑基本不用改**，「图钉全灭」这个根因（V1.6.6 坑表第 1 条）**直接从方案中消除**。

### 4.2 四条实施原则

- **A 数据先行**：数据件是纯离线产物，先落库、先自检，不碰线上。
- **B 引而不接**：先让模块能加载但默认不生效，页面行为零变化。
- **C 一层一换**：每次只换一类源的渲染，旧路径保留，出问题只 revert 一步。
- **D 异常可见**：重做后的 apply 链路**禁止 try-catch 吞异常**，失败必须能在 console 一眼看到。

### 4.3 关键决策（沿用 V1.6.6 已验证结论）

- **boss 5 类**（lynel/hinox/talus/molduga/guardian）**保留 data.js 旧标记**不动 —— 讨伐绿勾 `liveIds` / `#N` 编号 / `enemy_save` 92 条匹配全依赖旧 id 稳定。
- **`camp_weapons.js` 保留**：equip 卡「武器 → 营地」反查仍走旧源（换源列 v1.6.7 候选）。
- **无存档 = tier-0**（新档真实状态）；有存档按 §12 算法算积分 → 各链档位。
- **生成脚本是唯一数据源**，产物「勿手改」；坐标换算与字段裁剪放脚本，前端零算法。

## 5. 五阶段路线图（先易后难）

| 阶段 | 内容 | 难度 | 是否上线 | 验收 | 回退 |
|---|---|---|---|---|---|
| **P0** | 修 `tools/verify.js` 回纯 v1.6.5 口径 + 清回档残留 | ★☆☆☆☆ | 不影响 | `verify --all` 全绿 | 不涉及生产 |
| **P1** | 跑管线生成 7 件数据件 + 离线自检护栏 | ★★☆☆☆ | **不上线** | 自检脚本全过 | 删目录即可 |
| **P2** | 建 `enemy-tiers.js` + `index.html` 引入，**不调用 `apply()`** | ★★☆☆☆ | 可上线 | 标记数/侧栏行数与改前逐项一致 | revert 一步 |
| **P3** | 存档积分计算 + Lv 档位徽标（**不动地图标记**） | ★★★☆☆ | 可上线 | 槽 5 存档 → Lv4（10065 分）；无存档 → Lv1 | revert 一步 |
| **P4** | 小怪 10 类 + dragon 换源 + DLC2/大师开关（**主目标：幽灵怪根治**） | ★★★★☆ | 可上线 | §12 三用例（台地 0 蜥蜴 / 开 DLC2 出 16 / 链档改名） | revert 一步 |
| **P5** | 营地层换源（`camp.js reload` + tier 营地数据） | ★★★★★ | 可上线 | 营地计数鲁棒断言 + 莱尼尔并入 + 卡片字段 | revert 一步 |

> **P0–P2 做完全是"页面行为零变化"** —— 等于把地基与护栏全部铺好，风险为零；可见变化从 P3 开始。

## 6. 本轮范围（P0-P2）施工清单

### P0 · 基线对齐
- `tools/verify.js` p4c 段回纯 v1.6.5 口径（6 处）：`BOTW_TIER_CAMPS` → `CAMP_WEAPONS`；计数断言 → `54/555` 精确值；莱尼尔 → `=== 11`；`js/camp.js?v=23` → `?v=15`；描述去 tier 字样。
- 清理回档期诊断残留：`tools/tmp_diag2[4-8].js`、`tools/tmp_patch.js`。
- **验收**：`verify --all` 全绿。

### P1 · 数据件离线重建 + 自检护栏
- 跑 `source/build_botwmap_tiers.py` → `app/data/enemy-tiers/` 7 件（tier-0~3 / quest / hardmode / scaling）。
- 新增 **离线自检脚本**（node 直跑，不开浏览器），校验：
  1. `camps.loc` 为嵌套 `[[px,py]]` 且非空；
  2. 无 `NaN` / `null` 坐标（pos 异常已跳过）；
  3. cat 覆盖完整（无未映射 actor skip）；
  4. 武器 key 无 `-` 后缀、无 `*Pipe`；
  5. 四档 markers 同序同长（index 对齐）；
  6. 计数与 §12 量级对拍（markers ≈3847/档、camps ≈1416/档 hot 96~105、quest 92、hardmode 765）。
- **验收**：自检全过；数据件**不进 `index.html`**（不上线）。

### P2 · 静态模块零行为接入
- 新建 `app/js/enemy-tiers.js`（复用 `f4f158a` 实现，**去掉自动 apply**，`apply()` 未就绪时空操作返回）。
- `index.html` 按序引入 `data/enemy-tiers/scaling.js → tier-0..3.js → quest.js → hardmode.js → js/enemy-tiers.js`，bump 相关 `?v=`（**同步 grep `tools/verify.js` 硬编码版本号**）。
- **验收**：`verify --all` 全绿，且页面标记数 / 侧栏行数与改前逐项一致。

## 7. 风险与回退

- 每阶段 = 一个独立 commit + 独立验收 + 独立可 revert。
- P0 / P1 对线上零影响；P2 虽上线但行为零变化。
- **基线全绿是每阶段放行前提**；每步 bump `?v=` 后必须 grep 测试断言并重跑 verify。

## 8. 后续（P3-P5 待本轮完成后另出方案）

- P3 存档选档计算 + 徽标展示（F-006）
- P4 小怪层换源 + 开关（F-004/F-005/F-008/F-009/F-010）
- P5 营地层换源（F-007）
- 相关反馈台账：`tools/FEEDBACK.md` F-004~F-011

---

## 9. 执行记录（2026-10-15）

### 9.1 地基三件套（P0-P2）

| 阶段 | 结果 | 关键数据 |
|---|---|---|
| P0 | ✅ 完成 | `verify --all` **100/100**（改前 92 passed / 1 failed + p4c 崩溃）；verify.js 改 6 处；清 6 个 tmp 残留 |
| P1 | ✅ 完成 | 7 件数据件（约 6.3MB）；`tools/check_tiers.js` **31/31**；量级对拍 3845/1416/100~105/92/765 |
| P2 | ✅ 完成 | 随 P3 在 `index.html` 按序引入 7 件数据件 + `enemy-tiers.js`，带 `?v=` |
| P3 | ✅ 完成 | `verify --all` **143/143 全绿**；侧栏存档区世界档位徽标展示（Lv1~Lv4）；真实槽 5 验证 10135 分 Lv4；清空归隐；纯增量零触碰地图标记 |
| P4 | ✅ 完成 | `verify --all` **155/155 全绿**；小怪与龙换源官方缩放；台地默认 0 蜥蜴（根治幽灵怪）；设置面板补齐 DLC2 开关；随档改名改色；Boss 5 类与绿勾完好；设置按钮 Toggle 修复 |
| P5 | ✅ 完成 | `verify --all` **164/164 全绿**；营地数据源由旧营地平滑切换至 `BOTW_TIER_CAMPS`（1416/100）；严格保持 `[[px,py]]` 嵌套口径；武器随档动态升级且 key 清洗无残留；时序兜底与重置完备 |

**P1 自检新发现（已修正）**：`Enemy_Dragon_Grudge` / `_Far` 两条 —— `nameCn` 为 null、`sensorMode`/`chain`/`equip`/`drops` 全空、**id 重复**（782489849）、同坐标，属剧情演出锚点而非可讨伐怪，渲染即「空名图钉」。已在生成脚本 `EXCLUDE_ACTORS` 加入排除，markers 3847 → **3845**。

### 9.2 本批实际上线 = 路线 A「最小止血」（2026-10-15 晚）

- **老大拍板**：DLC2 一击剑任务怪是一次性任务怪（触发任务才生成、杀光不再刷，做完就没了），不需要显示在地图上
- **落地**：`source/build_enemies.py` 增加剔除逻辑（依据 radar 源 `spawn == 'ichigeki'` 坐标匹配，容差 3 游戏单位 = 6 像素）
- **效果**：
  - 全站怪物标记 **3437 → 3344**（剔除 93 点）
  - 台地/城堡西 **144 → 61**，其中蜥蜴战士 **16 → 0**
- **测试**：`verify --all` **102/102**，含 2 条新防回归断言（台地无蜥蜴战士 / 总数 3344）
- **发布**：**不更新版本号**（仍 V1.6.5），CHANGELOG 记入 V1.6.5 节「小修」

### 9.3 遗留待办

1. **P2 引用待加回**：P3/P4 开工时在 `index.html` 引入 7 件数据件 + `enemy-tiers.js`
2. **加载性能**：6MB 数据件建议 `defer` 或按需加载
3. **部署缓存**：`edgeone.json` 的 `/data/*` 未配长缓存 → 建议补（配合 `?v=`）
4. **DATA-README §12 口径同步**：怨念锚点过滤 + markers 3845 是否回写 romfs 侧（待老大定）
5. **命名澄清**：本方案 P3/P4/P5 与项目历史 P3/P4（v1.6.3 装备数据层/营地刷图层）**撞名**，沟通须加限定词

## 附：不可遗忘的九坑（重做时逐条核对，详见 `docs/releases/UNPUBLISHED-BOTWmap-V1.6.6.md` §4）

1. loc 嵌套口径（根因级）2. cat 拼写 Moriblin/Octarock 3. killScores 在 enemy-scaling.json 4. camps.weapons 是 `{item,count}` 数组 5. index 对齐非 id 6. camp hit 坐标口径 = 地图像素 7. 小怪标记与营地刷点同源重合（click 被抢）8. 初始化时序 9. apply 异常被 try 吞
