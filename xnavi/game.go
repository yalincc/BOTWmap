// game.go — 游戏适配层接口。
//
// 平台负责"端序解码 + 内存访问"，游戏负责"坐标语义"：
//   坐标是否可信、展示序、地图换算、位面（layer）、神庙/特殊场景钩子、
//   存档解析、已知偏移。
//
// xnavi 只服务 BOTW（TOTK 已于 2026-10-02 拆分到 TOTKmap/live-go 独立工程，
// 见交接文档：BOTW=固定偏移秒锁、TOTK=扫描+启发式，两套策略冲突是 v2.2.x
// 定位不稳的结构性根源）。接口保留单实现 game_botw.go。

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
	// Name 游戏标识："botw"（用于 known 文件命名、日志、契约附加字段）。
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
	// LayerOf 展示序 → 位面号（BOTW 恒 0）。
	LayerOf(gx, gy, gz float32) int
	// ValidRaw 第二道坐标校验（兼容现有端序相关参数序差异；DecodeAt 已校验过一道）。
	ValidRaw(mem []float32, littleEndian bool) bool

	// KnownOffsets 该游戏在指定平台上的已知固定偏移（相对锚块基址，按优先级排序）。
	// 启动时优先用这些偏移直接读，失败再回退全量扫描。
	KnownOffsets(p Platform) []uintptr

	// FixedNeedsMoveConfirm 固定偏移锁定是否需要"移动确认"才算 verified。
	// BOTW：偏移经多次重启样本验证（player = block_base + 偏移恒成立），直接
	// verified（秒锁不降级）。
	FixedNeedsMoveConfirm() bool

	// PreferScanOnUnlock 解锁后是否优先"全量扫描 + 探针选活组"（固定偏移退为扫描失败备胎）。
	// BOTW=false：偏移已验证，秒锁优先。
	PreferScanOnUnlock() bool

	// UnverifiedRescanAfter 未验证锁定超过该时长强制重扫（0=禁用）。
	// BOTW=15s：固定偏移锁错时快速自愈。
	UnverifiedRescanAfter() time.Duration

	// FrozenRescanAfter 已验证锁读数冻结多久后触发"共识兜底/重扫"。
	// BOTW=30s（live-go 原口径）。
	FrozenRescanAfter() time.Duration

	// PickOffset 从固定偏移候选（已通过 ValidRaw）仲裁出要锁定的候选。
	// BOTW：存档锚点距离最近（单偏移多块镜像，距离仅辅助确认）。
	// detail 为日志后缀（如 "dist=9m"）；ok=false 时调用方回退扫描。
	PickOffset(cands []OffsetCand, p Platform) (OffsetCand, string, bool)

	// 神庙/特殊场景钩子（BOTW）。
	ShrineExit() float32
	MatchShrine(gx, gy, gz float32) *ProgressPoint
	ShrineReturn() (window time.Duration, dist float32)

	// SaveAnchors 该游戏在指定平台上的存档锚点（内存序三元组，主档在前）。
	SaveAnchors(p Platform) []*SaveAnchor
	// SaveFileNames 该游戏的存档文件名列表（存档发现用，如 ["game_data.sav"]）。
	SaveFileNames() []string
}
