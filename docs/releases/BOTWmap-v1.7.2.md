# BOTWmap v1.7.2 版本报告（2026-10-22）

## 1. 版本概述

- **目标**：根治用户反馈「标记宝箱完成后，同坐标的武器/装备图标不跟着标记完成」——宝箱↔装备**点位级一一对应联动**（老大提案、三轮讨论定型）。
- **最终结论**：**已正式发布**。自动化门禁 209 → **212 项全绿（212 passed, 0 failed）**；存档链路 e2e **13 项全绿**。
- 同批把已在库未登版的两个功能一并发齐：xnavi /save 管道全自动探索度同步（92a3ead）、设置面板「移动轨迹」（3fd845c）。

## 2. 发布内容

- **新增（宝箱↔装备点位级联动，老大拍板规则）**：
  - 宝箱与装备一一对应：开一个宝箱只灭**该点**的装备图标（变灰 + 绿勾），其他地区同名装备不受影响；
  - 一件装备名下宝箱**全部**开完 = 整件完成（卡片绿勾）；取消宝箱完成同样可逆；
  - 以存档为准，手动变更不特殊处理；**不加任何存档加载开关**（不想同步用「清空本地探索度」）；
  - 防误操作：宝箱全开的推导完成态下，点装备卡「取消完成」不误清宝箱标记；
  - 装备卡「获取来源」宝箱 chip 显示已开启态（绿色序号 + 虚线半透明 + ✓），与地图完成态同源实时。
- **随批首发（提交已在库，版本随本批）**：
  - xnavi /save 管道全自动同步全量探索度（任务/宝箱/讨伐/人马/图鉴/世界档位，存档更新即加载）；
  - 设置面板「移动轨迹」显示/隐藏/清除 + 设置面板手机端 max-height 溢出修复。

## 3. 技术方案

- **根因**：宝箱完成按坐标点记（`botwmap.chestDone.v1` / `botwmap.chestSaveDone.v1`），装备完成按装备 id 记（`botwmap.equipDone.v1` / `botwmap.equipSaveDone.v1`），两套独立；原只有「装备→宝箱」单向联动（`EquipDone.toggle` → `ChestPanel.markPoints`），反向没有任何推导。
- **数据层实测（实现前验证）**：装备 414 个宝箱获取点与宝箱表坐标 **100% 精确重合**（最近距离 < 0.5px）；宝箱 content 字段 121/163 直接等于装备 id，其余 42 项为矿石/箭矢/卢比等非装备类。→ 点位级完成态可直接**实时推导** = `ChestPanel.isDone(pt)`，**零新增存储**，不需要坐标模糊匹配。
- **关键口径**：`EquipDone.isDone(id)` = 手动 ∪ 存档图鉴 ∪ `allChestsDone(id)`（≥1 个宝箱点且全部已开）；地图 paint/hit 按**点位级**判完成（原整件级口径废弃——图鉴登记但宝箱未开的装备，其宝箱点不再提前灭掉，与「开箱=拿了」直觉一致）；`toggle` 联动宝箱加 `had` 守卫，只在手动状态真正变化时级联。
- **模块边界**：chest.js 零改动（原有 `ChestPanel.onChange` 广播够用）；equip.js 订阅该广播实现装备卡开着时实时重渲染。
- **版本事实**：线上 v1.7.1 tag 不含 /save 同步管道（92a3ead 在 tag 之后入库）；EdgeOne 随 push 自动部署，故本批 push 即全量上线。

## 4. 踩坑记录

| 坑 | 根因 | 修复 | 教训 |
|---|---|---|---|
| 新断言首跑 FAIL | 断言条件把 `undoneItem` 写反（期望 false 却要求真值）——功能输出全对 | 修正断言条件 | 行为断言先写清"期望值是什么"，别把观测值直接当条件 |
| e2e「isDone 生效 = 图鉴数」FAIL（94 vs 90） | 旧断言按旧口径（isDone 恰好等于图鉴数）；多出的 4 件 = 宝箱全开但图鉴未登记的推导完成 | 断言改口径：`isDone = 图鉴数 + 宝箱推导数` | 联动类改动会波及"精确相等"型旧断言，改口径前先证明多出的部分正是新语义 |
| equip.js 订阅 ChestPanel 时序疑虑 | chest.js 在 equip.js 之后加载，担心 `window.ChestPanel` 未定义 | chest.js 在 IIFE 顶层同步定义 `window.ChestPanel`（早于 DOMContentLoaded），订阅安全 | script 加载顺序问题先看定义时机（解析期 vs 引用期），别急着改加载顺序 |

## 5. 测试与验收

- `tools/verify.bat --all`：**212 passed / 0 failed**（209 → 212，新增 3 条：标记宝箱→整件推导完成+卡片绿勾+取消可逆 / 装备卡宝箱 chip 已开启态 / 推导完成态下取消装备不误清宝箱标记）；
- `tools/e2e_save_p4.js`：**13 passed / 0 failed**（1 条断言按新口径更新，见踩坑表）；
- 数据层离线验证（Node）：装备宝箱点 ↔ 宝箱表 414/414 精确重合；content → 装备 id 命中 121/163。

## 6. 遗留与后续

- `docs/backlog/BACKLOG.md`：Ryujinx 选档策略修复、大师档槽位指引文案、地图坐标网格 overlay、三维高度仅空中点显示；
- `PLAN-SAVE-FLOW-AND-PROGRESS-SYNC.md`（讨论稿）三方向待老大拍板（GUI 存档可见性 / 进度同步扩充 / 存档板块模块化）。

## 7. 回溯

- 提交范围：`app/js/equip.js`（?v=34）/ `app/css/style.css`（?v=67）/ `app/index.html` / `CHANGELOG.md` / `README.md` / 本报告；
- 版本号同步四处：`app/index.html` 页脚 / `README.md` / `CHANGELOG.md` / 本报告；tag `botwmap-v1.7.2`；
- 本地不入库：`tools/verify.js` / `tools/e2e_save_p4.js` 断言、`docs/INDEX.md`、`tools/FEEDBACK.md`（`*.md` 全局忽略，docs/releases 例外）；
- 归档：`PLAN-XNAVI-SIMPLIFY-AND-STABILIZE.md` → `归档/开发计划/`（对应 xnavi v3.2.0 已发布）。
