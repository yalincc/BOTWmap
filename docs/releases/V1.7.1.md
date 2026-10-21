# BOTWmap V1.7.1 版本报告（2026-10-21）

## 1. 版本概述

- **目标**：根治用户反馈「网站加载大师档存档后不显示时间」（F-019）——上传校验防误传 + PlayTime=0 明示；并按老大指示将世界等级徽标移至「探索度」折叠行右端。
- **最终结论**：**已正式发布**。调研全文见 `docs/notes/REPORT-MASTERMODE-SAVE.md`；自动化门禁 201 → **203 项全绿（203 passed, 0 failed）**。

## 2. 发布内容

- **修复（F-019，存档上传防误传）**：
  - `app/js/save_upload.js?v=31` 三处防护：① 文件名拦截 `caption.sav` / `option.sav`（提示选同文件夹 `game_data.sav`，重命名主存档不受影响）；② 条目数阈值 100 → 10000（双保险）；③ PlayTime=0 时 toast 明示「（游玩时间未记录）」，不再静默省略；
  - i18n 三键中英双语：`save.wrongFile` / `save.entriesTooFew` / `save.noPlaytime`。
- **修复（Lynel23 人马大师限定门禁）**：
  - 背景：#23 白马是大师模式限定生成的点位（普通模式无此布点；兽神大剑+兽神弓为 V1.6.5 人工实测掉落），此前常显在普通模式地图；
  - 修复：`app/js/data.js?v=6` 给 Lynel23 打 `hard:1` 数据标记 + `app/js/enemy-tiers.js?v=8` 在 apply() 单点随大师开关摘放（与大师层注入同点，绘制/命中/计数/搜索全链路一致）；
  - 效果：普通模式地图人马 22 只（侧栏计数同步），开「大师模式专属怪」开关后第 23 只出现，卡片 / #N 编号 / 实测掉落说明全部保留；
  - 未采用方案（老大提议后评估）：改成大师层小怪注入——会丢图标 / #N 编号 / Boss 卡片掉落信息，且 SMALL_CATS 本就不收 boss 类。
- **变更（世界等级徽标移位）**：
  - 徽标从存档加载区移至「探索度」折叠行右端（第一版放敌人大类标题右侧，老大反馈不协调且英文截断，二移定稿）；
  - 显示格式 `世界等级N（M分）` / `World LvN (M pts)`，悬停阶梯提示内容不变；`enemy-tiers.js` + `style.css?v=64`。
- **测试**：`tools/verify.js` 新增 3 条回归断言（caption.sav 误传拒绝、徽标位置、Lynel23 门禁摘放）。

## 3. 技术方案

- **根因（实测锤实）**：`caption.sav` 头 4 字节 = version `0x471E` + marker `FFFFFFFF`（与 `game_data.sav` 相同）→ 骗过版本白名单；1528B > 1024 → 骗过大小检查；189 条 > 100 → 骗过条目检查；文件内无 `PlayReport_PlayTime` → 静默"加载成功"只差时间。`option.sav`（344B）被大小检查挡住（loud fail），**caption.sav 是唯一能静默漏网的文件**。
- **大师档同构性实证**（Eden 真档 + Ryujinx 交叉）：大师档与普通档 version / 大小 / 条目数完全相同，PlayTime 从第一个存档起就非零（216/219 秒）→ 彻底排除"大师档格式差异"假设；PlayTime 哈希在 16 个存档中均只出现 1 次 → 排除字符串撞哈希。
- **附带结论**：`IsLastPlayHardMode` 在 `option.sav` 不在 `game_data.sav` → F-017「自动识别大师档」不可行，已降级为槽位文案指引（入候选池）。
- **徽标定位方案**：与折叠按钮**平级 + 绝对定位**（`.stats-head-wrap` 包裹层）——躲开 `toggle.textContent` 重绘冲掉子节点的坑，且徽标点击不冒泡、不触发折叠。

## 4. 踩坑记录

| 坑 | 根因 | 修复 | 教训 |
|---|---|---|---|
| 徽标放折叠按钮内被冲掉 | `ui.js` 用 `toggle.textContent` 重设按钮文字，子节点全灭 | 改平级 + 绝对定位包裹层 | 动态重设 textContent 的容器不能挂子节点 |
| 徽标英文在敌人大类被挤截断 | 组标题行与「标签 + 全选按钮」抢宽度 | 移至整宽的探索度行 | 长文本徽标优先挂整行容器 |
| 误把 Marc 编辑器 "Master editor" 当大师模式 | 其 master = raw 全字段编辑器，与游戏大师模式无关 | 查提交历史 + 官方文档证伪 | 第三方工具命名坑，先查证再引用 |
| 0x0F423D/3E/3F 疑云 | 社区无文档的三个"1.6 变体"版本值 | 判定非大师档版本（官方表无载 + 双编辑器同加 + FILESIZE 与 v1.6 同） | 查无实据的值不进白名单，靠结构式校验兜底 |
| ui.js 改了又撤 | 徽标从敌人大类二移探索度行，建组逻辑整体作废 | git diff 核实零改动后回退无用 bump | 发布前用 git diff 复核净改动，别把过程 bump 带上线 |

## 5. 测试与验收

- `tools/verify.bat --all`：**203 passed / 0 failed**（V1.7.0 基线 201 → +3；本机追踪服务占用 8766 时会多报几条「无 JS 报错」环境噪音，与本次改动无关）；
- 新增断言：9c3 构造「版本合法 + marker + 189 条假条目」的 caption.sav 拖入必须明确拒绝；徽标必须位于 `.stats-head-wrap`（探索度行）；Lynel23 普通模式不在基础标记、开大师开关后回归；
- P4 Boss 计数断言改为大师模式感知（先开大师拿全量 255 再还原），避免与 Lynel23 门禁互相打架；
- playwright 截图实测：徽标与按钮行垂直中心完全重合（y=583）、右对齐 14px 边距、英文 `World Lv4 (7985 pts)` truncated=false；
- 真实存档链路：Ryujinx slot5 实测 `世界等级4（7915分）` 正常显示，清空后优雅隐藏；
- 手机端 390×844：徽标不换行、不溢出。

## 6. 遗留与后续

- Ryujinx 选档策略修复（大师档永不当主档，`/progress` 显示普通档进度）——候选池 中/S，修复前须验证 Ryujinx mtime 可靠性；
- 大师档槽位指引文案（F-017 降级项）——候选池 中/S；
- 地图坐标网格 overlay（S）+ 三维高度仅空中点显示（M）——待老大排期；
- F-019 闭环：caption.sav 类误传已有断言防回归。

## 7. 回溯

- 提交范围：`app/js/save_upload.js` / `app/js/enemy-tiers.js` / `app/js/data.js` / `app/index.html` / `app/css/style.css` / `app/data/i18n/{zh,en}.js` + `CHANGELOG.md` / `README.md` / 本报告；
- 版本号同步四处：`app/index.html` 页脚 / `README.md` / `CHANGELOG.md` / 本报告；
- 本地不入库：`tools/verify.js` 断言、`tools/FEEDBACK.md`、`docs/INDEX.md`、`docs/backlog/BACKLOG.md`（`*.md` 全局忽略，docs/releases 例外）；
- 知识库指针：`docs/notes/REPORT-MASTERMODE-SAVE.md`（大师档调研定型报告）。
