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
	"sync"
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

// matchShrine 跳变目标（展示序 gx, alt, gz）是否落在某**固定传送点**
// （神庙入口 / 希卡塔）的像素半径内（手册 §4.5.1 传送目的地锚定）。
//
// 用途边界（别再混用）：
//   - 这里只服务"传送目的地是否可信 + 打印目的地名"；
//   - **进庙判定不看它**（庙内是本地尺度坐标，换算成地图像素会落在海拉鲁中心，
//     几乎不可能命中图标 —— v3.1.3 教训 / 台账 #24）。进庙只看坐标尺度，见 watch.go。
func matchShrine(gx, alt, gz float32) *ProgressPoint {
	if progress == nil {
		return nil
	}
	px, py := gameToMap(gx, alt, gz)
	progress.mu.RLock()
	defer progress.mu.RUnlock()
	var best *ProgressPoint
	bestD := float32(1e18)
	for i := range progress.refs {
		p := &progress.refs[i]
		if p.T != "shrine" && p.T != "tower" {
			continue
		}
		dx, dy := px-float32(p.X), py-float32(p.Y)
		d := dx*dx + dy*dy
		if d < gameMatchShrineRadius*gameMatchShrineRadius && d < bestD {
			best, bestD = p, d
		}
	}
	return best
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
//   - Eden: 空（无固定偏移——金手指链 0x02D1EA00 在 Eden 确认不可用，
//     定位走 platform_eden.go 锚点窗口扫描 + 绝对地址缓存，见开发计划阶段 0）
func (g *gameBotw) KnownOffsets(p Platform) []uintptr {
	if p == nil {
		return []uintptr{0xA77FCBBC}
	}
	switch p.Name() {
	case "Cemu":
		return []uintptr{0x1055300C, 0xC1F8BF4}
	case "Eden":
		return nil
	default:
		return []uintptr{0xA77FCBBC}
	}
}

// FixedNeedsMoveConfirm BOTW 偏移已跨重启验证，保持秒锁 + 直接 verified（不改已验证行为）。
func (g *gameBotw) FixedNeedsMoveConfirm() bool { return false }

// PreferScanOnUnlock BOTW 偏移已验证，解锁后固定偏移优先秒锁。
func (g *gameBotw) PreferScanOnUnlock() bool { return false }

// UnverifiedRescanAfter BOTW 按平台：
//   - Cemu/Ryujinx=15s（原行为）：扫描兜底锁错概率高，15s 快速自愈。
//   - Eden=0（禁用）：Eden 扫描锁信任——站立不动是常态（挂机），15s unverified
//     重扫会导致每 15s 重扫闪红点 + 无限循环。锁错自愈：玩家开始动 → 探针
//     （锁定后 10s 窗口）换到移动中的组并 verified；探针已关 + 锁错 → 读数
//     不变 → FrozenRescanAfter=30s 冻结检测 → bgScan 全量重扫换活槽。
func (g *gameBotw) UnverifiedRescanAfter() time.Duration {
	if p := currentPlatform(); p != nil && p.Name() == "Eden" {
		return 0
	}
	return 15 * time.Second
}

// FrozenRescanAfter BOTW 按平台：
//   - Cemu/Ryujinx=0（禁用）：固定偏移是跨重启验证的活副本，游戏运行期
//     不会失效；站立不动=副本无写入属正常，30s 冻结阈值曾造成"站立期间反复后台
//     重扫"（Q13）。真异常（块切换读数错乱）由读数失效路径处理。
//   - Eden=12s：Eden 坐标槽会话内稳定、场景切换漂移——User Heap 扫描仅 0.9s，
//     停滞 12s 即可启动轻量级后台重扫自愈换活槽。
func (g *gameBotw) FrozenRescanAfter() time.Duration {
	if p := currentPlatform(); p != nil && p.Name() == "Eden" {
		return 12 * time.Second
	}
	return 0
}

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

// anchorSkipLogged 空档过滤日志去重：SaveAnchors 会被 EdenLocate / edenShrinePos /
// EdenSaveKnown 多次调用，同一个空档只报一次，避免日志刷屏。
var anchorSkipLogged sync.Map

// logAnchorSkip 报告被排除的空档/新档（#25 判读入口：一眼看出锚点为什么是废的）。
func logAnchorSkip(path string, playtime uint32, dones int) {
	if _, seen := anchorSkipLogged.LoadOrStore(path, true); seen {
		return
	}
	fmt.Printf("  [anchor] skip empty/new save slot: %s (playtime=%ds, shrines=%d)\n",
		path, playtime, dones)
}

// ---- 存档新鲜度提示（2026-10-20 15:40，纯日志引导，不改任何判据）----
//
// 背景（台账 #40 / §〇.9）：冷启动首次定位慢的根因是"初始静态锁经常不是玩家"，
// 而**站桩时玩家本身不产生任何"活组"信号**，程序只能挑"离锚点最近的 struct>0 组"，
// 挑中的可能是 47~100m 外的静态物（石头/箱子/桥/守护者残骸）。
//
// 老大实测：进游戏先手工存档 → 站桩不动 → 等需要移动验证时再动。
// 存档 mtime 新鲜时，锚点≈玩家真实位置，"离锚点最近"这个启发式就变得可信。
// 所以本函数只做一件事：**把存档新鲜度告诉用户**，让"先存档"从口头约定变成程序引导。
//
// 阈值取 2 分钟：老大测试习惯是"进游戏先手工存档再测"，2 分钟内的新鲜存档
// 锚点基本等于玩家位置；超过则锚点可能已过时（传送/跑远），提示重新存档。
const saveFreshWindow = 2 * time.Minute

// saveFreshLogged 去重：SaveAnchors 会被 EdenLocate / edenShrinePos / EdenSaveKnown
// 多次调用，同一个存档只提示一次，避免刷屏。
var saveFreshLogged sync.Map

// logSaveFreshness 报告存档新鲜度：过旧则提示用户重新手工存档。
func logSaveFreshness(a *SaveAnchor) {
	if a == nil || a.Path == "" {
		return
	}
	fi, err := os.Stat(a.Path)
	if err != nil {
		return
	}
	age := time.Since(fi.ModTime())
	if age <= 0 {
		return
	}
	key := a.Path
	if _, seen := saveFreshLogged.LoadOrStore(key, true); seen {
		return
	}
	if age > saveFreshWindow {
		fmt.Printf("  [save] 存档已 %.0f 分钟前写入，建议重新手工存档（新鲜存档能让首次定位快很多）\n",
			age.Minutes())
		return
	}
	fmt.Printf("  [save] 存档新鲜（%.0f 秒前写入）—— 锚点≈玩家位置，首次定位会快很多\n",
		age.Seconds())
}

// SaveAnchors = 原 savefile.go readSaveAnchors 逻辑原样平移。
// 主档策略：Cemu/Eden=最新 mtime 槽（BotW 轮转写 6 槽，槽号固定不可靠，
// 按 mtime 取最新，live-cemu 与 live-eden 双端实测）；Ryujinx=最高 playtime。
// Cemu/Eden 选档时额外排除空档/新档（#25），见下方注释。
func (g *gameBotw) SaveAnchors(p Platform) []*SaveAnchor {
	files := allSaveFiles(p)
	switch p.Name() {
	case "Cemu", "Eden":
		// #25 空档陷阱（2026-10-20 05:19 实测）：不能只按 mtime 取"最新"——
		// 6 个轮转槽里可能混着空档/新档（日志 `done: shrines 0/136`），其坐标是
		// 初始位置（实测锚点 (-1026.5, 252.3, 1803.2)，玩家在 5000 米外的阿卡莱），
		// 拿它当扫描中心会让整轮定位全废（±120m 只命中 391 条 → 全堆兜底锁伪坐标）。
		// 选档策略（按 mtime 降序遍历，命中即停，只解析到答案为止）：
		//   ① 最新档里 Playtime>0 且神庙完成数>0 的第一个 —— 正常在玩的档；
		//   ② 兜底：最新档里 Playtime>0 的第一个（刚出初始台地、还没通关神庙）；
		//   ③ 极端兜底：全部是空档 → 沿用旧行为（按 mtime 最新），不降低老下限。
		type saveCand struct {
			path string
			mod  time.Time
		}
		var cands []saveCand
		for _, f := range files {
			if fi, err := os.Stat(f); err == nil {
				cands = append(cands, saveCand{f, fi.ModTime()})
			}
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].mod.After(cands[j].mod) })
		var strong, played *SaveAnchor
		for _, c := range cands {
			entries, a := parseGameData(c.path)
			if a == nil {
				continue
			}
			dones := shrineDoneCount(entries)
			if a.Playtime == 0 || dones == 0 {
				logAnchorSkip(c.path, a.Playtime, dones)
			}
			if a.Playtime == 0 {
				continue
			}
			if played == nil {
				played = a
			}
			if dones > 0 {
				strong = a
				break
			}
		}
		if strong != nil {
			logSaveFreshness(strong)
			return []*SaveAnchor{strong}
		}
		if played != nil {
			logSaveFreshness(played)
			return []*SaveAnchor{played}
		}
		for _, c := range cands {
			if _, a := parseGameData(c.path); a != nil {
				return []*SaveAnchor{a}
			}
		}
		return nil
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

// gameIsMountState 检查原始 12 字节是否满足 BOTW 骑乘（马/大师摩托车）特征标志。
// 实测数据（Ryujinx 实机真机验证）：
// 林克上马/上摩托后，Transform 槽前 8 字节复用为标志位：X=0.0, Y=0x00000001 (IsRiding=1)，
// 只有 Z 轴保留合法游戏世界坐标（与坐骑真实 Z 轴差值 < 1.0m）。
func gameIsMountState(raw []byte, littleEndian bool) (bool, float32) {
	if len(raw) < 12 {
		return false, 0
	}
	var u0, u1, u2 uint32
	if littleEndian {
		u0 = uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24
		u1 = uint32(raw[4]) | uint32(raw[5])<<8 | uint32(raw[6])<<16 | uint32(raw[7])<<24
		u2 = uint32(raw[8]) | uint32(raw[9])<<8 | uint32(raw[10])<<16 | uint32(raw[11])<<24
	} else {
		u0 = uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
		u1 = uint32(raw[4])<<24 | uint32(raw[5])<<16 | uint32(raw[6])<<8 | uint32(raw[7])
		u2 = uint32(raw[8])<<24 | uint32(raw[9])<<16 | uint32(raw[10])<<8 | uint32(raw[11])
	}
	// X 为 0 且 Y 为 1（IsRiding 标志）
	if u0 == 0 && u1 == 1 {
		z := math.Float32frombits(u2)
		if math.Abs(float64(z)) > 10.0 && z > gameMinZ && z < gameMaxZ && !math.IsNaN(float64(z)) {
			return true, z
		}
		return true, 0 // 骑乘标志有效，但 Z 轴未更新或为 0
	}
	return false, 0
}

func checkMountState(h uintptr, addr uintptr) (bool, float32) {
	plat := currentPlatform()
	if plat == nil {
		return false, 0
	}
	raw := readMem(h, addr, 12)
	return gameIsMountState(raw, plat.LittleEndian())
}
