// probe_chain — Eden 金手指链验证（xnavi 三合一阶段 0，只读，不写内存/不写 known）。
//
// probe_botw.exe layout   — 枚举 Eden 全部内存区域（commit + reserve），输出大块布局
//                           与 AllocationBase 分组，确认 fastmem arena 形态。
// probe_botw.exe chain    — 自动找 arena 基址候选 → 解金手指链（小端 u64 指针）：
//                           playerA: 0x02D1EA00 → +0x198 → +0x2D18
//                           playerB: 0x02CC5DE8 → +0x30 → +0x10 → +0x8 → +0x10
//                           → 链尾 actor 附近扫坐标槽 → 与存档锚点交叉验证。
//
// 背景（调研报告已确认）：金手指码用 guest 虚拟地址（游戏版本决定，与 DRAM 大小无关）；
// Eden（Yuzu fork）用 fastmem 把 guest 39-bit 地址空间线性映射到宿主一段虚拟地址，
// 故 host = arena_base + guest_va。链根 0x02D1EA00 与 Cemu 社区链（xnavi 已验证）相同。

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const (
	stateReserve = 0x2000
	guestAddrMin = uint32(0x01000000) // guest 指针 sanity：用户态低地址下限
	guestAddrMax = uint32(0x80000000) // 32 位进程用户态上限
	slotRadius   = uintptr(0x20000)   // 链尾 actor 附近扫描半径
	anchorDistOK = float32(500.0)     // 与存档锚点距离 < 500m 视为强命中
	arenaMinGB   = 256 << 20          // AllocationBase 组 commit 总量下限
)

type region struct {
	Base  uintptr
	Alloc uintptr
	Size  uintptr
	State uint32
	Prot  uint32
	Type  uint32
}

// guestRegionsAll 枚举全部区域（commit + reserve），保留 AllocationBase（一次 VirtualAlloc 的归属）。
func guestRegionsAll(h uintptr) []region {
	var out []region
	addr := uintptr(0)
	for {
		var m MBI
		r, _, _ := procVQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
		if r == 0 {
			break
		}
		if m.RegionSize > 0 && (m.State == memCommit || m.State == stateReserve) {
			out = append(out, region{m.BaseAddress, m.AllocationBase, m.RegionSize, m.State, m.Protect, m.Type})
		}
		if m.RegionSize == 0 {
			break
		}
		next := m.BaseAddress + m.RegionSize
		if next <= addr {
			break
		}
		addr = next
	}
	return out
}

// arenaCandidates 按 AllocationBase 分组，组内 commit 总量 ≥256MB 视为 guest 地址空间候选，
// 组起点（组内最小 BaseAddress）作为 arena_base 候选（guest VA 0 对应的宿主地址）。
func arenaCandidates(regs []region) []uintptr {
	type grp struct {
		start  uintptr
		commit uint64
	}
	m := map[uintptr]*grp{}
	for _, r := range regs {
		g, ok := m[r.Alloc]
		if !ok {
			g = &grp{start: r.Base}
			m[r.Alloc] = g
		}
		if r.Base < g.start {
			g.start = r.Base
		}
		if r.State == memCommit {
			g.commit += uint64(r.Size)
		}
	}
	var cands []uintptr
	for _, g := range m {
		if g.commit >= arenaMinGB {
			cands = append(cands, g.start)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i] < cands[j] })
	return cands
}

var goldenChains = []struct {
	Name   string
	Root   uint32
	Offs   []uint32
	Source string
}{
	{"playerA", 0x02D1EA00, []uint32{0x198, 0x2D18}, "BOTW Switch 1.6.0 金手指 Moonjump（GBAtemp/CheatSlips）；同 Cemu 社区链"},
	{"playerB", 0x02CC5DE8, []uint32{0x30, 0x10, 0x8, 0x10}, "Cemu 社区链（xnavi platform_cemu.go 已收录）"},
}

// resolveChain 在 arena_base 上解 guest 链（小端 u64 指针，32 位进程高 32 位应为 0），
// 返回链尾（actor）宿主地址；任一跳失败返回 0。
func resolveChain(h uintptr, base uintptr, root uint32, offs []uint32) uintptr {
	cur := base + uintptr(root)
	for _, off := range offs {
		d := readMem(h, cur, 8)
		if d == nil || len(d) < 8 {
			return 0
		}
		v := binary.LittleEndian.Uint64(d)
		if v>>32 != 0 || v < uint64(guestAddrMin) || v >= uint64(guestAddrMax) {
			return 0
		}
		cur = base + uintptr(v) + uintptr(off)
	}
	return cur
}

var perms6 = [6][3]int{
	{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
}

type slotHit struct {
	Addr uintptr
	Mem  [3]float32
	Perm [3]int
	Dist float32
}

func minPermDist(a, b [3]float32) ([3]int, float32) {
	best := perms6[0]
	bestD := float32(1e9)
	for _, p := range perms6 {
		dx := a[0] - b[p[0]]
		dy := a[1] - b[p[1]]
		dz := a[2] - b[p[2]]
		d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
		if d < bestD {
			bestD = d
			best = p
		}
	}
	return best, bestD
}

// cmdLayout 枚举 Eden 全部区域，输出布局（先看 arena 形态，不猜测）。
func cmdLayout() {
	pid := findPid("eden")
	if pid == 0 {
		fmt.Println("Eden not running - start it and load BOTW first.")
		return
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed (run as admin?).")
		return
	}
	defer procCloseHandle.Call(h)
	regs := guestRegionsAll(h)
	var commitTotal uint64
	type grpInfo struct {
		start   uintptr
		commit  uint64
		reserve uint64
		n       int
	}
	grps := map[uintptr]*grpInfo{}
	for _, r := range regs {
		g, ok := grps[r.Alloc]
		if !ok {
			g = &grpInfo{start: r.Base}
			grps[r.Alloc] = g
		}
		if r.Base < g.start {
			g.start = r.Base
		}
		if r.State == memCommit {
			g.commit += uint64(r.Size)
			commitTotal += uint64(r.Size)
		} else {
			g.reserve += uint64(r.Size)
		}
		g.n++
	}
	fmt.Printf("Eden pid=%d  regions=%d  commit_total=%.2f GB\n", pid, len(regs), float64(commitTotal)/1073741824.0)

	// 按 commit 总量降序列出大组
	var gs []grpInfo
	for _, g := range grps {
		gs = append(gs, *g)
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].commit > gs[j].commit })
	fmt.Println("\ntop AllocationBase 组（按 commit 降序，前 12）:")
	for i, g := range gs {
		if i >= 12 {
			break
		}
		fmt.Printf("  start=0x%014X commit=%.2f GB reserve=%.2f GB regions=%d\n",
			g.start, float64(g.commit)/1073741824.0, float64(g.reserve)/1073741824.0, g.n)
	}

	// 单个最大 commit 大块
	fmt.Println("\n单个最大 MEM_COMMIT 区域（前 8）:")
	var big []region
	for _, r := range regs {
		if r.State == memCommit {
			big = append(big, r)
		}
	}
	sort.Slice(big, func(i, j int) bool { return big[i].Size > big[j].Size })
	for i, r := range big {
		if i >= 8 {
			break
		}
		prot := "?"
		switch r.Prot & 0xFF {
		case 0x04:
			prot = "RW"
		case 0x40:
			prot = "RWX"
		case 0x02:
			prot = "RO"
		}
		fmt.Printf("  0x%014X size=%.2f GB state=%s prot=%s type=0x%X\n", r.Base, float64(r.Size)/1073741824.0, "COMMIT", prot, r.Type)
	}
	fmt.Println("\n下一步：probe_botw.exe chain")
}

// cmdChain 验证金手指链可用性。
func cmdChain() {
	pid := findPid("eden")
	if pid == 0 {
		fmt.Println("Eden not running - start it and load BOTW first.")
		return
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed (run as admin?).")
		return
	}
	defer procCloseHandle.Call(h)
	t0 := time.Now()

	// 存档锚点（自动找 Eden BOTW 存档）
	var anchor *[3]float32
	anchorPath := ""
	for _, cand := range edenSaveRoots() {
		if fi, err := osStat(cand); err == nil && fi.IsDir() {
			for _, p := range findSaveFiles(cand, "01007EF00011E000") {
				if a := parseGameData(p); a != nil {
					anchor, anchorPath = a, p
					break
				}
			}
		}
	}
	if anchor == nil {
		fmt.Println("no save anchor found - chain 判定将仅凭链 sanity 与槽扫描（弱信号）")
	} else {
		fmt.Printf("save anchor: (%.1f, %.1f, %.1f) @ %s\n", anchor[0], anchor[1], anchor[2], anchorPath)
	}

	regs := guestRegionsAll(h)
	cands := arenaCandidates(regs)
	fmt.Printf("arena candidates: %d\n", len(cands))
	for _, c := range cands {
		fmt.Printf("  0x%014X\n", c)
	}
	if len(cands) == 0 {
		fmt.Println("no arena candidate (无 ≥256MB commit 组) — 先跑 layout 观察布局")
		return
	}

	// --- main 宿主检测（金手指地址 = main 相对偏移，需先定位 main 模块宿主）---
	fmt.Println("\n[main 宿主检测]")
	for _, base := range cands {
		// 若 Eden 为线性 fastmem（guestVA + arenaBase = hostVA），main 宿主 = base + 0x7100000000
		d := readMem(h, base+0x7100000000, 16)
		if d != nil && len(d) >= 16 {
			fmt.Printf("  [线性] base 0x%014X +0x7100000000 可读: %s\n", base, describeData(d))
			d2 := readMem(h, base+0x7100000000+0x02D1EA00, 16)
			if d2 != nil {
				fmt.Printf("    main+0x02D1EA00: %s\n", describeData(d2))
			}
		} else {
			fmt.Printf("  [线性] base 0x%014X +0x7100000000 不可读\n", base)
		}
	}
	// 独立块扫描：60-320MB commit 块内查链根（BOTW main ~120MB）
	for _, r := range regs {
		if r.State == memCommit && r.Size >= 60<<20 && r.Size <= 320<<20 {
			d := readMem(h, r.Base+0x02D1EA00, 16)
			if d == nil || len(d) < 16 {
				continue
			}
			fmt.Printf("  [60-320MB块] 0x%014X size=%.0fMB prot=0x%X type=0x%X +0x02D1EA00: %s\n",
				r.Base, float64(r.Size)/(1<<20), r.Prot, r.Type, describeData(d))
		}
	}

	okAny := false
	dumpAddrs := []struct {
		name string
		off  uint64
	}{
		{"playerA根 0x02D1EA00", 0x02D1EA00},
		{"playerB根 0x02CC5DE8", 0x02CC5DE8},
		{"ryu坐标 0x277FCBBC", 0x277FCBBC},
		{"guest 0x01000000", 0x01000000},
		{"guest main 0x7100000000", 0x7100000000},
	}
	for _, base := range cands {
		fmt.Printf("\n[base 0x%014X]\n", base)
		for _, da := range dumpAddrs {
			addr := base + uintptr(da.off)
			d := readMem(h, addr, 16)
			if d == nil || len(d) < 16 {
				fmt.Printf("  %-24s 0x%014X  unreadable\n", da.name, addr)
				continue
			}
			fmt.Printf("  %-24s 0x%014X  %s\n", da.name, addr, describeData(d))
		}
		for _, ch := range goldenChains {
			actor := resolveChain(h, base, ch.Root, ch.Offs)
			if actor == 0 {
				continue
			}
			hits := scanCoordSlots(h, actor, anchorOrZero(anchor))
			fmt.Printf("\n[chain OK] base=0x%014X %s root=0x%08X actor=0x%014X slots=%d\n", base, ch.Name, ch.Root, actor, len(hits))
			for i, s := range hits {
				if i >= 6 {
					fmt.Printf("  ... (%d more)\n", len(hits)-6)
					break
				}
				fmt.Printf("  slot 0x%014X mem=(%.2f, %.2f, %.2f) perm=%v dist=%.1fm\n",
					s.Addr, s.Mem[0], s.Mem[1], s.Mem[2], s.Perm, s.Dist)
			}
			if len(hits) > 0 {
				okAny = true
			}
		}
	}

	// 汇总：是否有任何 base 在 ryu 坐标 guest VA 处读出坐标样（Eden 快路径直接成立）
	// 判定收紧：三轴均 sane（|v|<8000）且非零样（至少两轴 >5），排除全零/垃圾。
	ryuOK := false
	for _, base := range cands {
		if d := readMem(h, base+0x277FCBBC, 12); d != nil && len(d) >= 12 {
			f := floats(d)
			if len(f) >= 3 && absf(f[0]) < 8000 && absf(f[1]) < 8000 && absf(f[2]) < 8000 &&
				(absf(f[0]) > 5 || absf(f[1]) > 5 || absf(f[2]) > 5) && !(f[0] == 0 && f[1] == 0 && f[2] == 0) {
				fmt.Printf("\n[ryu guest VA OK] base=0x%014X +0x277FCBBC = (%.2f, %.2f, %.2f)\n", base, f[0], f[1], f[2])
				ryuOK = true
			}
		}
	}

	// --- sane 坐标全扫 → 反推正确 arena_base（关键）---
	fmt.Println("\n--- sane 坐标全扫（反推 arena_base）---")
	blocks := guestBlocks(h)
	saneHits := scanSaneHits(h, blocks, 1000000)
	fmt.Printf("sane hits: %d\n", len(saneHits))
	// 宿主地址分布（按 1GB 桶），坐标数据聚在哪些区域一目了然
	type bkt struct {
		addr uintptr
		n    int
	}
	bucket := map[uintptr]int{}
	for _, s := range saneHits {
		bucket[(s.Addr>>30)<<30]++
	}
	var bkts []bkt
	for a, n := range bucket {
		bkts = append(bkts, bkt{a, n})
	}
	sort.Slice(bkts, func(i, j int) bool { return bkts[i].n > bkts[j].n })
	fmt.Println("宿主地址分布（1GB 桶，top 12）:")
	for i, b := range bkts {
		if i >= 12 {
			break
		}
		fmt.Printf("  0x%014X  hits=%d\n", b.addr, b.n)
	}
	// 候选 base 低区统计（原 5 候选）
	baseStats := map[uintptr]int{}
	for _, s := range saneHits {
		for _, base := range cands {
			if s.Addr > base && s.Addr-base < 0x80000000 {
				baseStats[base]++
			}
		}
	}
	type bs struct {
		base uintptr
		n    int
	}
	var bss []bs
	for b, n := range baseStats {
		bss = append(bss, bs{b, n})
	}
	sort.Slice(bss, func(i, j int) bool { return bss[i].n > bss[j].n })
	for _, b := range bss {
		fmt.Printf("  base 0x%014X -> 低区 sane 命中 %d\n", b.base, b.n)
	}
	if len(bss) > 0 {
		best := bss[0].base
		fmt.Printf("  最佳 arena_base 候选: 0x%014X\n", best)
		tryChainsOn(h, best, anchor, &okAny)
	}
	// 锚点最近 hit → 反推 ryu-坐标 base（host - 0x277FCBBC）
	if anchor != nil && len(saneHits) > 0 {
		bi := 0
		bd := float32(1e9)
		for i, s := range saneHits {
			_, d := minPermDist(s.Mem, *anchor)
			if d < bd {
				bd, bi = d, i
			}
		}
		s := saneHits[bi]
		fmt.Printf("\n玩家坐标候选（距锚点最近）: host=0x%014X mem=(%.2f, %.2f, %.2f) 距锚点 %.0fm\n",
			s.Addr, s.Mem[0], s.Mem[1], s.Mem[2], bd)
		if s.Addr > 0x277FCBBC {
			rb := s.Addr - 0x277FCBBC
			fmt.Printf("  反推 ryu-base: 0x%014X（= host - 0x277FCBBC）\n", rb)
			tryChainsOn(h, rb, anchor, &okAny)
		}
		for _, base := range cands {
			if s.Addr > base && s.Addr-base < 0x80000000 {
				fmt.Printf("  [base 0x%014X 下 guestVA=0x%08X]\n", base, s.Addr-base)
			}
		}
	}
	// top 桶中心 → 反推 ryu-base 尝试（多个独立桶分别试）
	for i, b := range bkts {
		if i >= 3 {
			break
		}
		c := b.addr + 0x40000000 // 桶中心
		if c > 0x277FCBBC {
			rb := c - 0x277FCBBC
			fmt.Printf("top桶反推 ryu-base: 0x%014X (桶 0x%014X)\n", rb, b.addr)
			tryChainsOn(h, rb, anchor, &okAny)
		}
	}

	fmt.Printf("\n=== 判定 ===\n")
	if okAny {
		fmt.Println("金手指链可用：Eden 快路径成立（链解析 + 坐标槽命中）")
	} else if ryuOK {
		fmt.Println("链未解通，但 Ryujinx guest VA 0x277FCBBC 在 Eden 可用 → 直接固定偏移快路径（比链更强）")
	} else {
		fmt.Println("金手指链未命中：需检查 layout（arena 形态）或游戏版本差异；保留扫描回退方案")
	}
	fmt.Printf("elapsed %s\n", time.Since(t0).Round(time.Millisecond))
}

// describeData 判定 16 字节原始数据的语义（指针样/坐标样/纯数据），供人工核对。
func describeData(d []byte) string {
	if len(d) < 16 {
		return "short"
	}
	var out []string
	u0 := binary.LittleEndian.Uint64(d[0:8])
	u1 := binary.LittleEndian.Uint64(d[8:16])
	if u0 >= uint64(guestAddrMin) && u0 < uint64(guestAddrMax) && u0>>32 == 0 {
		out = append(out, fmt.Sprintf("ptrA=0x%08X", u0))
	}
	if u1 >= uint64(guestAddrMin) && u1 < uint64(guestAddrMax) && u1>>32 == 0 {
		out = append(out, fmt.Sprintf("ptrB=0x%08X", u1))
	}
	if u0 >= 0x800000000 && u0 < 0x800000000+0x100000000 {
		out = append(out, fmt.Sprintf("ptrHi=0x%X", u0))
	}
	f := floats(d)
	if len(f) >= 4 {
		allF := true
		for i := 0; i < 4; i++ {
			if absf(f[i]) > 8000 {
				allF = false
				break
			}
		}
		if allF && !(f[0] == 0 && f[1] == 0 && f[2] == 0 && f[3] == 0) {
			out = append(out, fmt.Sprintf("f=(%.1f, %.1f, %.1f, %.1f)", f[0], f[1], f[2], f[3]))
		}
	}
	hexs := make([]string, len(d))
	for i, b := range d {
		hexs[i] = fmt.Sprintf("%02X", b)
	}
	if len(out) == 0 {
		return "data  " + strings.Join(hexs, " ")
	}
	return strings.Join(out, " | ") + "  [" + strings.Join(hexs, " ") + "]"
}

func anchorOrZero(a *[3]float32) [3]float32 {
	if a == nil {
		return [3]float32{}
	}
	return *a
}

// tryChainsOn 用给定 base 尝试所有金手指链：解链 → 坐标槽扫描 → 命中即算快路径成立。
func tryChainsOn(h uintptr, base uintptr, anchor *[3]float32, okAny *bool) {
	for _, ch := range goldenChains {
		actor := resolveChain(h, base, ch.Root, ch.Offs)
		if actor == 0 {
			continue
		}
		hits2 := scanCoordSlots(h, actor, anchorOrZero(anchor))
		fmt.Printf("\n[chain OK] base=0x%014X %s actor=0x%014X slots=%d\n", base, ch.Name, actor, len(hits2))
		for i, s := range hits2 {
			if i >= 6 {
				fmt.Printf("  ... (%d more)\n", len(hits2)-6)
				break
			}
			fmt.Printf("  slot 0x%014X mem=(%.2f, %.2f, %.2f) perm=%v dist=%.1fm\n",
				s.Addr, s.Mem[0], s.Mem[1], s.Mem[2], s.Perm, s.Dist)
		}
		if len(hits2) > 0 {
			*okAny = true
		}
	}
}

// cmdMain 定位 BOTW main 模块宿主：特征搜索（NSO0 magic / 版本字符串）+ 大块头部检查。
// 金手指链根 = main + 0x02D1EA00，必须先确定 main 宿主（Eden 分段映射，非线性）。
func cmdMain() {
	pid := findPid("eden")
	if pid == 0 {
		fmt.Println("Eden not running - start it and load BOTW first.")
		return
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed (run as admin?).")
		return
	}
	defer procCloseHandle.Call(h)
	t0 := time.Now()

	blocks := guestBlocks(h)
	regs := guestRegionsAll(h)

	// 1. 全内存搜 "NSO0" (0x304F534E) — main.nso 文件头 magic
	fmt.Println("--- 搜索 NSO0 magic ---")
	var nso []uintptr
	for _, b := range blocks {
		for off := uintptr(0); off < b[1]; off += chunkSize {
			n := uintptr(chunkSize)
			if off+n > b[1] {
				n = b[1] - off
			}
			d := readMem(h, b[0]+off, int(n))
			if d == nil {
				continue
			}
			for k := 0; k+4 <= len(d); k += 4 {
				if binary.LittleEndian.Uint32(d[k:]) == 0x304F534E {
					nso = append(nso, b[0]+off+uintptr(k))
					if len(nso) >= 30 {
						break
					}
				}
			}
			if len(nso) >= 30 {
				break
			}
		}
		if len(nso) >= 30 {
			break
		}
	}
	for _, a := range nso {
		fmt.Printf("  NSO0 @ 0x%014X\n", a)
	}
	if len(nso) == 0 {
		fmt.Println("  (无 NSO0 命中 — main 原始文件缓冲可能已被擦除)")
	}

	// 2. 全内存搜 BOTW 版本字符串 "1.6.0"（ASCII，限可打印上下文）
	fmt.Println("\n--- 搜索 \"1.6.0\" 字符串 ---")
	var ver []uintptr
	for _, b := range blocks {
		for off := uintptr(0); off < b[1]; off += chunkSize {
			n := uintptr(chunkSize)
			if off+n > b[1] {
				n = b[1] - off
			}
			d := readMem(h, b[0]+off, int(n))
			if d == nil {
				continue
			}
			for k := 0; k+5 <= len(d); k++ {
				if d[k] == '1' && d[k+1] == '.' && d[k+2] == '6' && d[k+3] == '.' && d[k+4] == '0' {
					ver = append(ver, b[0]+off+uintptr(k))
					if len(ver) >= 30 {
						break
					}
				}
			}
			if len(ver) >= 30 {
				break
			}
		}
		if len(ver) >= 30 {
			break
		}
	}
	for _, a := range ver {
		d := readMem(h, a-64, 192)
		prefix := ""
		if d != nil && len(d) >= 192 {
			prefix = strings.Map(func(r rune) rune {
				if r >= 32 && r < 127 {
					return r
				}
				return '.'
			}, string(d))
		}
		// 所在区域属性
		var m MBI
		procVQueryEx.Call(h, a, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
		prot := "?"
		switch {
		case m.Protect&0x04 != 0:
			prot = "RW"
		case m.Protect&0x02 != 0:
			prot = "R"
		case m.Protect&0x20 != 0:
			prot = "RX"
		case m.Protect&0x40 != 0:
			prot = "RWX"
		}
		fmt.Printf("  \"1.6.0\" @ 0x%014X [%s %dMB] ctx=...%s...\n",
			a, prot, m.RegionSize/(1<<20), prefix)
	}

	// 3. 所有含执行位（0x20/0x40/0x80）的 commit 块 ≥8MB — main 代码段/JIT 缓存候选
	fmt.Println("\n--- 可执行块（prot 含 EXECUTE）≥8MB ---")
	for _, r := range regs {
		if r.State != memCommit || r.Size < 8<<20 {
			continue
		}
		if r.Prot&0x20 == 0 && r.Prot&0x40 == 0 && r.Prot&0x80 == 0 {
			continue
		}
		d := readMem(h, r.Base, 32)
		head := ""
		if d != nil && len(d) >= 32 {
			head = strings.Map(func(r rune) rune {
				if r >= 32 && r < 127 {
					return r
				}
				return '.'
			}, string(d))
		}
		hexs := make([]string, 0, 16)
		if d != nil {
			for i := 0; i < 16 && i < len(d); i++ {
				hexs = append(hexs, fmt.Sprintf("%02X", d[i]))
			}
		}
		fmt.Printf("  0x%014X size=%.0fMB prot=0x%X type=0x%X head=%s hex=%s\n",
			r.Base, float64(r.Size)/(1<<20), r.Prot, r.Type, head, strings.Join(hexs, " "))
	}

	// 4. 全面链根扫描：所有 commit 块（任意大小/类型/保护），+0x02D1EA00/+0x02CC5DE8 处
	//    读 u64，若为 guest 低区指针（0x01000000-0x80000000）或 heap 指针（0x8000000000+）
	//    → 该块即 main .data 段。
	fmt.Println("\n--- 全面链根扫描（所有 commit 块）---")
	cands := arenaCandidates(regs)
	guestLowBase := uintptr(0)
	for _, c := range cands {
		if c == 0x236B2A00000 {
			guestLowBase = c
		}
	}
	if guestLowBase == 0 && len(cands) > 0 {
		guestLowBase = cands[0]
	}
	fmt.Printf("guest 低区映射基址: 0x%014X\n", guestLowBase)
	chainRoots := []struct {
		off  uint64
		name string
	}{{0x02D1EA00, "playerA"}, {0x02CC5DE8, "playerB"}}
	// 按 AllocationBase 合并连续同组块减少读次（仍按 region 粒度足够）
	checked := 0
	hitAny := false
	for _, r := range regs {
		if r.State != memCommit || r.Size <= 0x02D1EA00+8 {
			continue
		}
		for _, cr := range chainRoots {
			d := readMem(h, r.Base+uintptr(cr.off), 8)
			if d == nil || len(d) < 8 {
				continue
			}
			p := binary.LittleEndian.Uint64(d)
			checked++
			if p >= 0x01000000 && p < 0x80000000 {
				host := guestLowBase + uintptr(p)
				dd := readMem(h, host, 16)
				desc := ""
				if dd != nil && len(dd) >= 16 {
					desc = describeData(dd)
				}
				fmt.Printf("  [%s] 块 0x%014X size=%.0fMB prot=0x%X type=0x%X +0x%08X: 低区指针=0x%08X → host 0x%014X: %s\n",
					cr.name, r.Base, float64(r.Size)/(1<<20), r.Prot, r.Type, cr.off, p, host, desc)
				hitAny = true
			} else if p >= 0x8000000000 && p < 0x9000000000 {
				fmt.Printf("  [%s] 块 0x%014X size=%.0fMB prot=0x%X type=0x%X +0x%08X: heap指针=0x%X\n",
					cr.name, r.Base, float64(r.Size)/(1<<20), r.Prot, r.Type, cr.off, p)
				hitAny = true
			}
		}
	}
	fmt.Printf("检查 %d 块次；链根指针命中: %v\n", checked, hitAny)
	fmt.Printf("elapsed %s\n", time.Since(t0).Round(time.Millisecond))
}

// cmdCoords 反查玩家坐标引用者：dump 坐标附近内存 + 低区扫描指向玩家坐标的指针，
// 直方图按 guestVA 偏移聚合，找金手指链根线索（链解不通时的诊断工具）。
func cmdCoords() {
	pid := findPid("eden")
	if pid == 0 {
		fmt.Println("Eden not running - start it and load BOTW first.")
		return
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed (run as admin?).")
		return
	}
	defer procCloseHandle.Call(h)
	t0 := time.Now()

	// 存档锚点（同 cmdChain）
	var anchor *[3]float32
	for _, cand := range edenSaveRoots() {
		if fi, err := osStat(cand); err == nil && fi.IsDir() {
			for _, p := range findSaveFiles(cand, "01007EF00011E000") {
				if a := parseGameData(p); a != nil {
					anchor = a
					break
				}
			}
		}
	}
	if anchor == nil {
		fmt.Println("no save anchor - 用最近 sane 命中（非锚点）继续")
	} else {
		fmt.Printf("save anchor: (%.1f, %.1f, %.1f)\n", anchor[0], anchor[1], anchor[2])
	}

	// sane 全扫 → best base
	blocks := guestBlocks(h)
	saneHits := scanSaneHits(h, blocks, 2000000)
	fmt.Printf("sane hits: %d\n", len(saneHits))
	baseHits := map[uintptr]int{}
	regs := guestRegionsAll(h)
	cands := arenaCandidates(regs)
	for _, s := range saneHits {
		for _, base := range cands {
			if s.Addr > base && s.Addr-base < 0x80000000 {
				baseHits[base]++
			}
		}
	}
	var bestBase uintptr
	bestN := 0
	for b, n := range baseHits {
		if n > bestN {
			bestBase, bestN = b, n
		}
	}
	if bestBase == 0 {
		fmt.Println("no base with low-area hits - 无法反推")
		return
	}
	fmt.Printf("best base: 0x%014X (低区命中 %d)\n", bestBase, bestN)

	// 玩家坐标（距锚点最近）
	var player slotHit
	if anchor != nil {
		bi := 0
		bd := float32(1e9)
		for i, s := range saneHits {
			_, d := minPermDist(s.Mem, *anchor)
			if d < bd {
				bd, bi = d, i
			}
		}
		player = saneHits[bi]
		fmt.Printf("player host=0x%014X mem=(%.2f, %.2f, %.2f) dist=%.0fm\n",
			player.Addr, player.Mem[0], player.Mem[1], player.Mem[2], bd)
	} else {
		player = saneHits[0]
		fmt.Printf("player host=0x%014X mem=(%.2f, %.2f, %.2f) (no anchor)\n",
			player.Addr, player.Mem[0], player.Mem[1], player.Mem[2])
	}
	gv := player.Addr - bestBase
	fmt.Printf("player guestVA=0x%08X\n", gv)

	// dump 玩家坐标附近 ±0x300（16 步进）
	fmt.Println("\n--- 玩家坐标附近内存 ---")
	for off := int64(-0x200); off <= 0x300; off += 16 {
		addr := int64(player.Addr) + off
		d := readMem(h, uintptr(addr), 16)
		marker := ""
		if off == 0 {
			marker = "  <<坐标"
		}
		if d == nil || len(d) < 16 {
			fmt.Printf("  %+5X 0x%014X unreadable\n", off, addr)
			continue
		}
		fmt.Printf("  %+5X 0x%014X %s%s\n", off, addr, describeData(d), marker)
	}

	// 全内存指针反查：u64 ∈ [gv-0x1000, gv+0x1000]，不限低区（main/heap 也可能持有指针）
	fmt.Println("\n--- 全内存指针反查（指向玩家坐标 guestVA±4KB 的 u64）---")
	lo, hi := gv-0x1000, gv+0x1000
	hist := map[uintptr]int{} // 宿主偏移（相对各自块）→ 次数
	limit := 0
	for _, b := range blocks {
		for off := uintptr(0); off < b[1]; off += chunkSize {
			n := uintptr(chunkSize)
			if off+n > b[1] {
				n = b[1] - off
			}
			d := readMem(h, b[0]+off, int(n))
			if d == nil {
				continue
			}
			for k := 0; k+8 <= len(d); k += 8 {
				v := binary.LittleEndian.Uint64(d[k:])
				if v >= uint64(lo) && v <= uint64(hi) {
					hist[b[0]+off+uintptr(k)]++
					limit++
				}
			}
		}
	}
	fmt.Printf("命中指针槽: %d\n", limit)
	type pe struct {
		addr uintptr
		n    int
	}
	var pes []pe
	for a, n := range hist {
		pes = append(pes, pe{a, n})
	}
	sort.Slice(pes, func(i, j int) bool { return pes[i].n > pes[j].n })
	for i, p := range pes {
		if i >= 40 {
			break
		}
		rel := p.addr - bestBase
		region := "低区"
		if p.addr >= bestBase+0x80000000 && p.addr < bestBase+0x100000000 {
			region = "guest堆区"
		} else if p.addr >= bestBase+0x7100000000 {
			region = "main区?"
		}
		fmt.Printf("  host 0x%014X (guestVA? 0x%08X %s) x%d\n", p.addr, rel, region, p.n)
	}
	fmt.Printf("elapsed %s\n", time.Since(t0).Round(time.Millisecond))
}

// botwSaneProbe 内存三元组 sane 判据（BOTW 实测轴序 (X, alt, Z)，与 engine botwSane 同量级）。
func botwSaneProbe(x, alt, z float32) bool {
	if x != x || alt != alt || z != z {
		return false
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	if absf(x) < 5 && absf(z) < 5 {
		return false
	}
	if x <= -7000 || x >= 7000 || z <= -7000 || z >= 7000 || alt <= -600 || alt >= 5000 {
		return false
	}
	if absf(x) < 5 && alt < 5 {
		return false
	}
	if absf(z) < 5 && alt < 5 {
		return false
	}
	return true
}

// scanSaneHits 全内存并发扫 sane 坐标三元组（不限目标值）。按 8MB 分块读（逐 4 字节
// 系统调用会慢几个数量级，probe 首版踩过这个坑）。
func scanSaneHits(h uintptr, blocks [][2]uintptr, max int) []slotHit {
	type job struct {
		base uintptr
		size uintptr
	}
	jobs := make(chan job, len(blocks))
	var mu sync.Mutex
	var hits []slotHit
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				for off := uintptr(0); off < j.size; off += chunkSize {
					n := uintptr(chunkSize)
					if off+n > j.size {
						n = j.size - off
					}
					d := readMem(h, j.base+off, int(n))
					if d == nil {
						continue
					}
					for k := 0; k+12 <= len(d); k += 4 {
						f := floats(d[k:])
						if len(f) < 3 || !botwSaneProbe(f[0], f[1], f[2]) {
							continue
						}
						mu.Lock()
						if len(hits) < max {
							hits = append(hits, slotHit{j.base + off + uintptr(k), [3]float32{f[0], f[1], f[2]}, [3]int{0, 1, 2}, 0})
						}
						mu.Unlock()
					}
				}
			}
		}()
	}
	for _, b := range blocks {
		jobs <- job{b[0], b[1]}
	}
	close(jobs)
	wg.Wait()
	return hits
}

// scanCoordSlots 在 actor 附近扫 12 字节三元组（4 对齐），sane 过滤后与锚点 6 排列比对。
// 半径按 chunk 分块读，避免逐 4 字节系统调用。
func scanCoordSlots(h uintptr, actor uintptr, anchor [3]float32) []slotHit {
	start := actor - slotRadius
	end := actor + slotRadius
	var hits []slotHit
	for off := uintptr(0); start+off < end; off += chunkSize {
		n := uintptr(chunkSize)
		if start+off+n > end {
			n = end - (start + off)
		}
		d := readMem(h, start+off, int(n))
		if d == nil {
			continue
		}
		for k := 0; k+12 <= len(d); k += 4 {
			f := floats(d[k:])
			if len(f) < 3 {
				continue
			}
			t := [3]float32{f[0], f[1], f[2]}
			if absf(t[0]) > 8000 || absf(t[1]) > 8000 || absf(t[2]) > 8000 {
				continue
			}
			if t[0] == 0 && t[1] == 0 && t[2] == 0 {
				continue
			}
			if t[0] == 1 && t[1] == 1 && t[2] == 1 {
				continue
			}
			perm, dmin := minPermDist(t, anchor)
			if dmin < anchorDistOK {
				hits = append(hits, slotHit{start + off + uintptr(k), t, perm, dmin})
			}
		}
	}
	return hits
}

// osStat 包装（避免与 probe_botw.go 的 os.Stat 冲突的写法统一）
func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }
