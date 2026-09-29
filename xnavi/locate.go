// locate.go — 结构扫描定位（双平台统一）。
//
// 合并自 live-go/locate.go（存档锚点窗口 + 小端）与 live-cemu/locate.go（大端结构扫描）：
//   - 有存档锚点：锚点 ±120/±400 窗口扫描（快，与 live-go 一致）
//   - 无存档锚点：退回全量结构扫描（坐标合法 + 后随正交旋转矩阵，与 live-cemu 一致）
//   - 端序：按平台分流（小端零拷贝视图 / 大端 toF32BE 转换）
//   - 排序：ds 文档唯一规则（TOTK V1.8.7 已验证）——struct>0 门槛 → copies 降序 →
//     dist 升序；不比较 struct 大小（struct 大小无意义）
//   - struct 计数：检查组内前 64 个地址（BOTW 布局已验证；TOTK 曾遇并发截断漏矩阵槽，
//     故此处保留 64 上限与 TOTK"检查全部"折中——见 watch.go 探针复核兜底）

package main

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
	"unsafe"
)

const (
	chunkSize  = 64 << 20 // 每次读取 64MB
	hitCap     = 3000000  // 命中上限（防窗口过宽）
	minBlockMB = 512.0    // 结构扫描只看大块（游戏内存所在区域）
)

type Hit struct {
	Addr     uintptr
	X, Y, Z  float32 // (X, alt, Z) 内存顺序
	Ri       int
	Dist     float32
}

type Group struct {
	Addrs  []uintptr
	Ri     int
	Dist   float32
	Struct int
	Copies int
	X, Y, Z float32 // 组坐标（内存顺序）
}

type ShortlistEntry struct {
	Hud    [3]float32 // (X, Z, alt) 展示顺序
	Copies int
	Struct int
	Dist   float32
	Slot   int
	Addrs  []uintptr
}

type LocateResult struct {
	Addr      uintptr
	Copies    int
	Struct    int
	Hud       [3]float32
	Shortlist []ShortlistEntry
	Log       []string
}

// floats 小端 float32 零拷贝视图。
func floats(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
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

// rotOKBytes 检查缓冲区内是否存在正交 3x3 旋转矩阵（小端）。
func rotOKBytes(buf []byte) bool {
	if len(buf) < 64 {
		return false
	}
	a := floats(buf[:64])
	for st := 0; st+9 <= len(a); st++ {
		if rotOKF(a[st : st+9]) {
			return true
		}
	}
	return false
}

// rotOKBytesBE 检查缓冲区内是否存在正交 3x3 旋转矩阵（大端）。
func rotOKBytesBE(buf []byte) bool {
	if len(buf) < 64 {
		return false
	}
	fl := make([]float32, 16)
	n := toF32BE(buf[:64], fl)
	for st := 0; st+9 <= n; st++ {
		if rotOKF(fl[st : st+9]) {
			return true
		}
	}
	return false
}

// rotOK 读进程内存后检查旋转矩阵特征（按平台端序）。
func rotOK(h uintptr, addr uintptr) bool {
	d := readMem(h, addr, 128)
	if len(d) < 64 {
		return false
	}
	plat := currentPlatform()
	if plat != nil && !plat.LittleEndian() {
		return rotOKBytesBE(d)
	}
	return rotOKBytes(d)
}

// windowBounds 计算多个锚点合并后的扫描窗口边界。
func windowBounds(refs [][3]float32, window float32) (gx0, gx1, gh0, gh1, gz0, gz1 float32) {
	gx0, gx1 = refs[0][0]-window, refs[0][0]+window
	gh0, gh1 = refs[0][1]-window, refs[0][1]+window
	gz0, gz1 = refs[0][2]-window, refs[0][2]+window
	for _, r := range refs[1:] {
		if r[0]-window < gx0 {
			gx0 = r[0] - window
		}
		if r[0]+window > gx1 {
			gx1 = r[0] + window
		}
		if r[1]-window < gh0 {
			gh0 = r[1] - window
		}
		if r[1]+window > gh1 {
			gh1 = r[1] + window
		}
		if r[2]-window < gz0 {
			gz0 = r[2] - window
		}
		if r[2]+window > gz1 {
			gz1 = r[2] + window
		}
	}
	return
}

// scanChunk 扫描一块内存缓冲：窗口内 float32 三元组（内存序 X, alt, Z）。
// dst 为大端解码复用缓冲（小端路径不用，传 nil 即可）。
func scanChunk(buf []byte, base uintptr, gx0, gx1, gh0, gh1, gz0, gz1, win2 float32, refs [][3]float32, dst []float32, hits *[]Hit) {
	if len(buf) < 12 {
		return
	}
	plat := currentPlatform()
	var a []float32
	if plat != nil && !plat.LittleEndian() {
		n := toF32BE(buf, dst)
		a = dst[:n]
	} else {
		a = floats(buf)
	}
	for i := 0; i+3 <= len(a); i++ {
		x, y, z := a[i], a[i+1], a[i+2]
		if x <= gx0 || x >= gx1 {
			continue
		}
		if y <= gh0 || y >= gh1 {
			continue
		}
		if z <= gz0 || z >= gz1 {
			continue
		}
		bestD, bestRi := float32(1e18), 0
		for ri, r := range refs {
			dx, dy, dz := x-r[0], y-r[1], z-r[2]
			d := dx*dx + dy*dy + dz*dz
			if d < bestD {
				bestD, bestRi = d, ri
			}
		}
		if bestD <= win2 {
			*hits = append(*hits, Hit{base + uintptr(i)*4, x, y, z, bestRi, float32(math.Sqrt(float64(bestD)))})
		}
	}
}

// scanAll 并发扫描所有区域（worker pool），窗口模式。
func scanAll(h uintptr, blocks []MemBlock, refs [][3]float32, window float32, hits *[]Hit) {
	gx0, gx1, gh0, gh1, gz0, gz1 := windowBounds(refs, window)
	win2 := window * window
	jobs := make([]MemBlock, 0, len(blocks)*2)
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(chunkSize)
			if b.Size-off < n {
				n = b.Size - off
			}
			jobs = append(jobs, MemBlock{b.Base + off, n})
			off += n
		}
	}
	workers := runtime.NumCPU()
	if workers > 16 {
		workers = 16
	}
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	ch := make(chan MemBlock)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []Hit
			dst := make([]float32, chunkSize/4) // 大端解码复用缓冲
			for j := range ch {
				if len(*hits) > hitCap {
					return
				}
				buf := readMem(h, j.Base, int(j.Size))
				if buf == nil {
					continue
				}
				scanChunk(buf, j.Base, gx0, gx1, gh0, gh1, gz0, gz1, win2, refs, dst, &local)
			}
			if len(local) > 0 {
				mu.Lock()
				*hits = append(*hits, local...)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

// scanBytesBE 扫描大端缓冲：坐标合法三元组 + 其后紧跟正交旋转矩阵的槽（全量模式）。
func scanBytesBE(buf []byte, base uintptr, dst []float32) []Hit {
	if len(buf) < 48 {
		return nil
	}
	n := toF32BE(buf, dst)
	var out []Hit
	for i := 0; i+12 <= n; i++ {
		x, y, z := dst[i], dst[i+1], dst[i+2]
		if !gameValidTriple(x, y, z) {
			continue
		}
		if !rotOKF(dst[i+3 : i+12]) {
			continue
		}
		out = append(out, Hit{base + uintptr(i)*4, x, y, z, 0, 0})
	}
	return out
}

// scanBytesLE 扫描小端缓冲：坐标合法三元组 + 其后紧跟正交旋转矩阵的槽（全量模式）。
func scanBytesLE(buf []byte, base uintptr) []Hit {
	if len(buf) < 48 {
		return nil
	}
	a := floats(buf)
	var out []Hit
	for i := 0; i+12 <= len(a); i++ {
		x, y, z := a[i], a[i+1], a[i+2]
		if !gameValidTriple(x, y, z) {
			continue
		}
		if !rotOKF(a[i+3 : i+12]) {
			continue
		}
		out = append(out, Hit{base + uintptr(i)*4, x, y, z, 0, 0})
	}
	return out
}

// structuralScan 全量结构扫描（无存档锚点兜底，worker pool）。
func structuralScan(h uintptr, blocks []MemBlock, logf func(string)) []Hit {
	t0 := time.Now()
	var jobs []MemBlock
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(chunkSize)
			if b.Size-off < n {
				n = b.Size - off
			}
			jobs = append(jobs, MemBlock{b.Base + off, n})
			off += n
		}
	}
	workers := runtime.NumCPU()
	if workers > 16 {
		workers = 16
	}
	var mu sync.Mutex
	var hits []Hit
	wg := sync.WaitGroup{}
	ch := make(chan MemBlock)
	plat := currentPlatform()
	bigEndian := plat != nil && !plat.LittleEndian()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dst := make([]float32, chunkSize/4)
			for j := range ch {
				buf := readMem(h, j.Base, int(j.Size))
				if buf == nil {
					continue
				}
				var local []Hit
				if bigEndian {
					local = scanBytesBE(buf, j.Base, dst)
				} else {
					local = scanBytesLE(buf, j.Base)
				}
				if len(local) > 0 {
					mu.Lock()
					hits = append(hits, local...)
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
	if logf != nil {
		logf(fmt.Sprintf("structural scan: %d candidates in %.1fs", len(hits), time.Since(t0).Seconds()))
	}
	return hits
}

// groupAndRank 命中分组打分：struct>0 门槛 → copies 降序 → dist 升序。
// （ds 文档 4.4 唯一规则：不比较 struct 大小，struct 大小无意义。）
func groupAndRank(h uintptr, hits []Hit) ([]*Group, []ShortlistEntry) {
	groups := map[[3]int32]*Group{}
	for _, hit := range hits {
		k := [3]int32{
			int32(math.Round(float64(hit.X) * 10)),
			int32(math.Round(float64(hit.Y) * 10)),
			int32(math.Round(float64(hit.Z) * 10)),
		}
		g := groups[k]
		if g == nil {
			g = &Group{Ri: hit.Ri, Dist: hit.Dist, X: hit.X, Y: hit.Y, Z: hit.Z}
			groups[k] = g
		}
		g.Addrs = append(g.Addrs, hit.Addr)
		if hit.Dist < g.Dist {
			g.Dist, g.Ri = hit.Dist, hit.Ri
		}
	}
	var byCopies []*Group
	for _, g := range groups {
		g.Copies = len(g.Addrs)
		byCopies = append(byCopies, g)
	}
	sort.Slice(byCopies, func(i, j int) bool { return byCopies[i].Copies > byCopies[j].Copies })
	top := byCopies
	if len(top) > 30 {
		top = top[:30]
	}
	for _, g := range top {
		n := g.Addrs
		if len(n) > 64 {
			n = n[:64]
		}
		for _, a := range n {
			if rotOK(h, a) {
				g.Struct++
			}
		}
	}
	sort.Slice(top, func(i, j int) bool {
		a, b := top[i], top[j]
		al, bl := a.Struct > 0, b.Struct > 0
		if al != bl {
			return al // struct>0 门槛
		}
		// 离存档锚点近的优先（<100m），避免 copies 高的静态对象压过真玩家
		an, bn := a.Dist < 100, b.Dist < 100
		if an != bn {
			return an
		}
		if a.Copies != b.Copies {
			return a.Copies > b.Copies
		}
		return a.Dist < b.Dist
	})
	shortlist := make([]ShortlistEntry, 0, len(top))
	for _, g := range top {
		shortlist = append(shortlist, ShortlistEntry{
			Hud:    [3]float32{g.X, g.Z, g.Y},
			Copies: g.Copies,
			Struct: g.Struct,
			Dist:   g.Dist,
			Slot:   g.Ri,
			Addrs:  g.Addrs[:min(len(g.Addrs), 24)],
		})
	}
	return top, shortlist
}

// locate 主流程：存档锚点 → 窗口扫描（120/400）→ 分组打分；
// 无锚点（含 Cemu 无存档）→ 全量结构扫描。
// onlyBlocks 非空时只扫这些块（会话块世代用）。
func locate(pid uint32, window float64, onlyBlocks []MemBlock, logf func(string)) *LocateResult {
	t0 := time.Now()
	log := func(s string) {
		if logf != nil {
			logf(s)
		}
	}
	plat := currentPlatform()
	if plat == nil {
		log("no platform selected")
		return nil
	}
	h := plat.Handle()
	if h == 0 {
		log("not attached to emulator")
		return nil
	}

	anchors := readSaveAnchors(plat)
	var refs [][3]float32
	if len(anchors) > 0 {
		primary := anchors[0]
		refs = [][3]float32{primary.Pos}
		log(fmt.Sprintf("save anchor: pt=%d  mem=(%.1f, %.1f, %.1f)", primary.Playtime, primary.Pos[0], primary.Pos[1], primary.Pos[2]))
		if len(anchors) > 1 {
			log(fmt.Sprintf("  (%d more save slots exist; not used - they are old positions and only widen the scan)", len(anchors)-1))
		}
	} else if p := loadKnownPos(); len(p) == 3 {
		refs = [][3]float32{{p[0], p[1], p[2]}}
		log(fmt.Sprintf("no save anchor; last-known pos as window center mem=(%.1f, %.1f, %.1f)", p[0], p[1], p[2]))
	} else {
		log("no save anchor & no known pos - falling back to full structural scan")
	}

	blocks := onlyBlocks
	if len(blocks) == 0 {
		blocks = plat.Blocks(minBlockMB)
	}
	var total uintptr
	for _, b := range blocks {
		total += b.Size
	}
	log(fmt.Sprintf("RW regions>=%.0fMB: %d  (%.1f GB)", minBlockMB, len(blocks), float64(total)/1073741824.0))
	for _, b := range blocks {
		log(fmt.Sprintf("  region 0x%012X  %8.0f MB", b.Base, float64(b.Size)/1048576.0))
	}

	var best *Group
	var shortlist []ShortlistEntry
	if len(refs) > 0 {
		for attempt, win := range []float32{float32(math.Max(window, 120.0)), 400.0} {
			var hits []Hit
			scanAll(h, blocks, refs, win, &hits)
			log(fmt.Sprintf("pass %d (window +/-%.0f): %d triple hits in %.1fs", attempt+1, win, len(hits), time.Since(t0).Seconds()))
			if len(hits) == 0 {
				continue
			}
			var ranked []*Group
			ranked, shortlist = groupAndRank(h, hits)
			for i := 0; i < len(ranked) && i < 12; i++ {
				g := ranked[i]
				log(fmt.Sprintf("    x%-5d struct %2d  d=%6.1f  slot#%d  mem=(%.1f, %.1f, %.1f)",
					g.Copies, g.Struct, g.Dist, g.Ri, g.X, g.Y, g.Z))
			}
			best = ranked[0]
			if best.Struct > 0 {
				break
			}
		}
	} else {
		hits := structuralScan(h, blocks, log)
		if len(hits) > 0 {
			var ranked []*Group
			ranked, shortlist = groupAndRank(h, hits)
			for i := 0; i < len(ranked) && i < 12; i++ {
				g := ranked[i]
				log(fmt.Sprintf("    x%-5d struct %2d  d=%6.1f  mem=(%.1f, %.1f, %.1f)",
					g.Copies, g.Struct, g.Dist, g.X, g.Y, g.Z))
			}
			best = ranked[0]
		}
	}
	if best == nil {
		log("FAILED: no confident candidate")
		return nil
	}
	addr := best.Addrs[0]
	log(fmt.Sprintf("candidate 0x%012X  copies=%d struct=%d  [pending liveness]", addr, best.Copies, best.Struct))
	log(fmt.Sprintf("shortlist: %d groups, %d addresses", len(shortlist), shortlistAddrs(shortlist)))
	log(fmt.Sprintf("total %.1fs", time.Since(t0).Seconds()))

	var hud [3]float32
	if d := decodeTripleAt(h, addr); d != nil {
		hud = [3]float32{d[0], d[1], d[2]}
	}
	// 候选瞬时不可读（Cemu 大端偶发读取失败/槽刚被回收）→ 顺延到下一组，
	// 避免锁到读不出的地址（此前实测出现过锁 0,0,0 后靠 invalidN 自愈的 2s 抖动）。
	for hud == [3]float32{} && len(shortlist) > 1 {
		shortlist = shortlist[1:]
		addr = shortlist[0].Addrs[0]
		if d := decodeTripleAt(h, addr); d != nil {
			hud = [3]float32{d[0], d[1], d[2]}
		}
	}
	if hud == [3]float32{} {
		log("FAILED: candidate unreadable")
		return nil
	}
	return &LocateResult{
		Addr:      addr,
		Copies:    best.Copies,
		Struct:    best.Struct,
		Hud:       hud,
		Shortlist: shortlist,
	}
}

// decodeTripleAt 读 12 字节并端序解码 + 游戏校验 → 展示序 (gx, gz, alt)；无效返回 nil。
func decodeTripleAt(h uintptr, addr uintptr) []float32 {
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

func shortlistAddrs(sl []ShortlistEntry) int {
	n := 0
	for _, g := range sl {
		n += len(g.Addrs)
	}
	return n
}
