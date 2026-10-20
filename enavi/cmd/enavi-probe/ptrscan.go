// 本文件 = PlayerInfo 深挖工具（指针链爬升 + 模块锚点）。
//
// 目标（老大要的"先找到人，再看身高"）：
//
//	不要在一堆坐标里挑哪个是玩家，而是先认出"玩家这个对象"（PlayerInfo / PlayerBase），
//	再在它内部读坐标。一旦指针链的末端能落到"模块基址 + 固定偏移"，
//	每次启动只需：找模块基址 → 加偏移 → 读 12 字节。零扫描、零参数、零状态。
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"botwmap/enavi/internal/known"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

// zone 一个宿主地址区间，用于标注"这个地址属于内存的哪一块"。
type zone struct {
	name string
	lo   uintptr
	hi   uintptr
}

// memZones 依据实测的 Eden 布局划分区间（2026-10-20）。
// 注意：这些只是"标签"，用于人工判读日志，不参与任何判定逻辑。
func memZones(d winmem.Dram, p *winmem.Process) []zone {
	zs := []zone{
		{"DRAM物理", d.BlockBase, d.BlockBase + d.BlockSize},
		{"模块区", d.ArenaLow, d.HeapLo()},
		{"UserHeap", d.HeapLo(), d.HeapHi()},
	}
	_ = p
	return zs
}

func zoneOf(zs []zone, a uintptr) string {
	for _, z := range zs {
		if a >= z.lo && a < z.hi {
			return z.name
		}
	}
	return "其它"
}

// cmdLayout 打印 Eden 的地址空间划分，并确认"模块区"是否存在、里面有什么。
func cmdLayout(p *winmem.Process) {
	hr("Eden 地址空间布局")
	d, cands, err := p.FindDram()
	if err != nil {
		printf("[layout] %v\n", err)
		return
	}
	printf("[layout] DRAM 块候选 %d 个\n", len(cands))
	printf("[layout] 选中 block=0x%X size=%.2fGB  arenaLow=0x%X\n",
		d.BlockBase, float64(d.BlockSize)/1073741824.0, d.ArenaLow)
	printf("[layout] 换算：host = arenaLow + guestVA\n")
	printf("          物理视图 = blockBase + phys\n\n")

	zs := memZones(d, p)
	for _, z := range zs {
		var cnt int
		var total uint64
		var maxR winmem.Region
		p.Regions(z.lo, z.hi, func(r winmem.Region) bool {
			if r.State == winmem.MemCommit {
				cnt++
				total += uint64(r.Size)
				if r.Size > maxR.Size {
					maxR = r
				}
			}
			return false
		})
		printf("[layout] %-10s 0x%X ~ 0x%X : %d 个已提交区 / %.2fGB  最大块 %s\n",
			z.name, z.lo, z.hi, cnt, float64(total)/1073741824.0, maxR)
	}

	// 模块区里前 20 个可读区（期望能看到 IMAGE 特征的模块）
	printf("\n[layout] 「模块区」(arenaLow ~ arenaLow+0x2000000000) 前 20 个可读区：\n")
	n := 0
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.Readable() {
			printf("   %s\n", r)
			n++
		}
		return n >= 20
	})
	if n == 0 {
		printf("   （空 —— 说明模块不在这个区间，需要换思路找模块基址）\n")
	}

	// 找 `1.6.0` 的物理偏移（上一轮实测存在于 DRAM 内，可能是个稳定锚点）
	printf("\n[layout] 在 DRAM 内定位版本串 \"1.6.0\"（潜在模块锚点）：\n")
	found := findVersionAnchor(p, d)
	if found == 0 {
		printf("   未找到\n")
	} else {
		printf("   phys=+0x%X  host=0x%X\n", found-d.BlockBase, found)
		printf("   ★ 若重启 Eden 后该偏移不变，它就是模块锚点（L1 的基石）\n")
	}
}

// findVersionAnchor 在 DRAM 里找 "1.6.0" 的宿主地址。
func findVersionAnchor(p *winmem.Process, d winmem.Dram) uintptr {
	var hit uintptr
	const chunk = 8 << 20
	for off := uintptr(0); off < d.BlockSize && hit == 0; off += chunk {
		n := chunk
		if remain := int(d.BlockSize - off); remain < n {
			n = remain
		}
		buf, err := p.Read(d.BlockBase+off, n)
		if err != nil {
			continue
		}
		if a := scan.FindASCII(buf, d.BlockBase+off, "1.6.0"); len(a) > 0 {
			hit = a[0]
		}
	}
	return hit
}

// ptrRef 一条"某处存着指向目标的指针"的记录。
type ptrRef struct {
	Holder uintptr // 指针本身所在的地址
	Value  uintptr // 指针的值（指向目标）
	Offset int64   // Value - Target
}

// cmdPtrScan 指针链爬升：从玩家坐标地址出发，逐层找"谁指向它"，
// 直到爬到 DRAM / 模块区（= 静态锚点）。
//
// 判定"静态"的口径：持有指针的地址落在 DRAM 物理视图或模块区。
// 一旦某层出现这种地址，说明"模块基址 + (该地址 - 模块基址)"是固定偏移，
// 下次启动就能直接算出来，不用再扫坐标。
func cmdPtrScan(p *winmem.Process) {
	hr("指针链爬升（从坐标地址反查 PlayerInfo / 静态锚点）")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[ptr] %v\n", err)
		return
	}
	zs := memZones(d, p)

	// 起点：known 缓存里的坐标地址（已验证可读、且坐标合理）
	var start uintptr
	if *addrFlag != "" {
		if a, e := parseAddr(*addrFlag); e == nil {
			start = a
		}
	}
	if start == 0 {
		store := known.Load()
		e, ok := store.Pick(p.PID, p.StartedAt())
		if !ok && len(store.Entries) > 0 {
			e, ok = store.Entries[0], true
		}
		if !ok {
			sayln("[ptr] 没有已知坐标地址。先跑一次 enavi.exe（会自动扫描并写 known_player.json），")
			sayln("     或用 -addr 0x... 指定。")
			return
		}
		start = e.Addr
		printf("[ptr] 起点来自 known 缓存：0x%X  pos=(%.1f, %.1f, %.1f)\n",
			e.Addr, e.Pos[0], e.Pos[1], e.Pos[2])
	} else {
		printf("[ptr] 起点来自 -addr：0x%X\n", start)
	}

	if v, ok := scan.ReadVec3(p, start); ok {
		printf("[ptr] 起点坐标校验：(%.1f, %.1f, %.1f) ✓\n", v.X, v.Y, v.Z)
	} else {
		printf("[ptr] ⚠ 起点读不出合法坐标，可能地址已失效\n")
	}

	// 扫描范围：User Heap + 模块区 + DRAM（指针可能在任何地方）
	var regions []winmem.Region
	for _, z := range []zone{zs[1], zs[2], zs[0]} { // 模块区 / UserHeap / DRAM
		p.Regions(z.lo, z.hi, func(r winmem.Region) bool {
			if r.Readable() && r.Size >= 4096 {
				regions = append(regions, r)
			}
			return false
		})
	}
	var total uint64
	for _, r := range regions {
		total += uint64(r.Size)
	}
	printf("[ptr] 搜索范围 %d 区 / %.2fGB\n", len(regions), float64(total)/1073741824.0)

	// ── 第 1 层：谁指向"坐标地址" ──
	const span = 0x2000 // 允许指针指向坐标 ±8KB（结构体内偏移）
	t0 := time.Now()
	l1 := findPointers(p, regions, start, span, d.ArenaLow)
	printf("[ptr] 第1层（指向坐标 0x%X ±0x%X 的指针）：%d 条，耗时 %v\n",
		start, span, len(l1), time.Since(t0).Round(time.Millisecond))

	printRefs(zs, l1, 20, start)

	// 如果第 1 层就有静态区命中，直接报告
	if statics := filterZone(zs, l1, "DRAM物理", "模块区"); len(statics) > 0 {
		printf("\n[ptr] ★ 第1层就出现静态区引用 %d 条：\n", len(statics))
		for i, r := range statics {
			if i >= 10 {
				break
			}
			printf("   持有者 0x%X（%s） → 目标+%+d\n", r.Holder, zoneOf(zs, r.Holder), r.Offset)
		}
	}

	// ── 第 2 层：谁指向第 1 层的持有者 ──
	// 只取"看起来像结构体成员"的候选：offset 小、且指向的对象里有合法内容
	holders := topHolders(l1, 40)
	var l2 []ptrRef
	seen := map[uintptr]bool{}
	for _, h := range holders {
		refs := findPointers(p, regions, h.Holder, span, d.ArenaLow)
		for _, r := range refs {
			if seen[r.Holder] {
				continue
			}
			seen[r.Holder] = true
			l2 = append(l2, r)
		}
	}
	printf("\n[ptr] 第2层（指向第1层持有者的指针）：%d 条\n", len(l2))
	printRefs(zs, l2, 20, 0)

	// ── 第 3 层 ──
	holders2 := topHolders(l2, 30)
	var l3 []ptrRef
	seen = map[uintptr]bool{}
	for _, h := range holders2 {
		refs := findPointers(p, regions, h.Holder, span, d.ArenaLow)
		for _, r := range refs {
			if seen[r.Holder] {
				continue
			}
			seen[r.Holder] = true
			l3 = append(l3, r)
		}
	}
	printf("\n[ptr] 第3层：%d 条\n", len(l3))
	printRefs(zs, l3, 20, 0)

	// ── 静态锚点汇总 ──
	sayln("\n================ 静态锚点汇总 ================")
	for lvl, refs := range map[int][]ptrRef{1: l1, 2: l2, 3: l3} {
		st := filterZone(zs, refs, "DRAM物理", "模块区")
		if len(st) == 0 {
			continue
		}
		printf("第%d层静态引用 %d 条：\n", lvl, len(st))
		for i, r := range st {
			if i >= 8 {
				printf("   …\n")
				break
			}
			printf("   0x%X（%s）→ +%+d   指针值=0x%X\n",
				r.Holder, zoneOf(zs, r.Holder), r.Offset, r.Value)
		}
	}
	sayln("=============================================")
	sayln("判读：若上面出现「DRAM物理」或「模块区」的地址，记下它的 host 值与 offset；")
	sayln("     重启 Eden 后再跑本命令，若 (该 host - DRAM块基址) 保持不变 → 它就是可用的静态锚点。")
}

// findPointers 找出所有"值落在 target±span 范围内"的 8 字节位置（允许 4 字节对齐）。
//
// ★ 关键（2026-10-20 踩坑）：游戏内部指针存的是 **guest 虚拟地址**（如 0x21C6…），
// 而我们拿到的坐标地址是 **宿主地址**（0x2A42… = arenaLow + gva）。
// 只按宿主地址搜会得到 0 条 —— 必须同时按 guest VA 口径搜。
func findPointers(p *winmem.Process, regions []winmem.Region, target uintptr, span int64, arenaLow uintptr) []ptrRef {
	var out []ptrRef
	var mu sync.Mutex
	// 两种口径：宿主地址、guest VA（= host - arenaLow，仅当 host > arenaLow 时有效）
	var los, his []int64
	los = append(los, int64(target)-span)
	his = append(his, int64(target)+span)
	if target > arenaLow {
		g := target - arenaLow
		los = append(los, int64(g)-span)
		his = append(his, int64(g)+span)
	}

	const chunk = 8 << 20
	const overlap = 16
	workers := 8
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range regions {
		for off := uintptr(0); off < r.Size; off += chunk {
			n := chunk
			if remain := int(r.Size - off); remain < n {
				n = remain
			}
			if uintptr(n) < r.Size-off {
				n += overlap
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}

	ch := make(chan job, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				if err != nil || len(buf) < 16 {
					continue
				}
				var local []ptrRef
				for i := 0; i+8 <= len(buf); i += 4 {
					v := binary.LittleEndian.Uint64(buf[i:])
					iv := int64(v)
					matched := false
					for k := range los {
						if iv >= los[k] && iv <= his[k] {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
					local = append(local, ptrRef{
						Holder: j.addr + uintptr(i),
						Value:  uintptr(v),
						Offset: iv - int64(target),
					})
				}
				if len(local) == 0 {
					continue
				}
				mu.Lock()
				out = append(out, local...)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return out
}

func filterZone(zs []zone, refs []ptrRef, names ...string) []ptrRef {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []ptrRef
	for _, r := range refs {
		if want[zoneOf(zs, r.Holder)] {
			out = append(out, r)
		}
	}
	return out
}

// topHolders 按 |offset| 升序（结构体成员偏移通常很小）取前 n 条。
func topHolders(refs []ptrRef, n int) []ptrRef {
	cp := make([]ptrRef, len(refs))
	copy(cp, refs)
	sort.Slice(cp, func(i, j int) bool {
		return absI64(cp[i].Offset) < absI64(cp[j].Offset)
	})
	if n < len(cp) {
		cp = cp[:n]
	}
	return cp
}

func printRefs(zs []zone, refs []ptrRef, n int, _ uintptr) {
	if len(refs) == 0 {
		return
	}
	// 按所在区间分组统计
	count := map[string]int{}
	for _, r := range refs {
		count[zoneOf(zs, r.Holder)]++
	}
	printf("   分布：")
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		printf("%s=%d  ", k, count[k])
	}
	printf("\n")
	if n > len(refs) {
		n = len(refs)
	}
	sort.Slice(refs, func(i, j int) bool { return absI64(refs[i].Offset) < absI64(refs[j].Offset) })
	for i := 0; i < n; i++ {
		r := refs[i]
		printf("   #%-3d 持有者 0x%-14X（%-8s） 值=0x%-14X offset=%+d\n",
			i+1, r.Holder, zoneOf(zs, r.Holder), r.Value, r.Offset)
	}
}

func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// 让 math 被用到（保留给后续扩展的坐标校验）。
var _ = math.Abs
var _ = fmt.Sprintf
