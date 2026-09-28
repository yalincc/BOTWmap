// Cemu 内存模型层。
//
// Cemu（Wii U 模拟器）在宿主进程里用 MMU 预留一块连续 4GB 虚拟地址空间模拟 Wii U 内存，
// guest 地址 = memory_base + 偏移；memory_base 每次启动由系统分配（不稳定），偏移稳定。
// 关键区域（guest 偏移，Cemu 源码 src/Cafe/HW/MMU/MMU.cpp）：
//
//	mmuRange_TEXT_AREA { 0x02000000, 0x0C000000 }  ← 192MB 程序/静态数据
//	mmuRange_MEM2      { 0x10000000, 0x40000000 }  ← 1GB 主数据堆（玩家对象在这里）
//	mmuRange_MEM1      { 0xF4000000, 0x02000000 }  ← 32MB 低内存
//
// ★ 字节序：Wii U 是 PowerPC 大端。Cemu 的 memory_readU64/readFloat 全部 swapEndian，
//   所以外部读内存时 u64 指针、f32 坐标一律按大端解释。
// ★ 指针值语义：游戏内存里存的指针是 guest 虚拟地址（cheat 码 580F 链即此语义），
//   外部读取 host 地址 = memory_base + guestVA。比 Ryujinx 的 guest↔host 换算简单得多。

package main

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ---- 大端读取 ----

func beU64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }
func beU32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

func readU64BE(h uintptr, addr uintptr) (uint64, bool) {
	d := readMem(h, addr, 8)
	if len(d) < 8 {
		return 0, false
	}
	return beU64(d), true
}

func readF32BE(h uintptr, addr uintptr) (float32, bool) {
	d := readMem(h, addr, 4)
	if len(d) < 4 {
		return 0, false
	}
	return math.Float32frombits(beU32(d)), true
}

// readPosBE 读 12 字节坐标槽 (X, alt, Z)，大端。
func readPosBE(h uintptr, addr uintptr) ([]float32, bool) {
	d := readMem(h, addr, 12)
	if len(d) < 12 {
		return nil, false
	}
	return []float32{
		math.Float32frombits(beU32(d[0:4])),
		math.Float32frombits(beU32(d[4:8])),
		math.Float32frombits(beU32(d[8:12])),
	}, true
}

// toF32BE 把大端字节 buffer 转为 float32 写入 dst（复用缓冲，避免每块 GC），返回有效长度。
func toF32BE(buf []byte, dst []float32) int {
	n := len(buf) / 4
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		o := i * 4
		dst[i] = math.Float32frombits(uint32(buf[o])<<24 | uint32(buf[o+1])<<16 | uint32(buf[o+2])<<8 | uint32(buf[o+3]))
	}
	return n
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// ---- memory_base 锚定 ----

// findCemuBase 反推 Cemu 的 memory_base。两条路线：
// 1) 区域结构校验：MEM2（≥512MB @ +0x10000000）反推 → 校验 TEXT（+0x02000000，≥64MB）。
//    （不要求 MEM1——Cemu 2.x 实测 MEM1 区可能未提交。）
// 2) 链探针法（不依赖区域布局假设）：把每个大块当作 TEXT/MEM2/整空间 的候选基址，
//    直接试解社区指针链（值必须落在合理 guest 地址区间），解通即确认。
// 返回 (base, mem2HostBase, ok)。
func findCemuBase(h uintptr) (uintptr, uintptr, bool) {
	blocks := rwBlocks(h, 64.0)
	for _, b := range blocks {
		if b.Size >= 512<<20 {
			base := b.Base - 0x10000000
			if hasCommittedRW(h, base+0x02000000, 64<<20) {
				return base, b.Base, true
			}
		}
	}
	for _, b := range blocks {
		if b.Size >= 128<<20 && b.Size < 512<<20 && b.Base < 0x10000000 {
			base := b.Base - 0x02000000
			if hasCommittedRW(h, base+0x10000000, 512<<20) {
				return base, base + 0x10000000, true
			}
		}
	}
	for _, b := range blocks {
		for _, delta := range []uintptr{0x02000000, 0x10000000, 0} {
			cand := b.Base - delta
			if cand == 0 || cand > b.Base {
				continue
			}
			for _, c := range cemuChains {
				if va, ok := resolveChain(h, cand, c); ok {
					if len(slotScanAround(h, cand, va)) > 0 {
						return cand, cand + 0x10000000, true
					}
				}
			}
		}
	}
	return 0, 0, false
}

// ---- 社区指针链（guest 偏移，Cemu 版本无关）----
// 来源：Cemu 社区 cheat 码 580F 指针链。BotW v1.x 实测。
// 链 A（月亮跳/速度同源）：[[0x02D1EA00]+0x198]+0x2D18 → 当前玩家 actor 对象
// 链 B（更深的稳定链）：[[[[0x02CC5DE8]+0x30]+0x10]+0x8]+0x10

type cemuChain struct {
	Name    string
	Root    uintptr
	Offsets []uintptr
}

var cemuChains = []cemuChain{
	{"playerA", 0x02D1EA00, []uintptr{0x198, 0x2D18}},
	{"playerB", 0x02CC5DE8, []uintptr{0x30, 0x10, 0x8, 0x10}},
}

// resolveChain 从根 guest 偏移沿链解出最终 guest 地址。每级指针值 = guest 虚拟地址。
func resolveChain(h uintptr, base uintptr, c cemuChain) (uintptr, bool) {
	cur := base + c.Root
	for _, off := range c.Offsets {
		v, ok := readU64BE(h, cur)
		if !ok || v < 0x01000000 || v > 0x4FFFFFFF {
			return 0, false // 指针必须落在合理 guest 地址区间
		}
		cur = base + uintptr(v) + off
	}
	return cur, true
}

// posRangeOK 坐标合理性（与 BotwNavi decodePos 同口径）：(X, alt, Z)。
// Fix 5 / 5b：剔除近零残留与单轴 denormal 垃圾。
// Fix 6（踩坑 #6）：NaN 必须单独判——它与任何数比较都是 false，范围检查抓不住。
// Fix 7：失效偏移常读到"按钮/未初始化槽"——单轴近零(≈0)且高度贴地(<5)，玩家坐标不会这样。
func posRangeOK(x, alt, z float32) bool {
	if x != x || alt != alt || z != z {
		return false // NaN
	}
	if x == 0 && alt == 0 && z == 0 {
		return false // 标题/加载画面槽未初始化
	}
	if abs32(x) < 5 && abs32(z) < 5 {
		return false // 近零簇（原点残留）
	}
	if (abs32(x) < 5 && alt < 5) || (abs32(z) < 5 && alt < 5) {
		return false // 未初始化/按钮槽：单轴近零且高度贴地（如 (0.0, 1.0, 1523.4)）
	}
	if abs32(x) < 1e-20 || abs32(z) < 1e-20 {
		return false // 单轴 denormal
	}
	if x <= -7000 || x >= 7000 || z <= -7000 || z >= 7000 ||
		alt <= -600 || alt >= 5000 {
		return false
	}
	return true
}

// rotOKF 检查 9 个 float 是否构成正交归一 3×3 旋转矩阵（BotW ActorBase 特征）。
// NaN 矩阵必须拒绝（与任何数比较都是 false，会导致全内存假命中）。
func rotOKF(m []float32) bool {
	if len(m) < 9 {
		return false
	}
	for _, v := range m[:9] {
		if v != v {
			return false
		}
	}
	cols := [3][3]float32{
		{m[0], m[3], m[6]},
		{m[1], m[4], m[7]},
		{m[2], m[5], m[8]},
	}
	for i := 0; i < 3; i++ {
		n := math.Sqrt(float64(cols[i][0]*cols[i][0] + cols[i][1]*cols[i][1] + cols[i][2]*cols[i][2]))
		if math.Abs(n-1.0) >= 0.05 {
			return false
		}
	}
	for i := 0; i < 3; i++ {
		for j := i + 1; j < 3; j++ {
			dot := cols[i][0]*cols[j][0] + cols[i][1]*cols[j][1] + cols[i][2]*cols[j][2]
			if math.Abs(float64(dot)) >= 0.08 {
				return false
			}
		}
	}
	return true
}

// slotScanAround 在 actor 对象 guest 地址附近窗口内找 [坐标(3f)+正交矩阵(9f)] 结构。
// 返回候选槽 host 地址（内存序 X, alt, Z）。
func slotScanAround(h uintptr, base uintptr, actorVA uintptr) []Hit {
	const lo, hi = 0x200, 0x600
	start := actorVA - lo
	var out []Hit
	buf := make([]byte, 48)
	fl := make([]float32, 12)
	for off := uintptr(0); off < lo+hi; off += 4 {
		addr := base + start + off
		d := readMem(h, addr, 48)
		if len(d) < 48 {
			continue
		}
		copy(buf, d[:48])
		n := toF32BE(buf[:48], fl)
		if n < 12 {
			continue
		}
		x, y, z := fl[0], fl[1], fl[2]
		if !posRangeOK(x, y, z) {
			continue
		}
		if !rotOKF(fl[3:12]) {
			continue
		}
		out = append(out, Hit{Addr: addr, X: x, Y: y, Z: z})
	}
	return out
}

// chainLocate 用社区链定位坐标槽（确定性，无需用户走动）。
func chainLocate(h uintptr, base uintptr) *LocateResult {
	for _, c := range cemuChains {
		actorVA, ok := resolveChain(h, base, c)
		if !ok {
			fmt.Printf("  [chain] %s: resolve failed\n", c.Name)
			continue
		}
		hits := slotScanAround(h, base, actorVA)
		fmt.Printf("  [chain] %s: actor=0x%012X  %d slot candidates\n", c.Name, actorVA, len(hits))
		if len(hits) == 0 {
			continue
		}
		best := hits[0]
		for _, hh := range hits[1:] {
			// 优先取坐标更大的（远离原点残留）
			if abs32(hh.X)+abs32(hh.Z) > abs32(best.X)+abs32(best.Z) {
				best = hh
			}
		}
		return &LocateResult{
			Addr:   best.Addr,
			Copies: len(hits),
			Struct: 1,
			Hud:    [3]float32{best.X, best.Z, best.Y},
			Log:    []string{fmt.Sprintf("chain %s -> 0x%012X", c.Name, best.Addr)},
		}
	}
	return nil
}
