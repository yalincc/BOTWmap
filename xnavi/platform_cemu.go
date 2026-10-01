// platform_cemu.go — Cemu 平台驱动。
//
// 基线：navicemu（live-cemu）。差异点：
//   - 进程：精确匹配 "cemu"（防 attach 到自己）；多实例时优先选有大块（游戏已加载）者
//   - 内存：Cemu 在宿主进程里预留连续 4GB 虚拟空间模拟 Wii U 内存；MMU 区域
//     TEXT_AREA{0x02000000,0x0C000000} / MEM2{0x10000000,0x40000000} / MEM1{0xF4000000,0x02000000}
//     guest 地址 = memory_base + 偏移；偏移稳定、基址每次启动变
//   - 字节序：Wii U = PowerPC 大端（u64 指针、f32 坐标一律按大端解释）
//   - 指针语义：游戏内存里存的指针是 guest 虚拟地址（cheat 码 580F 链即此语义）
//   - 存档：mlc01/usr/save/00050000；主档 = mtime 最新（BotW 轮转写槽，槽号不可靠）
//
// known 偏移锚点 = 最大已提交 RW 块（largestRWBlock），不依赖精确 memory_base；
// 社区指针链（best-effort，本机 JP v208 实测不可用但保留）。

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

type cemuPlatform struct {
	h   uintptr
	pid uint32
}

func init() {
	registerPlatform(&cemuPlatform{})
}

func (p *cemuPlatform) Name() string { return "Cemu" }

// findLiveCemu 找到"正在运行游戏"的 Cemu 实例：
// 多个 cemu.exe 并存时（重启残留），优先选有 ≥minBlockMB 大块的实例，
// 否则退回 pid 最大的最新实例（游戏可能还在加载）。
func findLiveCemu() (uint32, uintptr) {
	pids := findPids("cemu")
	if len(pids) == 0 {
		return 0, 0
	}
	live, maxPid := uint32(0), uint32(0)
	for _, pid := range pids {
		if pid > maxPid {
			maxPid = pid
		}
		h, err := openProcess(pid)
		if err != nil || h == 0 {
			continue
		}
		if len(cemuRWBlocks(h, minBlockMB)) > 0 && pid > live {
			live = pid
		}
		closeHandle(h)
	}
	choose := live
	if choose == 0 {
		choose = maxPid
	}
	if choose == 0 {
		return 0, 0
	}
	h, err := openProcess(choose)
	if err != nil || h == 0 {
		return 0, 0
	}
	return choose, h
}

func (p *cemuPlatform) Attach() bool {
	pid, h := findLiveCemu()
	if pid == 0 || h == 0 {
		if p.h != 0 {
			closeHandle(p.h)
		}
		p.h, p.pid = 0, 0
		return false
	}
	if p.h != 0 {
		if pid == p.pid && hLive(p.h) {
			closeHandle(h) // 新开探测句柄弃用，沿用旧句柄
			return true
		}
		closeHandle(p.h)
	}
	p.h, p.pid = h, pid
	return true
}

func (p *cemuPlatform) Handle() uintptr { return p.h }
func (p *cemuPlatform) PID() uint32     { return p.pid }
func (p *cemuPlatform) LittleEndian() bool { return false }

// Blocks 已提交可读写连续区域（不限 Type，靠大小/位置交叉校验）。
func (p *cemuPlatform) Blocks(minMB float64) []MemBlock {
	return cemuRWBlocks(p.h, minMB)
}

// LargestBlock 最大已提交 RW 块（known 偏移锚点，每次必可找到）。
func (p *cemuPlatform) LargestBlock(minMB float64) (uintptr, uintptr) {
	return cemuLargestRWBlock(p.h)
}

// SaveRoots Cemu 存档根候选（按序探测，含便携版与常见安装目录）。
func (p *cemuPlatform) SaveRoots() []string {
	var roots []string
	add := func(pp string) {
		if pp != "" {
			roots = append(roots, pp)
		}
	}
	if v := os.Getenv("CEMUNAVI_SAVE_ROOT"); v != "" {
		add(v)
	}
	if app := os.Getenv("APPDATA"); app != "" {
		add(filepath.Join(app, "Cemu", "mlc01", "usr", "save", "00050000"))
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), "mlc01", "usr", "save", "00050000"))
	}
	for _, base := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), "Documents", "Cemu"),
		`C:\Cemu`,
		`D:\Cemu`,
		`E:\Cemu`,
		`H:\Cemu`,
	} {
		add(filepath.Join(base, "mlc01", "usr", "save", "00050000"))
	}
	return roots
}

// ---- 内存枚举（Cemu 专属）----

// cemuRWBlocks 枚举所有"已提交 + 可读写"的连续区域（相邻同保护区合并），返回 ≥minMB 的块。
func cemuRWBlocks(h uintptr, minMB float64) []MemBlock {
	minSize := uintptr(minMB * 1048576.0)
	var out []MemBlock
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	consecFails := 0
	for addr < limit {
		var mbi MemoryBasicInformation
		if !vqRetry(h, addr, &mbi) {
			consecFails++
			if consecFails > 64 {
				break // 句柄失效等持续失败：放弃本轮，避免空转
			}
			addr += 0x1000
			continue
		}
		consecFails = 0
		size := mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit && (p == 0x02 || p == 0x04) && (mbi.Protect&pageGuard) == 0 {
			out = append(out, MemBlock{mbi.AllocationBase, size})
		}
		nxt := mbi.BaseAddress + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	var filtered []MemBlock
	for _, b := range out {
		if b.Size >= minSize {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// cemuLargestRWBlock 返回最大的一块已提交可读写区域（known offsets 锚点）。
func cemuLargestRWBlock(h uintptr) (uintptr, uintptr) {
	best, bestSize := uintptr(0), uintptr(0)
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	consecFails := 0
	for addr < limit {
		var mbi MemoryBasicInformation
		if !vqRetry(h, addr, &mbi) {
			consecFails++
			if consecFails > 64 {
				break // 句柄失效等持续失败：放弃本轮，避免空转
			}
			addr += 0x1000
			continue
		}
		consecFails = 0
		size := mbi.RegionSize
		if mbi.State == memCommit &&
			((mbi.Protect&0xFF) == 0x02 || (mbi.Protect&0xFF) == 0x04) &&
			(mbi.Protect&pageGuard) == 0 && size > bestSize {
			best, bestSize = mbi.AllocationBase, size
		}
		nxt := mbi.BaseAddress + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	return best, bestSize
}

// hasCommittedRW 检查 addr 起始的连续已提交可读区域合计是否 ≥ want 字节。
// addr 可能落在某个大区域内部：从 addr 所在区域起向后累计，不要求区域正好从 addr 开始。
// 允许 Exec 类保护（0x20/0x40/0x80），供校验用。
func hasCommittedRW(h uintptr, addr uintptr, want uintptr) bool {
	total := uintptr(0)
	cur := addr
	for total < want {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, cur, &mbi) == 0 {
			return false
		}
		if mbi.State != memCommit {
			return false
		}
		p := mbi.Protect & 0xFF
		if p != 0x02 && p != 0x04 && p != 0x20 && p != 0x40 && p != 0x80 {
			return false
		}
		total += mbi.RegionSize
		cur += mbi.RegionSize
	}
	return true
}

// ---- 大端读取 ----

func readU64BE(h uintptr, addr uintptr) (uint64, bool) {
	d := readMem(h, addr, 8)
	if len(d) < 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(d), true
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

// ---- memory_base 锚定与社区指针链（best-effort 快路径资产）----

// findCemuBase 反推 Cemu 的 memory_base。两条路线：
// 1) 区域结构校验：MEM2（≥512MB @ +0x10000000）反推 → 校验 TEXT（+0x02000000，≥64MB）。
//    （不要求 MEM1——Cemu 2.x 实测 MEM1 区可能未提交。）
// 2) 链探针法（不依赖区域布局假设）：把每个大块当作 TEXT/MEM2/整空间 的候选基址，
//    直接试解社区指针链（值必须落在合理 guest 地址区间），解通即确认。
// 返回 (base, mem2HostBase, ok)。
func findCemuBase(h uintptr) (uintptr, uintptr, bool) {
	blocks := cemuRWBlocks(h, 64.0)
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

// cemuChain 社区指针链（guest 偏移，Cemu 版本无关）。
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
		if !gameValidTriple(x, y, z) {
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
