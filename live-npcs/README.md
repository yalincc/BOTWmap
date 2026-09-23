# live-npcs：实体探测器（NPC 跟踪）

独立模块：读取 Ryujinx 内存，通过 **actor 系统逆向**（ActorInfo 名字对象 hash → 实例对象坐标）只跟踪带名字的 NPC（`Npc_` 前缀，不含动物/敌人），
输出名字 + 坐标 / 地图像素 / moving，供浮窗（FloatNavi）叠加显示。

**模块化**：独立于 `live-go/`（BotwNavi）与 `app/`（网页地图），可随时删除。
不启动 `live-npcs.exe` / 删除本目录即完全回退，现有导航/地图零影响。

## 启动

1. 启动 Ryujinx 并进入游戏（读档）
2. 以管理员身份运行 `live-npcs.exe`（读 Ryujinx 内存需管理员权限）

## 用法

```
live-npcs.exe            # 服务模式：HTTP API :8778，每 ~3s 刷新 NPC 列表
live-npcs.exe --probeafc # 探查：正式实例定位器（一键验证当前场景 NPC）
live-npcs.exe 8780       # 换端口
```

## API

```
GET http://127.0.0.1:8778/npcs
{
  "ok": true,
  "updated": "00:34:31",
  "player": { "X": 3397.4, "Z": 2120.5, "MX": 18795, "MY": 14241, "moving": false },
  "list": [
    { "name": "Npc_HatenoVillage022", "X": 3397.4, "Z": 2125.7, "MX": 18795, "MY": 14251, "moving": false },
    ...
  ]
}
```

- `name`：NPC 名字（如 `Npc_HatenoVillage022`）；`X/Y/Z`：游戏坐标（米）；`MX/MY`：地图像素
- `moving`：跨帧位移 >0.5m 判定为移动中
- 只返回玩家 ±500m 内的 NPC（按名字去重取最近实例）

## 数据源原理（M3' 逆向成果）

```
Ryujinx guest RAM (0x016B 运行时区, 已提交 224MB)
  │ 1) 扫 Npc_ 名字字符串 → ActorInfo（+0x4C = actor hash，0x344xxxxx）
  │ 2) 4B 反查 hash → 实例引用点（组件/未激活对象会被结构判别过滤）
  │ 3) 引用点 +0x80/+0x84/+0x88 = 世界坐标 XYZ（4x4 变换矩阵平移列）
  ▼
HTTP API :8778 /npcs → {list:[{name,x,y,z,moving}]}
  ▼
FloatNavi 浮窗 → 地图叠加 NPC 点 + 名字标签
```

- 只收 `Npc_` 前缀 → 无动物/敌人/假阳性
- 固定扫 0x016B12000000~0x016B20000000（已提交 224MB），不依赖玩家槽锚点（玩家槽偶发漂移会抖）
- ~2s/帧 + 3s 轮询：游戏不卡（旧启发式全量 6GB 扫描 2~3s/帧是卡顿根源，已停用）

## 里程碑状态

| 里程碑 | 状态 |
|---|---|
| M1 探测（扫描+报告） | ✅ 已验收（2026-09-23） |
| M2 服务（:8778 API） | ✅ 已验收（2026-09-23） |
| M3 浮窗渲染（启发式红点） | ✅ 已实机验证（2026-09-23），后被老大否决（闪烁/假阳性/卡顿） |
| **M3' 换思路（NPC hash→实例，带名字）** | ✅ **已实机验收（2026-09-23）：Hateno 村 16 个 NPC 全部命中，坐标 5~74m 合理，无假阳性，不卡** |
| M4 类型分色 | ⏳ 后置（现只跟踪 NPC，暂不需要） |
