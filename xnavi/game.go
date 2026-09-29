// game.go — 游戏层配置（BOTW）。
//
// 平台只负责"端序解码 + 内存访问"，坐标是否可信、展示顺序、地图换算、
// 位面（layer）这些是"游戏语义"，集中在这里。未来接入 TOTK 时新增
// game_totk.go，替换本文件的实现即可，状态机/扫描/服务端不用动。
//
// 本文件坐标口径（与 live-go/live-cemu 一致）：
//   内存序 = (X 东, alt 高, Z 南+)
//   展示序 = (gx=X, gz=Z, alt)  —— state.gx/gy/gz 中的 gy 即高度

package main

import "math"

// abs32 float32 绝对值。
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// ---- 坐标校验（合并 live-go decodePos 与 live-cemu posRangeOK 的判据）----

const (
	gameMinX, gameMaxX = -7000.0, 7000.0
	gameMinZ, gameMaxZ = -7000.0, 7000.0
	gameMinAlt, gameMaxAlt = -600.0, 5000.0
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
	// BOTW 地图坐标在千位级，排除 (30,80,100) 这种引擎内部小向量
	if abs32(x) < 100 || abs32(z) < 100 {
		return false
	}
	return true
}

// gameHud 校验并转换内存序 (X, alt, Z) → 展示序 (gx=X, gz=Z, alt)。无效返回 nil。
func gameHud(x, alt, z float32) []float32 {
	if !gameValidTriple(x, alt, z) {
		return nil
	}
	return []float32{x, z, alt}
}

// ---- 地图换算 ----

// gameToMap BOTW: 游戏坐标 → 地图像素（live-go poll 与 Cemu poll 同款换算）。
func gameToMap(gx, alt, gz float32) (mx, my float32) {
	return 2*gx + 12000, 2*gz + 10000
}

// gameLayer BOTW 无多层位面，恒 0。
const gameLayer = 0

// gameMatchShrineRadius 传送跳变目标与神庙坐标的像素匹配半径（px）。
const gameMatchShrineRadius = 80.0

// gameShrineExit 神庙内部坐标上限：|gx|,|gz| 超此判定已离开神庙。
const gameShrineExit = 300.0

// gameShrineReturnWindow 回跳撤销窗口：accept 跳变后 N 秒内读数回到跳变前
// 位置附近（<gameShrineReturnDist 米）→ 动画假传送（对话/克洛格等），回滚。
const (
	gameShrineReturnWindow = 3 * 1000 // ms（与 live-cemu poll 3s 一致）
	gameShrineReturnDist   = 50.0     // 米
)

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
