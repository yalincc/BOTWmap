# tasks/manual — 任务内容底表（每题一 YAML）

> 本目录是 **BOTWmap 任务数据的唯一人工编辑入口**（源数据）。
> 生成物 `app/data/quests.js` 由 `source/build_quests_v2.py` 单向生成，**禁止手改**。
> 结构仿 TOTKmap 的 `tasks/manual/*.yaml`。

## 为什么用 YAML

旧做法把任务全塞进 `app/data/quests.js` 单文件（机器生成的机器格式）——改一条任务会污染整文件、git diff 看不清、官方原文里还夹着 `\x0e` 截断符、note 常年为空。改成每题一 YAML 后：改一条只动一个文件、字段人可读、内容与 romfs 权威数据分层。

## 目录

```
tasks/manual/
├── main/    主线（含四神兽，语义 key，如 main_water_relic.yml）
├── shrine/  神庙挑战 42
└── side/    支线 90
```

## 每条 YAML 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `key` | ✅ | 稳定 id，与文件名一致；`main_01` / `main_water_relic` / `shrine_17` / `side_60` |
| `cat` | ✅ | `main` / `shrine` / `side` |
| `name` / `en` | ✅ | 中文名 / 英文名 |
| `region` | ✅ | 地区 |
| `flag` |  | 游戏内存档完成标志（官方 `<任务>_Finish(ed)`）；空 = 无独立标志 |
| `dlc` |  | `null` 本体 / `"1"` DLC1 / `"2"` DLC2 / `"collabo"` 联动 |
| `npc` |  | 接取 NPC 中文名 |
| `gx` / `gz` |  | 任务主坐标（游戏世界坐标）；`pts` 缺省时由它换算地图像素 |
| `desc` |  | 任务背景/目标（1-2 句） |
| `note` |  | 攻略要点（多行，`\|` 保留换行） |
| `reward` |  | 奖励 |
| `steps` |  | 流程步骤（字符串数组） |
| `flowPts` |  | 结构化流程点（连线的骨架），见下 |
| `reqs` / `unlockList` |  | 前置 / 后续（`[{key, name}]`）；无则 `[]` |

### flowPts 流程点

```yaml
flowPts:
  - {type: take,    gx: 3338, gz: -551, note: 卓拉领地·与希多接取}
  - {type: npc,     gx: 3595, gz: -342, note: 卓拉王座·多莱凡国王}
  - {type: battle,  gx: 3662, gz: -175, note: 水之神兽·水咒盖侬}
```

`type` ∈ `take`（接取）/ `deliver`（交付完成）/ `battle`（战斗）/ `npc`（关键 NPC）/ `other`（其他关键位置）。`note` = 人可读标签，卡片与连线显示用。

## 生成规则（build_quests_v2.py）

1. 以 romfs 权威骨架 `BOTWroms/raw/romfs/quest-data-complete.json` 为底。
2. 同 `key` 的 YAML 字段覆盖骨架（渐进迁移：未写 YAML 的任务仍用骨架，功能不受影响）。
3. 自动派生：`no`/`groupTotal`（档内编号）、`pts`（缺省由 `gx/gz` 换算）、`tier`（≥2 流程点=L1 / 单点=L2 / 无点=L3）。
4. 校验：条数、坐标换算、reqs 引用、flag 覆盖。

## 用法

```bash
# 校对前先跑校验器（报错会告诉你是哪条哪个字段）
C:/Users/Administrator/.workbuddy/binaries/python/envs/default/Scripts/python.exe source/validate_quests.py --check-generated
#   --detail            逐条打印警告明细
#   --check-generated   顺带查 app/data/quests.js 是否已与 YAML 同步
#   --cat shrine        只看某一档

# 改完 YAML → 重新生成
C:/Users/Administrator/.workbuddy/binaries/python/envs/default/Scripts/python.exe source/build_quests_v2.py
```

改一条任务 → 改它的 `.yml` → 跑校验器 → 重跑 build → `app/data/quests.js` 更新。记得 bump `app/index.html` 里 `quests.js?v=` 与版本号。

## 校对清单（V1.6.3 体检结果 · 152 条）

跑 `validate_quests.py` 的当前结论：**错误 1 条，警告 345 条**。真正值得动手的只有下面两组。

### 要补的 key（按优先级）

| 优先级 | key | 缺口 | 影响 |
|---|---|---|---|
| P0 | `region` | **side_148** 空（唯一硬错误） | 卡片不显示区域行 |
| P0 | `flag` | **89/152 空**：shrine 31 / side 56 / main 2 | 这些任务**无法与存档自动同步完成态**，只能手动标记 |
| P2 | `flowPts.actor` | 140 条任务含英文 id、77 条任务为空 | 卡片当前不显示 actor，可后置 |
| P2 | `reward` | 38 条含多个奖励 | 若日后要拆数组再处理 |
| P2 | `gx`/`gz` | 仅 side_148 为 `null` | 地图无点，只能从搜索进入；可加 `onMap: false` 显式标记 |

### 自动校对已通过（0 问题，不用人看）

跑 `validate_quests.py` 会自动查，目前全部为 0：

- **前后置对称** 0 处：A 的 `unlockList` 写了 B，B 的 `reqs` 就一定有 A
- **控制字符** 0 处：`name`/`desc`/`note`/`steps` 里没有 romfs 原文残留的 `\x0e` 截断符
- **坐标越界** 0 处：主坐标与全部 354 个流程点都在 24000×20000 画布内
- **region 与坐标一致** 0 处：按最近 r1 大区判定，填的区域都对得上
- **`name` / `en` 异常** 0 处：不空、不过长
- **`reqs` / `unlockList` 悬空** 0 处：没有指向不存在任务的 key

### ⚠️ 一个命名孤例（要你拍板，不是数据错）

`app/js/regions.js` 的 r1/r2 大区里写作 **「哈特尔」**（哈特尔地区 / 东哈特尔 / 哈特尔海 / 西哈特尔，共 4 处），
而全项目主流译名是 **「内卡鲁达」**（`app/js/data.js` 357 次、任务数据 45 次，从不出现「哈特尔」）。

后果：地图大区标注显示「哈特尔地区」，任务卡「区域」行显示「内卡鲁达」，同一个地方两个名字。
要么把 `regions.js` 那 4 处改成「内卡鲁达」，要么反过来——**我倾向改 regions.js 那 4 处**（少数服从多数，且 data.js 是权威大表）。

### 与 TOTKmap 的 key 差异（对齐结论）

对照 `TOTKmap/app/data/task-plan.js`（253 条）与模板设计稿 v4：

| TOTKmap key | BOTWmap 现状 | 结论 |
|---|---|---|
| `npc`（英文 id）+ `npcCn`（中文名） | 只有 `npc`，且**全是中文** | 相当于已有 `npcCn`；缺的英文 id 卡片不显示，**不必补** |
| `loc` + `locCn`（地点中文名） | `region`（区域）+ `flowPts.note`（地名·动作） | 覆盖够了，**不必补** |
| `guide.start` / `guide.deliver` | 无独立字段，内容在 `steps[]` | steps 中 132/152 有接取线索、37/152 有交付线索；拆独立字段收益不大，**暂不拆** |
| `guide.reward`（数组） | `reward` 字符串 | 38 条多奖励，需要时再拆 |
| `posValid` / `onMap` / `noPlaceReason` | 无 | 只有 side_148 一条无点，加个 `onMap: false` 即可 |
| `raw.*`（攻略原文三字段） | 无 | 需要保留官方原文时再补 |

> 结论：对齐 TOTKmap 后，**BOTWmap 真正缺的不是字段数量，而是 `flag` 覆盖率**（58% 无标志）。
> 手工校对应先攻 `flag`，而不是逐条重看文案。
