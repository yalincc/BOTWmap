// game.go — 游戏适配层接口。
//
// 平台负责"端序解码 + 内存访问"，游戏负责"坐标语义"：
//   坐标是否可信、展示序、地图换算、位面（layer）、神庙/特殊场景钩子、
//   存档解析、已知偏移。
//
// 设计依据：ds 文档五层架构"游戏语义层"；Xnavi V2.2.0 双游戏合并开发计划。
// 接入新游戏 = 新增 game_xxx.go 实现本接口（game_totk.go 见 P2）。
// 红线：BOTW 实现（game_botw.go）只做提取平移，不改变任何已验证行为。

package main

import "time"

// OffsetCand 固定偏移试读后通过校验的候选（展示序 (gx, gy, gz)）。
type OffsetCand struct {
	Addr uintptr
	Pos  [3]float32
}

// Game 描述一个游戏的定位语义。实现必须只读、不注入、不修改游戏内存。
// 同一时刻只有一个游戏生效（启动时由 --game 确定，切换需重启程序）。
type Game interface {
	// Name 游戏标识："botw" / "totk"（用于 known 文件命名、日志、契约附加字段）。
	Name() string
	// MapURL 打开的地图地址。
	MapURL() string

	// DecodeAt 读取 addr 处 12 字节并解码为"平台内存三元组"（平台端序已解、
	// 已做第一道坐标校验），无效返回 nil。BOTW 实现 = 原 decodeTripleAt 逻辑。
	DecodeAt(h uintptr, addr uintptr) []float32
	// Display 平台内存三元组 → 展示序 (gx, gy, gz)（直接写入 /pos 的口径）。
	Display(mem [3]float32) (gx, gy, gz float32)
	// ToMap 展示序 → 地图像素 (mx, my)。
	ToMap(gx, gy, gz float32) (mx, my float32)
	// LayerOf 展示序 → 位面号（BOTW 恒 0；TOTK 18 地面/19 地底/20 天空）。
	LayerOf(gx, gy, gz float32) int
	// ValidRaw 第二道坐标校验（兼容现有端序相关参数序差异；DecodeAt 已校验过一道）。
	ValidRaw(mem []float32, littleEndian bool) bool

	// KnownOffsets 该游戏在指定平台上的已知固定偏移（相对锚块基址，按优先级排序）。
	// 启动时优先用这些偏移直接读，失败再回退全量扫描。
	KnownOffsets(p Platform) []uintptr

	// PickOffset 从固定偏移候选（已通过 ValidRaw）仲裁出要锁定的候选。
	// 策略因游戏而异：
	//   - BOTW：存档锚点距离最近（单偏移多块镜像，距离仅辅助确认）
	//   - TOTK：多副本共识（多个偏移读到几乎相同坐标才算数；无共识拒绝锁定，
	//     避免把 NPC/静态数据等杂散合法三元组锁成玩家——live-go tryOffsets 同口径）
	// detail 为日志后缀（如 "dist=9m" / "consensus=3 copies"）；ok=false 时调用方回退扫描。
	PickOffset(cands []OffsetCand, p Platform) (OffsetCand, string, bool)

	// 神庙/特殊场景钩子（BOTW 用；TOTK 返回零值/禁用）。
	ShrineExit() float32
	MatchShrine(gx, gy, gz float32) *ProgressPoint
	ShrineReturn() (window time.Duration, dist float32)

	// SaveAnchors 该游戏在指定平台上的存档锚点（内存序三元组，主档在前）。
	SaveAnchors(p Platform) []*SaveAnchor
	// SaveFileNames 该游戏的存档文件名列表（存档发现用，如 ["game_data.sav"]）。
	SaveFileNames() []string
}
