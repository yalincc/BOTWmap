// 本文件 = 「先找到人」扫描器：按对象类型（vtable）认出玩家，再在对象内读坐标。
//
// 为什么这条路对（2026-10-20 实测对比）：
//
//	✗ 在一堆坐标里挑：2.69GB 里 2727 万个"三 float 合法"的假坐标，挑不出来。
//	✗ 从坐标地址反爬指针：实测 0 条引用 —— 坐标是值拷贝/数组元素，不是被指针指向的对象。
//	✓ 按对象类型认：每个 Actor 对象头第一个 8 字节是 vtable 指针（指向模块只读段的
//	  类函数表）。**玩家类型只有一个实例**，它的 vtable 出现次数必然是全场最少的那批。
//	  认出对象后，坐标在对象内固定偏移处，读它就行 —— 这才是"先找到人再看身高"。
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	knownpkg "botwmap/enavi/internal/known"
	"botwmap/enavi/internal/saveanchor"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

// objCand 一个"对象头"候选：首字段是指向模块区的指针（vtable）。
type objCand struct {
	Addr   uintptr // 对象起始地址（宿主）
	VTable uintptr // 首字段值（宿主口径）
	VGuest uintptr // 换算成 guest VA 的 vtable（便于判读）
}

// cmdActorScan 扫描所有"带 vtable 的对象"，按 vtable 分类统计，找出唯一实例的类型。
//
// 判据（无参数，全是硬条件）：
//  1. 对象头第一个 8 字节落在模块区（= 代码/只读数据段），说明是 vtable 指针；
//  2. 该 vtable 值在全场的出现次数 = 1~3（单例/极少实例）；
//  3. 对象内 ±4KB 能找到合法世界坐标，且接近存档锚点。
func cmdActorScan(p *winmem.Process) {
	hr("对象类型扫描（按 vtable 找玩家对象）")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[actor] %v\n", err)
		return
	}
	zs := memZones(d, p)

	// 锚点（存档坐标），用于挑出"在玩家附近"的对象
	var anchor scan.Vec3
	if anchors, _ := saveanchor.Find(p.ExePath()); len(anchors) > 0 {
		anchor = scan.Vec3{X: anchors[0].Pos[0], Y: anchors[0].Pos[1], Z: anchors[0].Pos[2]}
		printf("[actor] 存档锚点 (%.1f, %.1f, %.1f)  %s\n", anchor.X, anchor.Y, anchor.Z, anchors[0].AttrString())
	} else {
		sayln("[actor] 没有存档锚点，无法按「离玩家近」筛选（仍会输出 vtable 统计）")
	}

	// vtable 合法范围 = 「真实已提交的模块区/DRAM 区间」。
	// ★ 2026-10-20 踩坑：一开始用"arenaLow ~ arenaLow+0x2000000000"这种大范围判断，
	// 那个范围有 137GB、其中只有 0.12GB 真有数据 → 随机值也能通过，捞出 821 种假 vtable。
	// 必须收窄成"实际提交的区间列表"才准。
	// ★ 只认「模块区里 prot=PAGE_READONLY 的段」= 代码段 + .rodata，vtable 就住在这里。
	// 不把 DRAM 物理视图算进来：那是 4GB 全提交，任何值都能"落在里面"，等于没有判据
	// （实测把 DRAM 算进去 → 候选从 8 个暴涨到 190 万个）。
	var modRanges [][2]uintptr
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.State == winmem.MemCommit && r.Size >= 0x10000 && (r.Protect&0xFF) == 0x02 {
			modRanges = append(modRanges, [2]uintptr{r.Base, r.Base + r.Size})
		}
		return false
	})
	var modBytes uint64
	for _, r := range modRanges {
		modBytes += uint64(r[1] - r[0])
	}
	printf("[actor] 模块区实提交区间 %d 个 / %.2fGB（vtable 必须落在这里）\n",
		len(modRanges), float64(modBytes)/1073741824.0)

	inMod := func(u uintptr) bool {
		for _, r := range modRanges {
			if u >= r[0] && u < r[1] {
				return true
			}
		}
		return false
	}
	// ★ 两种口径都认（2026-10-20 踩坑）：
	//   host 口径：内存里存的是宿主地址（0x28xx…）
	//   gva  口径：内存里存的是 guest 虚拟地址（0x8xx… 或 0x19xx…），宿主地址 = gva + arenaLow
	// 只认其中一种会漏掉真对象 / 收进假对象，必须双认。
	isVTable := func(v uint64) bool {
		u := uintptr(v)
		if u%8 != 0 {
			return false
		}
		if inMod(u) {
			return true
		}
		if inMod(u + d.ArenaLow) {
			return true
		}
		return false
	}
	// 显示用：把值换算成宿主地址（便于人看）
	toHost := func(v uintptr) uintptr {
		if inMod(v) {
			return v
		}
		return v + d.ArenaLow
	}

	regions := scan.HeapRegions(p, d, 64<<10)
	printf("[actor] 扫 User Heap %d 区，对象头判据：首字段 ∈ 模块区或 DRAM\n", len(regions))

	t0 := time.Now()
	var cands []objCand
	var mu sync.Mutex
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		var local []objCand
		for off := 0; off+16 <= len(buf); off += 4 {
			v := binary.LittleEndian.Uint64(buf[off:])
			if !isVTable(v) {
				continue
			}
			// 第二个字段应该也是指针或 0（过滤纯数据巧合）
			v2 := binary.LittleEndian.Uint64(buf[off+8:])
			if v2 != 0 && !isVTable(v2) {
				continue
			}
			h := toHost(uintptr(v))
			local = append(local, objCand{
				Addr:   addr + uintptr(off),
				VTable: h,             // 统一成宿主地址
				VGuest: h - d.ArenaLow, // 对应的 guest VA
			})
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		cands = append(cands, local...)
		mu.Unlock()
	})
	printf("[actor] 对象头候选 %d 个，耗时 %v\n", len(cands), time.Since(t0).Round(time.Millisecond))

	if len(cands) == 0 {
		sayln("[actor] 一个都没有 → vtable 不在模块区/DRAM，需要换判据（试 -vtlo/-vthi 手工指定）")
		return
	}

	// 按 vtable 分组统计
	byVt := map[uintptr][]objCand{}
	for _, c := range cands {
		byVt[c.VTable] = append(byVt[c.VTable], c)
	}
	type vtStat struct {
		vt    uintptr
		count int
	}
	var stats []vtStat
	for vt, list := range byVt {
		stats = append(stats, vtStat{vt, len(list)})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].count < stats[j].count })
	printf("[actor] 共有 %d 种 vtable。出现次数最少的 15 种：\n", len(stats))
	printf("   %-4s %-16s %-8s %s\n", "#", "vtable(guest VA)", "实例数", "对象地址")
	for i, st := range stats {
		if i >= 15 {
			break
		}
		list := byVt[st.vt]
		addrStr := ""
		for k, c := range list {
			if k >= 3 {
				addrStr += " …"
				break
			}
			addrStr += fmt.Sprintf("0x%X ", c.Addr)
		}
		printf("   %-4d 0x%-14X %-8d %s\n", i+1, st.vt, st.count, addrStr)
	}

	// 对"实例少"的类型，在对象内找坐标，看是否靠近存档点
	printf("\n[actor] 单例/少例类型中有合法坐标且靠近存档点的对象：\n")
	printf("   %-14s %-16s %-30s %-8s %s\n", "对象地址", "vtable(guest)", "坐标", "对象内偏移", "距存档")
	type nice struct {
		c      objCand
		pos    scan.Vec3
		posOff int
		dist   float64
	}
	var found []nice
	for _, st := range stats {
		if st.count > 4 {
			break // 只看单例/极少实例
		}
		for _, c := range byVt[st.vt] {
			if pos, off, ok := findPosInObject(p, c.Addr, 8192); ok {
				dd := pos.Dist3D(anchor)
				if !anchor.Valid() || dd < 500 {
					found = append(found, nice{c: c, pos: pos, posOff: off, dist: dd})
				}
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].dist < found[j].dist })
	for i, f := range found {
		if i >= 20 {
			printf("   …（省略 %d 个）\n", len(found)-20)
			break
		}
		printf("   %-14s 0x%-14X (%9.1f,%8.1f,%9.1f) %-8d %.1fm\n",
			fmt.Sprintf("0x%X", f.c.Addr), f.c.VTable,
			f.pos.X, f.pos.Y, f.pos.Z, f.posOff, f.dist)
	}
	if len(found) == 0 {
		sayln("   （无）")
	}

	sayln("\n[actor] 下一步：选中候选后跑 -cmd ptrscan -addr <对象地址>，看能否爬到静态锚点；")
	sayln("         或直接验证：对象地址 + 上面那个「对象内偏移」= 坐标，重启后是否仍成立。")
	_ = zs
}

// findPosInObject 在对象起始 addr 之后的 span 字节内找合法世界坐标，
// 返回坐标与相对对象头的偏移。
func findPosInObject(p *winmem.Process, addr uintptr, span int) (scan.Vec3, int, bool) {
	buf, err := p.Read(addr, span)
	if err != nil || len(buf) < 12 {
		return scan.Vec3{}, 0, false
	}
	for off := 0; off+12 <= len(buf); off += 4 {
		v := vecAt(buf, off)
		if v.Valid() {
			return v, off, true
		}
	}
	return scan.Vec3{}, 0, false
}

// cmdBackScan 从某个"内嵌结构地址"往回找结构头（vtable 指针）。
//
// 用途（2026-10-20）：找到了玩家的逐帧位置缓冲（16B/帧，含 0.033s 帧间隔），
// 但没有任何指针指向它 → 说明它是某个大结构的内嵌成员。
// 那么结构头一定在更低地址，且头部会有 vtable（指向模块只读段的 8 字节）。
func cmdBackScan(p *winmem.Process) {
	hr("往回找结构头（vtable）")
	addr, err := parseAddr(*addrFlag)
	if err != nil {
		printf("[back] 需要 -addr 0x...（已知的内嵌成员地址）\n")
		return
	}
	d, _, err := p.FindDram()
	if err != nil {
		printf("[back] %v\n", err)
		return
	}

	// 模块只读段（代码 + .rodata），vtable 只能在这里
	var modRanges [][2]uintptr
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.State == winmem.MemCommit && r.Size >= 0x10000 && (r.Protect&0xFF) == 0x02 {
			modRanges = append(modRanges, [2]uintptr{r.Base, r.Base + r.Size})
		}
		return false
	})
	inMod := func(u uintptr) bool {
		for _, r := range modRanges {
			if u >= r[0] && u < r[1] {
				return true
			}
		}
		return false
	}
	// 双口径：host 或 guest VA
	asMod := func(v uint64) (uintptr, bool) {
		u := uintptr(v)
		if u%8 != 0 {
			return 0, false
		}
		if inMod(u) {
			return u, true
		}
		if inMod(u + d.ArenaLow) {
			return u + d.ArenaLow, true
		}
		return 0, false
	}

	const span = 128 << 10 // 往回 128KB
	start := addr - span
	buf, err := p.Read(start, span)
	if err != nil {
		printf("[back] 读取失败: %v\n", err)
		return
	}
	printf("[back] 基点 0x%X，往回扫 %dKB 找 vtable 候选\n", addr, span/1024)

	type cand struct {
		off int
		vt  uintptr
	}
	var cands []cand
	for i := 0; i+8 <= len(buf); i += 8 {
		if vt, ok := asMod(binary.LittleEndian.Uint64(buf[i:])); ok {
			cands = append(cands, cand{off: i - span, vt: vt})
		}
	}
	printf("[back] vtable 候选 %d 个（前 15 个最靠近基点的）：\n", len(cands))
	// 已按地址升序，取 offset 最接近 0 的（即最靠近基点）
	if len(cands) > 15 {
		cands = cands[len(cands)-15:]
	}
	for _, c := range cands {
		printf("   偏移 %+7d  0x%X  → 结构头候选 0x%X\n", c.off, c.vt, addr+uintptr(c.off))
	}
	if len(cands) == 0 {
		sayln("[back] 没有 → 结构头可能超过 128KB，或该结构没有 vtable（纯数据）")
		return
	}

	// 对最近的 vtable 位置，打印它后面 96 字节（结构头字段）
	best := cands[len(cands)-1]
	head := addr + uintptr(best.off)
	printf("\n[back] 最近的结构头候选 0x%X，其字段：\n", head)
	dbuf, err := p.Read(head, 96)
	if err != nil {
		printf("   读取失败: %v\n", err)
		return
	}
	printf("    偏移  float32×4                                    int64（指针候选）\n")
	for i := 0; i+16 <= len(dbuf); i += 16 {
		f := [4]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(dbuf[i:])),
			math.Float32frombits(binary.LittleEndian.Uint32(dbuf[i+4:])),
			math.Float32frombits(binary.LittleEndian.Uint32(dbuf[i+8:])),
			math.Float32frombits(binary.LittleEndian.Uint32(dbuf[i+12:])),
		}
		p1 := binary.LittleEndian.Uint64(dbuf[i:])
		p2 := binary.LittleEndian.Uint64(dbuf[i+8:])
		mark := " "
		if i == 0 {
			mark = "★"
		}
		printf("  %s+%-4d %12.3f %12.3f %12.3f %12.3f  0x%X 0x%X\n",
			mark, i, f[0], f[1], f[2], f[3], p1, p2)
	}
	printf("\n[back] 位置缓冲相对结构头偏移 = +%d（十进制 %d）\n", addr-head, addr-head)
	sayln("[back] 下一步：用 -cmd ptrscan -addr <结构头地址> 看能否爬到静态锚点。")
}

// cmdOwnerScan 从"内嵌成员地址"往回找"持有游戏对象指针的位置"，
// 并验证指针指向的对象里是否含玩家坐标。
//
// 用途（2026-10-20）：找到逐帧位置缓冲后，往回 128KB 没有 vtable →
// 说明承载它的结构没有虚表（PlayerInfo 很可能是普通类）。
// 那就换个判据：该结构里一定存着 mPlayerActor / mPlayerLink 这类**指向 heap 对象的指针**。
// 找到这些指针 → 顺着读那个对象 → 里面若有玩家坐标，就锁定了 Player 实体。
func cmdOwnerScan(p *winmem.Process) {
	hr("找持有者（含对象指针的结构）")
	addr, err := parseAddr(*addrFlag)
	if err != nil {
		printf("[owner] 需要 -addr 0x...\n")
		return
	}
	d, _, err := p.FindDram()
	if err != nil {
		printf("[owner] %v\n", err)
		return
	}
	heapLo, heapHi := d.HeapLo(), d.HeapHi()

	// 玩家当前位置（known 里的实时读），作为"这个对象是不是玩家"的判据
	var playerNow scan.Vec3
	hasPlayer := false
	if e, ok := knownpkg.Load().Pick(p.PID, p.StartedAt()); ok {
		if v, valid := scan.ReadVec3(p, e.Addr); valid {
			playerNow, hasPlayer = v, true
		}
	}
	if hasPlayer {
		printf("[owner] 玩家当前位置 (%.1f, %.1f, %.1f)\n", playerNow.X, playerNow.Y, playerNow.Z)
	}

	const span = 256 << 10
	start := addr - span
	buf, err := p.Read(start, span+4096)
	if err != nil {
		printf("[owner] 读取失败: %v\n", err)
		return
	}
	printf("[owner] 基点 0x%X，往回扫 %dKB 找指向 UserHeap 的指针\n", addr, span/1024)

	type hit struct {
		off    int
		ptr    uintptr
		pos    scan.Vec3
		posOff int
		dist   float64
	}
	var hits []hit
	seen := map[uintptr]bool{}
	for i := 0; i+8 <= len(buf); i += 8 {
		v := uintptr(binary.LittleEndian.Uint64(buf[i:]))
		// 双口径：host 或 guest VA
		target := uintptr(0)
		if v >= heapLo && v < heapHi {
			target = v
		} else if v+d.ArenaLow >= heapLo && v+d.ArenaLow < heapHi {
			target = v + d.ArenaLow
		}
		if target == 0 || seen[target] {
			continue
		}
		seen[target] = true
		// 读目标对象前 2KB，看里面有没有坐标
		if pos, posOff, ok := findPosInObject(p, target, 2048); ok {
			dd := 0.0
			if hasPlayer {
				dd = pos.Dist3D(playerNow)
			}
			hits = append(hits, hit{off: i - span, ptr: target, pos: pos, posOff: posOff, dist: dd})
		}
	}

	printf("[owner] 候选对象指针 %d 个（指向的对象里能找到合法坐标）\n", len(hits))
	if hasPlayer {
		sort.Slice(hits, func(i, j int) bool { return hits[i].dist < hits[j].dist })
		printf("\n  %-9s %-14s %-30s %-8s %s\n", "偏移", "指向的对象", "对象内坐标", "对象内偏移", "距玩家")
		for i, h := range hits {
			if i >= 15 {
				printf("  …（省略 %d 个）\n", len(hits)-15)
				break
			}
			mark := " "
			if h.dist < 5 {
				mark = "★"
			}
			printf(" %s%-9d %-14s (%9.1f,%8.1f,%9.1f) %-8d %.1fm\n",
				mark, h.off, fmt.Sprintf("0x%X", h.ptr), h.pos.X, h.pos.Y, h.pos.Z, h.posOff, h.dist)
		}
		near := 0
		for _, h := range hits {
			if h.dist < 5 {
				near++
			}
		}
		printf("\n[owner] ★ 指向「坐标在玩家 5m 内」的对象指针：%d 个\n", near)
		if near > 0 {
			printf("[owner] → 这些指针所在的位置（基点%+d 附近）= PlayerInfo 内的 mPlayerActor/mPlayerLink 字段；\n", hits[0].off)
			printf("        那个被指向的对象就是 Player 实体，结构头 0x%X。\n", hits[0].ptr)
		}
	} else {
		for i, h := range hits {
			if i >= 15 {
				break
			}
			printf("  偏移 %+7d → 0x%X  坐标(%.1f,%.1f,%.1f) @+%d\n",
				h.off, h.ptr, h.pos.X, h.pos.Y, h.pos.Z, h.posOff)
		}
	}
}
