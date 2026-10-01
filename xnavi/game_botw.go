// game_botw.go — 游戏层实现（BOTW）。
//
// 内容 = 原 game.go 全部函数原样平移（红线：行为零改动），
// 实现 Game 接口。平台只负责"端序解码 + 内存访问"，
// 坐标是否可信、展示顺序、地图换算、位面（layer）这些是"游戏语义"。
//
// 本文件坐标口径（与 live-go/live-cemu 一致）：
//   内存序 = (X 东, alt 高, Z 南+)
//   展示序 = (gx=X, gz=Z, alt)  —— state.gx/gy/gz 中的 gy 即高度
//
// 注意：BOTW 两平台解码路径的"平台内存三元组"轴序不同
//   （Ryujinx 内存序 X/Z/alt、Cemu 内存序 X/alt/Z），
//   已由 DecodeAt 按端序各自归一，Display 统一输出 (X, alt, Z) 展示序——
//   只提取、不"修正"，避免破坏已验证行为。

package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// abs32 float32 绝对值。
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// ---- 坐标校验（合并 live-go decodePos 与 live-cemu posRangeOK 的判据）----

const (
	gameMinX, gameMaxX         = -7000.0, 7000.0
	gameMinZ, gameMaxZ         = -7000.0, 7000.0
	gameMinAlt, gameMaxAlt     = -600.0, 5000.0
	gameMatchShrineRadius      = 80.0     // 传送跳变目标与神庙坐标的像素匹配半径（px）
	gameShrineExit             = 300.0    // 神庙内部坐标上限：|gx|,|gz| 超此判定已离开神庙
	gameShrineReturnWindow     = 3 * 1000 // 毫秒：回跳撤销窗口 = 3s（与 live-cemu poll 3s 一致；
	// P3 曾误作微秒使用导致窗口实际为 3µs，2026-10-01 修正为真正的 3 秒）
	gameShrineReturnDist = 50.0 // 米 回跳撤销距离
)

// gameValidTriple 校验内存序三元组 (X, alt, Z) 是否为合法玩家坐标。
// 判据（两平台踩坑固化，勿删任何一条）：
//   - NaN 单独判（与任何数比较都是 false，范围检查抓不住）
//   - 全零 = 标题/加载画面槽未初始化
//   - 近零簇（原点残留）
//   - 单轴近零且高度贴地（<5）= 失效偏移常读到的按钮/未初始化槽
//   - 单轴 denormal 垃圾
//   - 地图范围
func gameValidTriple(x, alt, z float32) bool {
	if x != x || alt != alt || z != z {
		return false // NaN
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	if abs32(x) < 5 && abs32(z) < 5 {
		return false // 近零簇（原点残留）
	}
	if (abs32(x) < 5 && alt < 5) || (abs32(z) < 5 && alt < 5) {
		return false // 未初始化/按钮槽
	}
	if abs32(x) < 1e-20 || abs32(z) < 1e-20 {
		return false // 单轴 denormal
	}
	if x <= gameMinX || x >= gameMaxX || z <= gameMinZ || z >= gameMaxZ ||
		alt <= gameMinAlt || alt >= gameMaxAlt {
		return false
	}
	// BOTW 地图坐标在千位级，排除 (30,80,100) 这种引擎内部小向量。
	// Q20 修正：原为 |x|<100 || |z|<100（单轴即拒），导致传送/行走到地图中心带
	// （|x|<100，如 x=-28）的合法玩家坐标被当垃圾拒绝——固定偏移永远锁不上、
	// 解码循环死转。改为三轴都小才拒（引擎小向量三轴皆小，玩家位置 z 或 alt 必大）。
	if abs32(x) < 100 && abs32(z) < 100 && alt < 100 {
		return false
	}
	return true
}

// gameHud 校验并转换内存序 (X, alt, Z) → 展示序 (gx=X, gy=alt, gz=Z)。无效返回 nil。
// V2.2.0 P2 统一序：DecodeAt 直接返回展示序，Display 恒等（原先返回 (X,Z,alt) 由 Display 再交换）。
// gameTripleRejectReason 诊断用：解码 12 字节并返回被拒原因（Q17）。
// 传送/加载期页保护瞬时切换会造成 RPM 瞬时失败或撕裂读，此函数让日志直接可判。
func gameTripleRejectReason(raw []byte, littleEndian bool) string {
	if len(raw) < 12 {
		return "short-read"
	}
	var x, alt, z float32
	if littleEndian {
		a := floats(raw[:12])
		x, alt, z = a[0], a[1], a[2]
	} else {
		x = math.Float32frombits(uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]))
		alt = math.Float32frombits(uint32(raw[4])<<24 | uint32(raw[5])<<16 | uint32(raw[6])<<8 | uint32(raw[7]))
		z = math.Float32frombits(uint32(raw[8])<<24 | uint32(raw[9])<<16 | uint32(raw[10])<<8 | uint32(raw[11]))
	}
	switch {
	case x != x || alt != alt || z != z:
		return fmt.Sprintf("NaN (%g,%g,%g)", x, alt, z)
	case math.Abs(float64(x)) < 1e-20 || math.Abs(float64(z)) < 1e-20:
		return fmt.Sprintf("denormal (%g,%g,%g)", x, alt, z)
	case x <= gameMinX || x >= gameMaxX:
		return fmt.Sprintf("x out of bounds (%g,%g,%g)", x, alt, z)
	case z <= gameMinZ || z >= gameMaxZ:
		return fmt.Sprintf("z out of bounds (%g,%g,%g)", x, alt, z)
	case alt <= gameMinAlt || alt >= gameMaxAlt:
		return fmt.Sprintf("alt out of bounds (%g,%g,%g)", x, alt, z)
	default:
		return fmt.Sprintf("valid-looking (%g,%g,%g) — RPM 撕裂/瞬时失败嫌疑", x, alt, z)
	}
}

func gameHud(x, alt, z float32) []float32 {
	if !gameValidTriple(x, alt, z) {
		return nil
	}
	return []float32{x, alt, z}
}

// ---- 地图换算 ----

// gameToMap BOTW: 游戏坐标 → 地图像素（live-go poll 与 Cemu poll 同款换算）。
func gameToMap(gx, alt, gz float32) (mx, my float32) {
	return 2*gx + 12000, 2*gz + 10000
}

// gameLayer BOTW 无多层位面，恒 0。
const gameLayer = 0

// gameDist3 三维欧氏距离。
func gameDist3(a1, a2, a3, b1, b2, b3 float32) float32 {
	dx, dy, dz := a1-b1, a2-b2, a3-b3
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// matchShrine 跳变目标（展示序 gx, alt, gz）是否落在某神庙入口附近（像素坐标）。
func matchShrine(gx, alt, gz float32) *ProgressPoint {
	if progress == nil {
		return nil
	}
	px, py := gameToMap(gx, alt, gz)
	progress.mu.RLock()
	defer progress.mu.RUnlock()
	for i := range progress.refs {
		p := &progress.refs[i]
		if p.T != "shrine" {
			continue
		}
		dx, dy := px-float32(p.X), py-float32(p.Y)
		if dx*dx+dy*dy < gameMatchShrineRadius*gameMatchShrineRadius {
			return p
		}
	}
	return nil
}

// ---- Game 接口实现（BOTW）----

type gameBotw struct{}

func (g *gameBotw) Name() string    { return "botw" }
func (g *gameBotw) MapURL() string  { return "https://botw.yalin.site/" }

// DecodeAt = 原 decodeTripleAt（locate.go）逻辑原样平移：
// 按当前平台端序解码 12 字节 → gameHud 校验 → 返回展示序三元组 (X, alt, Z)。
func (g *gameBotw) DecodeAt(h uintptr, addr uintptr) []float32 {
	d := readMem(h, addr, 12)
	if len(d) < 12 {
		return nil
	}
	plat := currentPlatform()
	var x, alt, z float32
	if plat != nil && !plat.LittleEndian() {
		x = math.Float32frombits(uint32(d[0])<<24 | uint32(d[1])<<16 | uint32(d[2])<<8 | uint32(d[3]))
		alt = math.Float32frombits(uint32(d[4])<<24 | uint32(d[5])<<16 | uint32(d[6])<<8 | uint32(d[7]))
		z = math.Float32frombits(uint32(d[8])<<24 | uint32(d[9])<<16 | uint32(d[10])<<8 | uint32(d[11]))
	} else {
		a := floats(d[:12])
		x, alt, z = a[0], a[1], a[2]
	}
	return gameHud(x, alt, z)
}

// Display 恒等：DecodeAt 已返回展示序 (gx=X, gy=alt, gz=Z)。
func (g *gameBotw) Display(mem [3]float32) (gx, gy, gz float32) {
	return mem[0], mem[1], mem[2]
}

func (g *gameBotw) ToMap(gx, gy, gz float32) (mx, my float32) {
	return gameToMap(gx, gy, gz)
}

func (g *gameBotw) LayerOf(gx, gy, gz float32) int { return gameLayer }

// ValidRaw 第二道校验（固定偏移候选）：DecodeAt 已给展示序，统一按 (x, alt, z) 校验，
// 顺带修正原 Cemu 大端路径的 (mem0, mem1, mem2) 参数错位。
func (g *gameBotw) ValidRaw(mem []float32, littleEndian bool) bool {
	return gameValidTriple(mem[0], mem[1], mem[2])
}

// KnownOffsets BOTW 已知固定偏移（相对锚块基址），按平台：
//   - Cemu: 0x1055300C（社区公开针位 koko-yl/BotWRamWatch，Cemu 2.6 + JP v208 实测有效）
//           备用 0xC1F8BF4（navicemu 多块固定偏移试读的备选）
//   - Ryujinx: 0xA77FCBBC（5 次重启样本验证，player = block_base + 偏移恒成立）
func (g *gameBotw) KnownOffsets(p Platform) []uintptr {
	if p != nil && p.Name() == "Cemu" {
		return []uintptr{0x1055300C, 0xC1F8BF4}
	}
	return []uintptr{0xA77FCBBC}
}

// FixedNeedsMoveConfirm BOTW 偏移已跨重启验证，保持秒锁 + 直接 verified（不改已验证行为）。
func (g *gameBotw) FixedNeedsMoveConfirm() bool { return false }

// PreferScanOnUnlock BOTW 偏移已验证，解锁后固定偏移优先秒锁。
func (g *gameBotw) PreferScanOnUnlock() bool { return false }

// UnverifiedRescanAfter BOTW 保持 15s 快速自愈（原行为）。
func (g *gameBotw) UnverifiedRescanAfter() time.Duration { return 15 * time.Second }

// ChainCapable BOTW 不参与指针链模式（固定偏移已稳定，隔离红线：BOTW 路径零改动）。
func (g *gameBotw) ChainCapable() bool { return false }

// FrozenRescanAfter BOTW=0（禁用）：固定偏移是跨重启验证的活副本，游戏运行期
// 不会失效；站立不动=副本无写入属正常，30s 冻结阈值曾造成"站立期间反复后台
// 重扫"（Q13）。真异常（块切换读数错乱）由读数失效路径处理。
func (g *gameBotw) FrozenRescanAfter() time.Duration { return 0 }

func (g *gameBotw) ShrineExit() float32 { return gameShrineExit }

// PickOffset BOTW：存档锚点距离最近（单偏移多块镜像，距离仅辅助确认 + 日志）。
func (g *gameBotw) PickOffset(cands []OffsetCand, p Platform) (OffsetCand, string, bool) {
	best := cands[0]
	anchors := g.SaveAnchors(p)
	if len(anchors) > 0 {
		savePt := anchors[0].Pos
		bestDist := float32(1e9)
		for _, cd := range cands {
			ddx := cd.Pos[0] - savePt[0]
			ddalt := cd.Pos[1] - savePt[1]
			ddz := cd.Pos[2] - savePt[2]
			dd := float32(math.Sqrt(float64(ddx*ddx + ddalt*ddalt + ddz*ddz)))
			if dd < bestDist {
				bestDist = dd
				best = cd
			}
		}
		return best, fmt.Sprintf("dist=%.0fm [confirmed]", bestDist), true
	}
	return best, "[confirmed]", true
}

func (g *gameBotw) MatchShrine(gx, gy, gz float32) *ProgressPoint {
	return matchShrine(gx, gy, gz)
}

func (g *gameBotw) ShrineReturn() (time.Duration, float32) {
	// P3 单位修正：gameShrineReturnWindow 数值为毫秒，原样返回 = 3µs（bug），
	// 现在真正返回 3s——神庙回跳撤销窗口恢复设计行为（BOTW 神庙场景需回归测试）
	return gameShrineReturnWindow * time.Millisecond, gameShrineReturnDist
}

// SaveAnchors = 原 savefile.go readSaveAnchors 逻辑原样平移。
func (g *gameBotw) SaveAnchors(p Platform) []*SaveAnchor {
	files := allSaveFiles(p)
	switch p.Name() {
	case "Cemu":
		best, bestT := "", time.Time{}
		for _, f := range files {
			if fi, err := os.Stat(f); err == nil && fi.ModTime().After(bestT) {
				best, bestT = f, fi.ModTime()
			}
		}
		if best == "" {
			return nil
		}
		_, a := parseGameData(best)
		if a == nil {
			return nil
		}
		return []*SaveAnchor{a}
	default: // Ryujinx / 兜底
		var out []*SaveAnchor
		seen := map[[3]int32]int{}
		for _, f := range files {
			_, a := parseGameData(f)
			if a == nil {
				continue
			}
			key := [3]int32{int32(round1(a.Pos[0])), int32(round1(a.Pos[2])), int32(round1(a.Pos[1]))}
			if i, ok := seen[key]; ok {
				if a.Playtime > out[i].Playtime { // 同位置多个槽：保留游玩时间最高者
					out[i] = a
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, a)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Playtime > out[j].Playtime })
		return out
	}
}

func (g *gameBotw) SaveFileNames() []string { return []string{"game_data.sav"} }

// currentGame 当前生效的游戏（main 解析 --game 后设置；默认 BOTW）。
var currentGame Game = &gameBotw{}
