// game_botw.go — BOTW 游戏语义（Eden 版）：神庙判据、位面。
//
// BOTW 与 TOTK 在 Eden 下的游戏层差异集中在本文件（阶段二抽 Game 接口时
// 直接平移）：坐标轴序在 locate.go decodePos、存档在 savefile_botw.go、
// 进度在 progress_botw.go、地图口径在 watch.go state 更新处。

package main

import "time"

// botwShrineExit 神庙内部坐标上限：|X|,|Z| 超过此值 = 世界尺度，小于 = 神庙
// 内部（照 xnavi gameShrineExit=300 / live-cemu shrineExit=300，双端已验证）。
const botwShrineExit = 300.0

// isShrinePos 判定神庙/洞穴本地场景：BOTW 神庙为独立场景，玩家坐标切到以
// 神庙原点为中心的本地坐标系（|X|<300 && |Z|<300）。进入判定由状态机叠加
// "世界尺度单步跳变"条件（watch.go enteredShrine），避免海拉鲁中心原点带
// （|X|<300,|Z|<300 的合法世界坐标）被误判进神庙。
func isShrinePos(p [3]float32) bool {
	return abs32(p[0]) < botwShrineExit && abs32(p[2]) < botwShrineExit
}

// layerOf BOTW 无多层位面（地图是单层大地图），恒 0。
func layerOf(gx, gy, gz float32) int { return 0 }

// ---- BOTW 半死槽快速换锁（2026-10-07 诊断修复）----
// BOTW 坐标槽质量参差（Eden 实测）：稳定活槽持续更新；"半死槽"在玩家移动时
// 偶尔被写一次（能混过 3 拍移动确认），之后冻结/跳变（实测 x227：60s 完全
// 冻结 + 70m 跳变 → 红点乱跳/卡住）。TOTK 所有槽持续更新，无此问题，因此
// 本机制只对 BOTW 启用（阶段二抽 Game 接口时 TOTK 语义不接）。
// 判据：本锁读数冻结 ≥ botwFastFrozenTicks（5s），但 knownPool/known 其他
// 地址存在净位移 >moveNet 的活跃成员（= 玩家在动、本锁死了）→ 立即换锁到
// 活跃成员（不等 30s 共识）。玩家站桩时无活跃成员 → 永不误换。位置闸门
// botwFastNear：候选必须距本锁读数 50m 内（远处相机/NPC 微动槽拒绝）。
const (
	botwFastFrozenTicks = 25                      // 本锁冻结 5s 即检查（原共识兜底 30s）
	botwFastEvery       = 400 * time.Millisecond  // 世界活性采样周期
	botwFastMoveN       = 2                       // 活跃成员净位移采样次数 ≥ 此值 → 判活
	botwFastNear        = 50.0                    // 换锁候选距本锁读数上限（位置闸门）
	botwFastAddrCap     = 24                      // 最多观察的地址数（微秒级 12B 读）
	botwFastRelockSrc   = "botw-fast-relock"
)

// fastWatchState 快速换锁的跨 tick 观察状态（stateMachine 局部持有）。
type fastWatchState struct {
	On      bool
	At      time.Time
	Addrs   []uintptr
	Base    map[uintptr][3]float32
	Moved   []int
	AnyMove bool
}

// botwFastRelock 检查并执行半死槽快速换锁；已换锁返回候选地址与位置（调用方
// setLock + resetFollow 并 continue）。frozenN 未达阈值或玩家站桩（无活跃成员）
// 时不动作，返回 false。
func botwFastRelock(h uintptr, lockAddr uintptr, cur [3]float32, frozenN int, st *fastWatchState) (uintptr, [3]float32, bool) {
	if frozenN < botwFastFrozenTicks {
		return 0, [3]float32{}, false
	}
	if !st.On {
		var addrs []uintptr
		seen := map[uintptr]bool{lockAddr: true}
		for _, a := range loadAddrs() {
			if !seen[a] {
				seen[a] = true
				addrs = append(addrs, a)
			}
		}
		for _, a := range knownPool {
			if !seen[a] {
				seen[a] = true
				addrs = append(addrs, a)
			}
		}
		if len(addrs) > botwFastAddrCap {
			addrs = addrs[:botwFastAddrCap]
		}
		if len(addrs) == 0 {
			return 0, [3]float32{}, false
		}
		st.On, st.At = true, time.Now()
		st.Addrs = addrs
		st.Base = map[uintptr][3]float32{}
		st.Moved = make([]int, len(addrs))
		st.AnyMove = false
		return 0, [3]float32{}, false
	}
	if time.Since(st.At) < botwFastEvery {
		return 0, [3]float32{}, false
	}
	st.At = time.Now()
	for i, adr := range st.Addrs {
		d := decodePos(readMem(h, adr, 12))
		if d == nil {
			continue
		}
		v := [3]float32{d[0], d[1], d[2]}
		if b, ok := st.Base[adr]; ok {
			if dist3(v, b) > moveNet { // 相对观察起点的净位移（抖动来回抵消，对齐 knownWatch）
				st.Moved[i]++
				st.AnyMove = true
			}
		} else {
			st.Base[adr] = v
		}
	}
	best := -1
	for i, mv := range st.Moved {
		if mv >= botwFastMoveN && (best < 0 || mv > st.Moved[best]) {
			best = i
		}
	}
	if best < 0 {
		return 0, [3]float32{}, false
	}
	adr := st.Addrs[best]
	d := decodePos(readMem(h, adr, 12))
	if d == nil {
		return 0, [3]float32{}, false
	}
	v := [3]float32{d[0], d[1], d[2]}
	if dist3(v, cur) >= botwFastNear {
		return 0, [3]float32{}, false // 位置闸门：远处微动槽拒绝
	}
	return adr, v, true
}
