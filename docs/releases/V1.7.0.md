# BOTWmap V1.7.0 版本报告（2026-10-19）

## 1. 版本概述
- **目标**：打造完整全链路中英双语国际化体系（i18n），实现全站 UI、Canvas 地名注记、所有详情卡片（装备/任务/宝箱/营地/Boss）与 152 个任务攻略纯英文化；新增「大师模式专属怪（空中八爪怪等）」行为开关与 765 个浮空点位；彻底根治卡片闪烁及左上角定位异常。
- **最终结论**：**已正式发布**。全量自动化测试门禁由 164 项跃升至 **201 项（201 passed, 0 failed，100% 全绿）**。

## 2. 发布内容
- **全链路双语国际化体系（i18n）**：
  - **原生轻量引擎（`app/js/i18n.js`）**：零外部依赖，提供 `t()`, `nameOf()`, `descOf()`, `regionOf()`, `towerOf()`, `trialOf()`，状态持久化到 `localStorage`；
  - **顶栏规范命名**：英文主标题定名为 **`BOTW · Master Interactive Map`**；
  - **左侧栏与操作按钮**：侧栏 7 大主分类、图层列表、搜索框、标记设置（Settings）、探索度折叠标题（`▾ Progress` / `▾ 探索度`）及存档加载按钮（`📂 Load Save Progress…`）全链路双语联动；
  - **地名 Canvas 注记**：全 539 个区域地名补齐规范英文名，Canvas 绘制智能双语自适应切换；
  - **全量 152 个任务卡片深度英文化**：
    - 引入专用英文化数据包 `app/data/i18n/quests_en.js`，包含全量 152 个任务（主线/神庙/支线/DLC）地道专业的纯英文 `desc`（要点攻略）与 `note`（避坑提示）；
    - 补齐 118 位唯一 NPC 官方英文名字典（`NPC_EN`）；
    - 补齐 83 种任务奖励英文清洗转换（`REWARD_EN`）；
    - 地名 `translateLocation()` 覆盖复苏神庙、初始之塔等关键地标。
  - **装备/宝箱/营地/Boss 卡片深度英文化**：
    - 97 种武器/防具/盾牌、Atk/Def/Durability、单手剑/弓等类型英文化；
    - 宝箱类型、冰箭数量、营地按档掉落武器英文化；
    - Lynel #23 营地掉落人工实测说明双语化；
    - 统计面板 Subjugation 3 大 Boss 规范输出英文（`Hinox` / `Stone Talus` / `Molduga`）。
- **大师模式专属怪与浮空平台图层**：
  - 「⚙️地图设置」中新增 **「大师模式专属怪（空中八爪怪等） / Master Mode Spawns (Sky Octoroks etc.)」** 独立行为开关（隐藏/显示）；
  - 开启后，底层数据源 `hardmode.js` 中的 **609 只空中八爪怪（Enemy_AirOctarock）** 及浮空木平台上的波克布林/蜥蜴战士/巡逻法师等 765 个点位动态注入地图小怪图层；
  - 支持状态持久化与重置复位。
- **交互与定位避让内核根治**：
  - 彻底斩断新卡因带 `.hidden` 类名被 `CardLayout.placeAll()` 误滤导致掉落屏幕左上角 `(0, 0)` 的缺陷；
  - `positionCard()` 优先以目标标记在画布中的实时屏幕投影坐标 `map.m2s(currentMarker.px)` 作为避让锚点，卡片紧贴标记展开并不遮挡；
  - 设置面板空白区域及头部支持鼠标自由拖拽移动。

## 3. 技术架构
- **i18n 微型架构**：
  - `app/data/i18n/zh.js` & `en.js`：核心 UI 字典；
  - `app/data/i18n/quests_en.js`：全量 152 任务要点与提示专业英文化字典（从历史 en_msbt 与官方术语提取生成）；
  - `app/js/i18n.js`：两级 fallback 防御机制，绝不返回 undefined，绝不报错崩溃；
  - `UI.onLangChange(lang)`：事件驱动全站 DOM、图层列表、统计面板、已打开卡片、地图 Canvas 注记重绘。
- **大师模式怪状态分流**：
  - `app/js/enemy-tiers.js` 中 `composeMarkers()` 依据 `dlcState().hardmode` 按需追加 `hardmodeData.markers`；
  - 与 DLC2 一击剑任务怪并列为两大独立行为开关。

## 4. 测试与质量保障
- 自动化端到端测试（`tools/verify.js`）：**201 passed, 0 failed（100% 全绿）**；
- 覆盖视口拖拽、移动端响应式、缩放算法、DLC 开关、大师模式怪 609 只浮空八爪怪注入、全卡片类型英文优先、任务卡 Summary & Note 全英文断言、Boss 英文名、避让锚点投影等。

## 5. 回溯
- **Git Tag**：`v1.7.0`
- **代码基线**：Playwright 201 项全绿

