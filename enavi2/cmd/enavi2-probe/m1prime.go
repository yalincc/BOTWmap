// M1'：User Heap 位置历史缓冲指纹扫描验证（enavi2 方向修正后的第一里程碑）。
//
// 判据来源：enavi/internal/poshist（2026-10-20 实测定案）——
//   BOTW 1.6.0 玩家位置历史缓冲，元素步长 0x28（40 字节）：
//     +0x00 Vector3f 位置 (X, alt, Z)
//     +0x0C float w ∈ (0.90, 1.00]   ← 朝向四元数 w
//     +0x10 float x ∈ (-0.5, 0.5)
//     +0x14 float y ∈ (-0.5, 0.5)
//     +0x18 float 朝向角（单位：度）∈ (-180, 180)
//     基址 +0x28 = elem1（当前帧），+0x50 = elem2（上一帧），其余槽空。
//
// 本命令只验证一件事：这套指纹在 User Heap 里命中是否唯一/可筛，
// 以及"会变验证"（间隔 1.5s 坐标变化 > 0.5m）能否进一步收敛。
// 用位模式预筛加速（浮点位模式对正数单调）。
package main

import (
	"encoding/binary"
	"flag"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"botwmap/enavi2/internal/winmem"
)

var (
	m1pWait   = flag.Duration("vwait", 1500*time.Millisecond, "会变验证等待间隔")
	anchorXY  = flag.String("anchor", "", "锚点坐标 x,z（排序用，如 375.7,1025.4；空=不排序）")
	dumpAddr  = flag.String("dumpaddr", "", "读宿主地址 0x50B（逗号分隔多个；每个读两遍间隔 400ms 看变化）")
	dumpN     = flag.Int("dumpslots", 8, "dump 的 0x28 槽数量（默认 8 槽 = 0x140 字节历史）")
	m1pCand1  = flag.Bool("cand1", false, "候选判据检查 elem1(base+0x28) 而非 elem0（玩家缓冲可能 elem0 为空槽）")
	m1pMaxDist = flag.Float64("maxdist", 0, "只显示距锚点该距离(米)内的候选；0=全部（需 -anchor）")
)

// m1pCand 一个候选元素起点（指向 elem0 基址，elem1 = base+0x28）。
type m1pCand struct {
	base uintptr
	pos  [3]float32
	quat int // 连续带指纹元素数
}

func cmdM1P() {
	hr("M1' User Heap 位置历史缓冲指纹验证")

	var pid uint32
	if *pidFlag > 0 {
		pid = uint32(*pidFlag)
	} else {
		pids := winmem.FindPIDs("eden")
		if len(pids) == 0 {
			printf("[attach] 没找到 eden.exe，请先启动 Eden 并进游戏\n")
			return
		}
		pid = pids[0]
	}
	p, err := winmem.Open(pid)
	if err != nil {
		printf("[attach] Open 失败：%v\n", err)
		return
	}
	defer p.Close()
	printf("[attach] pid=%d %s\n", pid, p.ExePath())

	if *dumpAddr != "" {
		for _, a := range strings.Split(*dumpAddr, ",") {
			ad, err := strconv.ParseUint(strings.TrimSpace(a), 0, 64)
			if err != nil || ad == 0 {
				printf("[dump] 地址解析失败：%s\n", a)
				continue
			}
			dumpSlots(p, uintptr(ad), *dumpN)
		}
	}

	d, _, err := p.FindDram()
	if err != nil {
		printf("[dram] %v\n", err)
		return
	}
	printf("[dram] arenaLow=0x%X UserHeap guest [0x%X,0x%X) → host [0x%X,0x%X)\n",
		d.ArenaLow, d.HeapLo()-d.ArenaLow, d.HeapHi()-d.ArenaLow, d.HeapLo(), d.HeapHi())

	var ax, az float64
	anchorValid := false
	if *anchorXY != "" {
		parts := strings.Split(*anchorXY, ",")
		if len(parts) != 2 {
			printf("[anchor] 解析失败（格式 x,z）\n")
			return
		}
		ax, _ = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		az, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if ax == 0 && az == 0 {
			printf("[anchor] 锚点无效（0,0）\n")
			return
		}
		anchorValid = true
		printf("[anchor] 锚点 (%.1f, %.1f)\n", ax, az)
	}

	regions := heapRegions(p, d)
	if len(regions) == 0 {
		printf("[heap] User Heap 无块，Eden 未载入游戏？\n")
		return
	}
	var total uint64
	for _, r := range regions {
		total += uint64(r.Size)
	}
	printf("[heap] User Heap 可读写块 %d 个，合计 %.2f GB\n", len(regions), float64(total)/1073741824.0)

	// 扫描（限速，保护 Eden）
	var cands []m1pCand
	var mu sync.Mutex
	runChunksL(p, regions, *workers, *scanDelay, func(addr uintptr, buf []byte) {
		var local []m1pCand
		if *m1pCand1 {
			// elem1 模式：base 起 +0x28 槽作为候选槽（elem0 可能为空槽漏判）
			for off := 0; off+0x28+0x24+4 <= len(buf); off += 4 {
				e1 := off + 0x28
				if !hasQuatBits(buf[e1:]) {
					continue
				}
				if binary.LittleEndian.Uint32(buf[e1+0x1C:]) != 0 {
					continue
				}
				// 位置历史缓冲强特征：+0x24 flags=0x21（enavi 实测）
				if binary.LittleEndian.Uint32(buf[e1+0x24:]) != 0x21 {
					continue
				}
				p1 := f3(buf[e1:])
				if !worldCoordOK(p1[0], p1[1], p1[2]) {
					continue
				}
				local = append(local, m1pCand{addr + uintptr(off), p1, 1})
			}
		} else {
			for off := 0; off+0x28+0x1C <= len(buf); off += 4 {
				if !hasQuatBits(buf[off:]) {
					continue
				}
				// +0x1C 保留位必须为 0（enavi 实测结构：位置历史缓冲槽 +0x1C 恒 0；
				// 2026-10-21 M1' 二轮新增：过滤单位模长判据带来的大量假槽）
				if binary.LittleEndian.Uint32(buf[off+0x1C:]) != 0 {
					continue
				}
				p0 := f3(buf[off:])
				if !worldCoordOK(p0[0], p0[1], p0[2]) {
					continue
				}
				// 下一槽（+0x28）也是合法坐标 + 指纹 → 确认是同一个缓冲
				nq := 1
				if off+0x28+0x1C <= len(buf) {
					q := f3(buf[off+0x28:])
					if worldCoordOK(q[0], q[1], q[2]) && hasQuatBits(buf[off+0x28:]) {
						dx := float64(q[0] - p0[0])
						dz := float64(q[2] - p0[2])
						if math.Hypot(dx, dz) < 200 {
							nq = 2
						}
					}
				}
				if nq < 2 {
					continue
				}
				local = append(local, m1pCand{addr + uintptr(off), p0, nq})
			}
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		cands = append(cands, local...)
		mu.Unlock()
	})

	printf("\n--- 候选（元素起点）%d 个 ---\n", len(cands))
	if len(cands) == 0 {
		printf("⚠ 零候选：指纹+双槽判据在 User Heap 里无命中（游戏在主菜单或玩家完全静止？）\n")
		return
	}
	// -maxdist 过滤（仅 -cand1 大候选集时用）
	if *m1pMaxDist > 0 && anchorValid {
		keep := cands[:0]
		for _, c := range cands {
			if math.Hypot(float64(c.pos[0])-ax, float64(c.pos[2])-az) <= *m1pMaxDist {
				keep = append(keep, c)
			}
		}
		cands = keep
		printf("[maxdist] 距锚点 %.0fm 内候选 %d 个\n", *m1pMaxDist, len(cands))
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].quat != cands[j].quat {
			return cands[i].quat > cands[j].quat
		}
		// 有锚点时按离锚点水平距离排（近的在前）
		if anchorValid {
			di := math.Hypot(float64(cands[i].pos[0])-ax, float64(cands[i].pos[2])-az)
			dj := math.Hypot(float64(cands[j].pos[0])-ax, float64(cands[j].pos[2])-az)
			if di != dj {
				return di < dj
			}
		}
		return cands[i].base < cands[j].base
	})
	show := len(cands)
	if show > 25 {
		show = 25
	}
	for i, c := range cands[:show] {
		dist := ""
		if anchorValid {
			dist = sprintf("  距锚点 %.0fm", math.Hypot(float64(c.pos[0])-ax, float64(c.pos[2])-az))
		}
		printf("  #%02d host=0x%X elem1=0x%X  坐标(%.1f, %.1f, %.1f)  连续指纹=%d%s\n",
			i, c.base, c.base+0x28, c.pos[0], c.pos[1], c.pos[2], c.quat, dist)
	}
	if len(cands) > show {
		printf("  ... 其余 %d 个略\n", len(cands)-show)
	}
	// 分布：落在哪几个 16MB 页窗口里（评估"可筛性"：集中 vs 散布）
	dist := map[uintptr]int{}
	for _, c := range cands {
		dist[c.base&^0xFFFFFF]++
	}
	printf("  分布：%d 个不同 16MB 窗口\n", len(dist))
	if len(dist) <= 4 {
		var keys []uintptr
		for k := range dist {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		for _, k := range keys {
			printf("    0x%X × %d\n", k, dist[k])
		}
	}

	// 会变验证：先读所有候选 elem1，等 1.5s 再读一遍同一槽，位置变了 = 活槽
	// （2026-10-21 修正：旧版比较 elem1(重读) vs elem0(扫描)，两槽帧间差 ~10m 造成
	//   "全部会变"假象；同一槽两遍对比才是真"在移动"）
	printf("\n--- 会变验证（同一槽 elem1 两遍，间隔 %v）---\n", *m1pWait)
	first := make(map[uintptr][3]float32)
	for _, c := range cands {
		if v1, ok := readPosT(p, c.base+0x28); ok {
			first[c.base] = v1
		}
	}
	time.Sleep(*m1pWait)
	type mv struct {
		c    m1pCand
		dist float64
	}
	var moved []mv
	for _, c := range cands {
		v1, ok := first[c.base]
		if !ok {
			continue
		}
		v2, ok2 := readPosT(p, c.base+0x28)
		if !ok2 {
			continue
		}
		dd := math.Hypot(float64(v2[0]-v1[0]), float64(v2[2]-v1[2]))
		if dd > 0.5 {
			moved = append(moved, mv{c, dd})
		}
	}
	printf("  会变的候选：%d/%d\n", len(moved), len(cands))
	for _, m := range moved {
		printf("    host=0x%X  位移 %.2f m\n", m.c.base, m.dist)
	}

	// 结论
	printf("\n--- M1' 判定 ---\n")
	switch {
	case len(cands) == 1:
		printf("★ PASS：指纹命中唯一（1 候选）——「唯一」成立，可继续 M2'（known 缓存跨会话秒锁）\n")
	case anchorValid && len(cands) >= 1:
		// 锚点收敛：最近的候选离锚点多近？
		nearest := math.Hypot(float64(cands[0].pos[0])-ax, float64(cands[0].pos[2])-az)
		second := math.Inf(1)
		if len(cands) >= 2 {
			second = math.Hypot(float64(cands[1].pos[0])-ax, float64(cands[1].pos[2])-az)
		}
		printf("  最近候选距锚点 %.0fm，次近 %.0fm\n", nearest, second)
		if nearest <= 3000 && second-nearest >= 500 {
			printf("★ PASS：锚点排序可筛（最近 %.0fm、次近 %.0fm）——known/存档坐标可直接锁定\n", nearest, second)
		} else {
			printf("○ 存疑：最近 %.0fm / 次近 %.0fm，锚点区分度不足\n", nearest, second)
		}
	case len(cands) <= 3 && len(moved) >= 1:
		printf("★ PASS：%d 候选但会变的只有 %d 个且唯一/可筛——可结合存档锚点锁定\n", len(cands), len(moved))
	case len(moved) == 1:
		printf("○ 部分 PASS：%d 候选里唯一「会变」——配合存档锚点可筛，需在定位器里验证\n", len(cands))
	default:
		printf("⚠ FAIL：候选 %d 个、会变 %d 个，指纹不唯一也不可筛——指纹判据需加强或此路线不成立\n", len(cands), len(moved))
	}
}

// dumpSlots 读宿主地址起 0x28*n 字节（n 个 0x28 槽），按槽输出 hex + float。
// 每个地址读两遍（间隔 400ms），第二遍只输出坐标，用于判断该槽是否在动。
func dumpSlots(p *winmem.Process, addr uintptr, n int) {
	if n < 1 {
		n = 1
	}
	size := n * 0x28
	printf("\n--- dump 0x%X (0x%XB = %d 槽) ---\n", addr, size, n)
	buf, err := p.ReadTimeout(addr, size, 500*time.Millisecond)
	if err != nil || len(buf) < size {
		printf("  读取失败：%v\n", err)
		return
	}
	for row := 0; row < n; row++ {
		o := row * 0x28
		var hexs []string
		for i := 0; i < 0x28; i++ {
			hexs = append(hexs, sprintf("%02X", buf[o+i]))
		}
		printf("  +0x%02X: %s\n", o, strings.Join(hexs, " "))
		v := f3(buf[o:])
		w := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+0x0C:]))
		x := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+0x10:]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+0x14:]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+0x18:]))
		res := binary.LittleEndian.Uint32(buf[o+0x1C:])
		flags := binary.LittleEndian.Uint32(buf[o+0x24:])
		printf("       pos(%.2f, %.2f, %.2f) quat(w,x,y)=(%.3f, %.3f, %.3f) 模长²=%.4f 角度z=%.3f +0x1C=0x%X +0x24=0x%X\n",
			v[0], v[1], v[2], w, x, y, float64(w)*float64(w)+float64(x)*float64(x)+float64(y)*float64(y), z, res, flags)
	}
	// 第二遍（间隔 400ms）只对比坐标变化
	time.Sleep(400 * time.Millisecond)
	buf2, err2 := p.ReadTimeout(addr, size, 500*time.Millisecond)
	if err2 != nil || len(buf2) < size {
		printf("  第二遍读取失败：%v\n", err2)
		return
	}
	for row := 0; row < n; row++ {
		o := row * 0x28
		v1 := f3(buf[o:])
		v2 := f3(buf2[o:])
		dx := float64(v2[0] - v1[0])
		dz := float64(v2[2] - v1[2])
		printf("  +0x%02X 第二遍 pos(%.2f, %.2f, %.2f)  位移 %.2f m\n", o, v2[0], v2[1], v2[2], math.Hypot(dx, dz))
	}
}

// ── 判据（来源：enavi/internal/poshist 2026-10-20 实测定案，位模式加速版） ──

// hasQuatBits 元素 +0x0C 起是否为朝向四元数指纹。
//
// ★ 2026-10-21 M1' 首轮实测修正：enavi 原判据 w∈(0.90,1.00] 只在玩家面朝
//   「w≈1」方向时成立——实测 known 玩家缓冲槽当前 w=-0.311（玩家转向后），
//   该判据直接漏掉玩家。改为**单位模长**（朝向无关）：
//     (w,x,y) 三个分量平方和 ≈ 1，且 +0x18 角度（度数）∈ [-180,180]。
//   位置历史缓冲的四元数三元组恒为单位向量，与朝向无关。
func hasQuatBits(b []byte) bool {
	if len(b) < 0x1C {
		return false
	}
	wb := binary.LittleEndian.Uint32(b[0x0C:])
	xb := binary.LittleEndian.Uint32(b[0x10:])
	yb := binary.LittleEndian.Uint32(b[0x14:])
	zb := binary.LittleEndian.Uint32(b[0x18:])
	// NaN/Inf：指数全 1
	if wb&0x7F800000 == 0x7F800000 || xb&0x7F800000 == 0x7F800000 ||
		yb&0x7F800000 == 0x7F800000 || zb&0x7F800000 == 0x7F800000 {
		return false
	}
	w := math.Float32frombits(wb)
	x := math.Float32frombits(xb)
	y := math.Float32frombits(yb)
	z := math.Float32frombits(zb)
	// 单位模长（容差 0.05，实测 w=-0.311,x=-0.116,y=-0.943 → 0.9994 ✓）
	nf := float64(w)*float64(w) + float64(x)*float64(x) + float64(y)*float64(y)
	if nf < 0.95 || nf > 1.05 {
		return false
	}
	// 角度（度数）∈ [-180, 180]
	if z < -180.0 || z > 180.0 {
		return false
	}
	return true
}

// worldCoordOK 是否像海拉鲁世界坐标（规则照搬 enavi poshist，含全部排坑）。
func worldCoordOK(x, alt, z float32) bool {
	xf, af, zf := float64(x), float64(alt), float64(z)
	if math.IsNaN(xf) || math.IsInf(xf, 0) || math.IsNaN(af) || math.IsInf(af, 0) || math.IsNaN(zf) || math.IsInf(zf, 0) {
		return false
	}
	if math.Abs(xf) > 8000 || math.Abs(zf) > 8000 {
		return false
	}
	if af < -600 || af > 6000 {
		return false
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	// X/Z 不能同时为 0（退化位置表特征）
	if x == 0 && z == 0 {
		return false
	}
	// 世界原点 10m 内排除（玩家不可能在海外原点）
	if math.Abs(xf) < 10 && math.Abs(zf) < 10 {
		return false
	}
	// X 与 alt 同时为整数排除（参数表特征：X=0.0、alt=1.0 这类占位坐标；
	//   ★ 2026-10-21 修正：原"X/Z 同时整数"误杀真实玩家整数坐标（实测 -717.0,618.0 是玩家真实位置））
	if math.Abs(xf-math.Round(xf)) < 0.001 && math.Abs(af-math.Round(af)) < 0.001 {
		return false
	}
	// ★ 2026-10-21 M1' 首轮实测新增：alt 与 z 同时为整数 → 假槽（实测 (24.2,1.0,1.0)
	//   这类"alt/z 恰好 1.0"的结构是每帧更新的临时数据，不是世界坐标；
	//   真实玩家坐标的 alt/z 是连续小数，几乎不可能同帧都落在整数上。
	if math.Abs(af-math.Round(af)) < 0.001 && math.Abs(zf-math.Round(zf)) < 0.001 {
		return false
	}
	return true
}

func f3(b []byte) [3]float32 {
	var v [3]float32
	for i := 0; i < 3 && (i+1)*4 <= len(b); i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func readPosT(p *winmem.Process, addr uintptr) ([3]float32, bool) {
	buf, err := p.ReadTimeout(addr, 12, 500*time.Millisecond)
	if err != nil || len(buf) < 12 {
		return [3]float32{}, false
	}
	return f3(buf), true
}

// heapRegions User Heap 可读写块（enavi scan.HeapRegions 同款口径）。
func heapRegions(p *winmem.Process, d winmem.Dram) []winmem.Region {
	lo, hi := d.HeapLo(), d.HeapHi()
	var out []winmem.Region
	p.Regions(lo, hi, func(r winmem.Region) bool {
		if r.RW() && r.Size >= 64<<10 {
			out = append(out, r)
		}
		return false
	})
	return out
}

// runChunksL 分块读 + 限速（保护模拟器），overlap 256B 防槽跨块断裂。
func runChunksL(p *winmem.Process, regions []winmem.Region, workers int, delay time.Duration, fn func(addr uintptr, buf []byte)) {
	const chunk = 8 << 20
	const overlap = 256
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
			if remain := int(r.Size - off); n > remain {
				n = remain
			}
			if int(off)+n < int(r.Size) {
				n += overlap
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}
	if len(jobs) == 0 {
		return
	}
	ch := make(chan job, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				time.Sleep(delay)
				if err != nil || len(buf) < 16 {
					continue
				}
				fn(j.addr, buf)
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}
