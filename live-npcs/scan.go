// 实体候选扫描：全内存扫描"坐标+旋转矩阵"结构簇 → 分组 → rotOK 确认。
// 复用 live-go 的扫描/分组思路（floats/rotOK/groupAndRank），改造为实体扫描：
// 不以"锚点附近"为窗口，而是全内存收集合法游戏坐标三元组，聚簇后逐组确认旋转矩阵结构。

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
	hitCap     = 4000000  // 命中上限（防过宽）
	minBlockMB = 512.0
)

// Hit 一个坐标三元组命中。
type Hit struct {
	Addr     uintptr
	X, Y, Z  float32 // (X, alt, Z) 内存顺序
	Dist     float32
}

// EntityGroup 一个实体候选组：同一位置附近的多个命中聚为一组。
type EntityGroup struct {
	Addrs   []uintptr
	Struct  int
	Copies  int
	X, Y, Z float32 // 组坐标（内存顺序）
	RepAddr uintptr // 第一个 rotOK 通过的地址（结构起始候选，探测/类型字段用）
	Stable  bool    // 跨帧地址稳定（探测用）
}

func floats(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// posSane 游戏坐标合法性过滤（与 live-go decodePos 的边界一致）。
func posSane(x, y, z float32) bool {
	if x <= -7000 || x >= 7000 || z <= -7000 || z >= 7000 ||
		y <= -600 || y >= 5000 {
		return false
	}
	// 近零残留（denormal/原点残留）判无效
	if abs32(x) < 5 && abs32(z) < 5 {
		return false
	}
	if abs32(x) < 1e-20 || abs32(z) < 1e-20 {
		return false
	}
	return true
}

// rotOKBytes 检查缓冲区内是否存在正交 3x3 旋转矩阵（ActorBase 特征）。
func rotOKBytes(buf []byte) bool {
	if len(buf) < 64 {
		return false
	}
	a := floats(buf[:64])
	for st := 0; st+9 <= len(a); st++ {
		m := a[st : st+9]
		cols := [3][3]float32{
			{m[0], m[3], m[6]},
			{m[1], m[4], m[7]},
			{m[2], m[5], m[8]},
		}
		ok := true
		for i := 0; i < 3; i++ {
			n := math.Sqrt(float64(cols[i][0]*cols[i][0] + cols[i][1]*cols[i][1] + cols[i][2]*cols[i][2]))
			if math.Abs(n-1.0) >= 0.05 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		for i := 0; i < 3 && ok; i++ {
			for j := i + 1; j < 3; j++ {
				dot := cols[i][0]*cols[j][0] + cols[i][1]*cols[j][1] + cols[i][2]*cols[j][2]
				if math.Abs(float64(dot)) >= 0.08 {
					ok = false
					break
				}
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// rotOK 读进程内存后检查旋转矩阵特征。
func rotOK(h uintptr, addr uintptr) bool {
	return rotOKBytes(readMem(h, addr, 128))
}

// collectHits 扫描一段缓冲，收集所有"合法游戏坐标"三元组。
// 不做窗口过滤（实体遍布世界，窗口会让远处的实体漏掉）。
func collectHits(buf []byte, base uintptr, hits *[]Hit) {
	a := floats(buf)
	for i := 0; i+3 <= len(a); i++ {
		x, y, z := a[i], a[i+1], a[i+2]
		if !posSane(x, y, z) {
			continue
		}
		*hits = append(*hits, Hit{base + uintptr(i)*4, x, y, z, 0})
	}
}

// scanAllHits 并发扫描所有 guest RAM，收集全部合法坐标三元组。
func scanAllHits(h uintptr, blocks []struct{ Base, Size uintptr }) []Hit {
	var jobs []struct{ base, size uintptr }
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(chunkSize)
			if b.Size-off < n {
				n = b.Size - off
			}
			jobs = append(jobs, struct{ base, size uintptr }{b.Base + off, n})
			off += n
		}
	}
	workers := runtime.NumCPU()
	if workers > 16 {
		workers = 16
	}
	var mu sync.Mutex
	var all []Hit
	wg := sync.WaitGroup{}
	ch := make(chan struct{ base, size uintptr })
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []Hit
			for j := range ch {
				collectHits(readMem(h, j.base, int(j.size)), j.base, &local)
			}
			if len(local) > 0 {
				mu.Lock()
				all = append(all, local...)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return all
}

// groupHits 按位置聚簇（0.5m 粒度），返回候选组（已按 copies 降序）。
// 关键：struct 计数打分——组内多个地址通过 rotOK 才算真槽（对齐 live-go groupAndRank）。
// 单地址碰巧有旋转矩阵的组是垃圾（噪声内存里到处都是正交矩阵）。
func groupHits(h uintptr, hits []Hit, maxGroups int) []*EntityGroup {
	groups := map[[3]int32]*EntityGroup{}
	for _, hit := range hits {
		k := [3]int32{
			int32(math.Round(float64(hit.X) * 2)),
			int32(math.Round(float64(hit.Y) * 2)),
			int32(math.Round(float64(hit.Z) * 2)),
		}
		g := groups[k]
		if g == nil {
			g = &EntityGroup{X: hit.X, Y: hit.Y, Z: hit.Z}
			groups[k] = g
		}
		if len(g.Addrs) < 64 {
			g.Addrs = append(g.Addrs, hit.Addr)
		}
		g.Copies++
	}
	var byCopies []*EntityGroup
	for _, g := range groups {
		byCopies = append(byCopies, g)
	}
	sort.Slice(byCopies, func(i, j int) bool { return byCopies[i].Copies > byCopies[j].Copies })
	if len(byCopies) > maxGroups {
		byCopies = byCopies[:maxGroups]
	}
	// rotOK 计数：组内每个地址检查，通过数 = struct。
	// 玩家槽等真实体槽 struct 高（live-go 实测 >=6），副本 struct 0~4，垃圾组 0~1。
	for _, g := range byCopies {
		for _, a := range g.Addrs {
			if rotOK(h, a) {
				g.Struct++
				if g.RepAddr == 0 {
					g.RepAddr = a
				}
			}
		}
	}
	return byCopies
}

// findEntities 一次完整扫描：返回实体候选组（仅含 struct>=2 的，剔除单地址巧合）。
func findEntities(h uintptr) ([]*EntityGroup, int, float64) {
	t0 := time.Now()
	blocks := guestBlocks(h, minBlockMB)
	hits := scanAllHits(h, blocks)
	groups := groupHits(h, hits, 4000)
	var out []*EntityGroup
	for _, g := range groups {
		if g.Struct >= 2 {
			out = append(out, g)
		}
	}
	return out, len(hits), time.Since(t0).Seconds()
}

// scanWindowHits 以参考点为锚的窗口扫描（运行时用，快）。
// 参考 live-go scanRegion：只收窗口内合法坐标三元组，避免全内存 8s/帧。
func scanWindowHits(h uintptr, blocks []struct{ Base, Size uintptr }, refs [][3]float32, window float32) []Hit {
	if len(refs) == 0 {
		return nil
	}
	gx0, gx1 := refs[0][0]-window, refs[0][0]+window
	gh0, gh1 := refs[0][1]-window, refs[0][1]+window
	gz0, gz1 := refs[0][2]-window, refs[0][2]+window
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
	win2 := window * window
	var all []Hit
	var mu sync.Mutex
	workers := runtime.NumCPU()
	if workers > 16 {
		workers = 16
	}
	wg := sync.WaitGroup{}
	ch := make(chan struct{ base, size uintptr })
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []Hit
			for j := range ch {
				buf := readMem(h, j.base, int(j.size))
				a := floats(buf)
				for i := 0; i+3 <= len(a); i++ {
					x, y, z := a[i], a[i+1], a[i+2]
					if x <= gx0 || x >= gx1 || y <= gh0 || y >= gh1 || z <= gz0 || z >= gz1 {
						continue
					}
					bestD := float32(1e18)
					for _, r := range refs {
						dx, dy, dz := x-r[0], y-r[1], z-r[2]
						d := dx*dx + dy*dy + dz*dz
						if d < bestD {
							bestD = d
						}
					}
					if bestD <= win2 {
						local = append(local, Hit{j.base + uintptr(i)*4, x, y, z, 0})
						if len(local) > hitCap {
							break
						}
					}
				}
			}
			if len(local) > 0 {
				mu.Lock()
				all = append(all, local...)
				mu.Unlock()
			}
		}()
	}
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(chunkSize)
			if b.Size-off < n {
				n = b.Size - off
			}
			ch <- struct{ base, size uintptr }{b.Base + off, n}
			off += n
		}
	}
	close(ch)
	wg.Wait()
	return all
}

// findEntitiesNear 窗口扫描版：以 refs 为锚 ±window 扫实体候选（运行时用）。
func findEntitiesNear(h uintptr, refs [][3]float32, window float32) ([]*EntityGroup, int, float64) {
	t0 := time.Now()
	blocks := guestBlocks(h, minBlockMB)
	hits := scanWindowHits(h, blocks, refs, window)
	groups := groupHits(h, hits, 1000)
	var out []*EntityGroup
	for _, g := range groups {
		if g.Struct >= 2 {
			out = append(out, g)
		}
	}
	return out, len(hits), time.Since(t0).Seconds()
}

func hexAddr(v uintptr) string {
	return fmt.Sprintf("0x%012X", v)
}
