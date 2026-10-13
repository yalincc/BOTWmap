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
C:/Users/Administrator/.workbuddy/binaries/python/envs/default/Scripts/python.exe source/build_quests_v2.py
```

改一条任务 → 改它的 `.yml` → 重跑脚本 → `app/data/quests.js` 更新。记得 bump `app/index.html` 里 `quests.js?v=` 与版本号。
