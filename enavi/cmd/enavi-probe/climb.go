package main

// cmdClimb —— B 路线：从坐标槽反向爬升，找「基址 + 固定偏移」
//
// 背景：§13 的 poshist 方案命中的是**堆地址**，Eden 一重启就变，
// 与 xnavi（扫描版）同类。真正的定位应该是「模块基址 + 固定偏移」。
//
// 方法（不猜偏移，纯实机反查）：
//   1. 用 poshist.ScanWithAnchor 找到当前玩家位置历史缓冲（已知可靠）
//   2. 反查「谁持有指向这个缓冲的指针」→ 逐层往上爬
//   3. ★ 内嵌数组没有指针指向数组中间，只有指针指向**对象起点**
//      → 搜指针时 span 必须覆盖缓冲起点（±0x200）
//   4. 爬到某层的持有者落在**模块区**（arenaLow ~ arenaLow+8GB）
//      → 那就是 PlayerInfo 实例的静态位置 = 「模块基址 + 固定偏移」
//   5. 同时记录每一层的成员偏移，得到完整链路
//
// 产出：模块静态偏移（跨 Eden 会话不变）—— 这正是「重启不用重扫」的关键。
//
// ★ 全程限速：ReadProcessMemory 全速扫会把 Eden 拖死（实测过）。

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"botwmap/enavi/internal/poshist"
	"botwmap/enavi/internal/winmem"
)

// climbSpan 搜指针时向前覆盖的范围。
// 内嵌数组的指针只指向对象起点，所以要把缓冲起点也覆盖进来。
// 实测第一层就 0 引用者 → 说明对象起点离缓冲很远，span 要能覆盖整个宿主对象。
var climbSpan uintptr = 0x200

// climbMaxLevel 最多爬几层
const climbMaxLevel = 8

// climbKeep 每层最多保留多少候选（防爆炸）
const climbKeep = 64

type climbRef struct {
	holder uintptr // 指针所在处（宿主地址）
	target uintptr // 指针值（宿主地址）
}

// climbIsModule 是否落在模块区（arenaLow ~ arenaLow + 8GB）
func climbIsModule(d winmem.Dram, a uintptr) bool {
	return a >= d.ArenaLow && a < d.ArenaLow+0x2000_0000_000
}

// climbFindPtrs 限速扫 regions，找指向 target ± span 的指针。
//
// 三种口径都要试（Eden 内存里指针的存法不唯一）：
//   1. 直接存宿主地址
//   2. 存 guest VA（host - arenaLow）
//   3. 存「指向结构中某处的偏移形式」—— 由 span 覆盖
func climbFindPtrs(p *winmem.Process, d winmem.Dram, regions []winmem.Region,
	target uintptr) []climbRef {

	var mu sync.Mutex
	var out []climbRef

	const chunk = 8 << 20
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += chunk {
			n := chunk
			if remain := int(r.Size - off); remain < n {
				n = remain
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}

	var wg sync.WaitGroup
	ch := make(chan job, 4)
	for i := 0; i < 2; i++ { // 并发压到 2
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				// 限速：每次块读之间 sleep —— 绝不能拖死模拟器
				time.Sleep(12 * time.Millisecond)
				if err != nil || len(buf) < 16 {
					continue
				}
				var local []climbRef
				for off := 0; off+8 <= len(buf); off += 8 {
					v := binary.LittleEndian.Uint64(buf[off:])
					if v == 0 {
						continue
					}
					hv := uintptr(v)
					// ★ 两种口径都要试，且**无条件**都试：
					//   口径 1：v 就是宿主地址（如 0x2736_2FF7E28）
					//   口径 2：v 是 guest VA（如 0x21C7_627E28），转宿主要 +arenaLow
					//   ⚠️ 不能用 `v >= arenaLow` 判断是不是 guest VA ——
					//      arenaLow 是宿主地址（0x27xx…），guest VA 是 0x21xx…，
					//      永远小于 arenaLow，那样判会把第二口径整个关掉。
					if climbInSpan(hv, target) {
						local = append(local, climbRef{j.addr + uintptr(off), hv})
						continue
					}
					if hv < 0x4000_0000_000 { // 只在合理量级内试 guest VA 口径
						h := hv + d.ArenaLow
						if h >= d.ArenaLow && climbInSpan(h, target) {
							local = append(local, climbRef{j.addr + uintptr(off), h})
						}
					}
				}
				if len(local) > 0 {
					mu.Lock()
					out = append(out, local...)
					mu.Unlock()
				}
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

// climbInSpan 是否落在 target ± span 内
func climbInSpan(v, target uintptr) bool {
	if v >= target-climbSpan && v <= target+climbSpan {
		return true
	}
	return false
}

var climbSpanFlag = flag.Uint64("span", 0x200, "搜指针时向前覆盖的字节数（内嵌数组要覆盖到对象起点）")

func cmdClimb(p *winmem.Process) {
	climbSpan = uintptr(*climbSpanFlag)
	if climbSpan < 8 {
		climbSpan = 8
	}
	d, _, err := p.FindDram()
	if err != nil {
		printf("[climb] 找不到 DRAM：%v\n", err)
		return
	}
	runClimb(p, d)
}

func runClimb(p *winmem.Process, d winmem.Dram) {
	printf("\n════════ B 路线：坐标槽 → 反向爬升 → 模块静态偏移 ════════\n\n")

	// ---- 1) 定位当前位置历史缓冲 ----
	var anchor [3]float32
	anchorValid := false
	if a, ok := pickLatestSaveAnchor(p); ok {
		anchor, anchorValid = a, true
		printf("[climb] 存档参照坐标 = (%.2f, %.2f, %.2f)\n", anchor[0], anchor[1], anchor[2])
	} else {
		printf("[climb] ⚠️ 没有存档参照坐标\n")
	}

	printf("[climb] 定位位置历史缓冲…\n")
	bufAddr, err := poshist.ScanWithAnchor(p, anchor, anchorValid)
	if err != nil {
		printf("[climb] 定位缓冲失败：%v\n", err)
		return
	}
	pos, ok := poshist.ReadPos(p, bufAddr)
	if !ok {
		printf("[climb] 读不出缓冲坐标\n")
		return
	}
	printf("[climb] ✓ 缓冲 @ host=0x%X  gva=0x%X\n", bufAddr, bufAddr-d.ArenaLow)
	printf("[climb]   当前坐标 (%.2f, %.2f, %.2f)\n\n", pos[0], pos[1], pos[2])

	// ---- 2) 逐层爬升 ----
	// ★ 搜索范围 = UserHeap + **模块区可写段**。
	//   之前只搜 UserHeap → 第 1 层就 0 引用者，得出"缓冲不被引用"的错误结论。
	//   实际上全局变量（单例指针）在**模块区**，玩家缓冲若被全局指针引用，
	//   引用者只可能在模块区。必须一起搜。
	searchRegions := scanHeapRegions(p, d)
	searchRegions = append(searchRegions, scanModuleRW(p, d)...)
	printf("[climb] 搜索范围：UserHeap + 模块区可写段，共 %d 个区\n", len(searchRegions))

	// cur = 本层要找指向它们的指针的目标集合
	cur := []uintptr{bufAddr}

	for lvl := 1; lvl <= climbMaxLevel; lvl++ {
		var allRefs []climbRef
		seen := make(map[uintptr]bool)
		for _, t := range cur {
			refs := climbFindPtrs(p, d, searchRegions, t)
			for _, r := range refs {
				if seen[r.holder] {
					continue
				}
				seen[r.holder] = true
				allRefs = append(allRefs, r)
			}
			// 限速：每个目标之间也歇一下
			time.Sleep(50 * time.Millisecond)
		}
		if len(allRefs) == 0 {
			printf("[climb] 第 %d 层：没有引用者 —— 爬升到此为止\n", lvl)
			break
		}

		// 分类：落在模块区的（★ 这就是我们要的）/ 落在堆里的（继续爬）
		var inMod, inHeap []climbRef
		for _, r := range allRefs {
			if climbIsModule(d, r.holder) {
				inMod = append(inMod, r)
			} else if r.holder >= d.HeapLo() && r.holder < d.HeapHi() {
				inHeap = append(inHeap, r)
			}
		}
		printf("[climb] 第 %d 层：引用者 %d 个（模块区 %d ★ / 堆内 %d）\n",
			lvl, len(allRefs), len(inMod), len(inHeap))


		// ★ 找到模块区的了
		if len(inMod) > 0 {
			printf("\n[climb] ★★ 爬到模块区了！这就是「基址 + 固定偏移」的入口 ★★\n\n")
			reportClimb(d, bufAddr, inMod)
			return
		}

		// 否则取堆内的继续爬（限制数量防爆炸）
		sort.Slice(inHeap, func(i, j int) bool {
			// 优先 holder 地址小的（通常是常驻对象，不是临时分配）
			return inHeap[i].holder < inHeap[j].holder
		})
		if len(inHeap) > climbKeep {
			inHeap = inHeap[:climbKeep]
		}
		cur = nil
		for _, r := range inHeap {
			cur = append(cur, r.holder)
		}
		printf("[climb]   继续往上爬 %d 个候选…\n", len(cur))
	}

	printf("\n[climb] 爬满 %d 层仍未到模块区。\n", climbMaxLevel)
	printf("[climb] （可能位置缓冲是栈上/临时分配的；或需要换起点）\n")
}

// reportClimb 报告爬到模块区的结果
func reportClimb(d winmem.Dram, bufAddr uintptr, inMod []climbRef) {
	for i, r := range inMod {
		if i >= 10 {
			break
		}
		gva := r.holder - d.ArenaLow
		printf("[climb] ── 候选 %d ──\n", i+1)
		printf("[climb]   模块内静态地址 guest VA = 0x%X\n", gva)
		printf("[climb]   指向的堆对象       = 0x%X\n", r.target)
		// 该对象离缓冲有多远（= 缓冲在对象内的偏移）
		if r.target <= bufAddr {
			printf("[climb]   → 缓冲在该对象内偏移 = 0x%X\n", bufAddr-r.target)
		} else {
			printf("[climb]   → 缓冲在该对象之前 0x%X（可能不是持有者）\n", r.target-bufAddr)
		}
		printf("\n")
	}
	printf("[climb] ★ 下一步：把这个 guest VA 记下来，重启 Eden 后验证它是否不变。\n")
	printf("[climb]   若不变 → 得到「模块基址 + 固定偏移」，定位问题彻底解决。\n")
}

// scanModuleRW 取模块区的可写段（.data/.bss = 全局变量所在）
func scanModuleRW(p *winmem.Process, d winmem.Dram) []winmem.Region {
	var out []winmem.Region
	p.Regions(d.ArenaLow, d.ArenaLow+0x2000_0000_000, func(r winmem.Region) bool {
		if !r.Readable() || r.Size < 4096 {
			return false
		}
		pp := r.Protect & 0xFF
		if pp == 0x04 || pp == 0x40 || pp == 0x08 { // RW / ERW / WRITECOPY
			out = append(out, r)
		}
		return false
	})
	return out
}

// scanHeapRegions 取 UserHeap 的可读区
func scanHeapRegions(p *winmem.Process, d winmem.Dram) []winmem.Region {
	var out []winmem.Region
	p.Regions(d.HeapLo(), d.HeapHi(), func(r winmem.Region) bool {
		if r.Readable() && r.Size >= 4096 {
			out = append(out, r)
		}
		return false
	})
	return out
}

var _ = fmt.Sprintf
var _ = math.Abs