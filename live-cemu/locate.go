// 内存扫描定位（结构扫描兜底）：在 Cemu 的 RW 块里找"合法坐标 + 正交旋转矩阵"结构，
// 分组打分 → shortlist 监听确认。与 BotwNavi(live-go) locate.go 同构，改为大端读取。

package main

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

const (
	chunkSize  = 64 << 20 // 每次读取 64MB
	hitCap     = 3000000
	minBlockMB = 512.0 // 结构扫描只看 MEM2（1GB）——玩家对象在堆里
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
	X, Y, Z float32
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

// scanBytes 扫描大端缓冲，收集"合法坐标三元组 + 其后紧跟正交旋转矩阵"的槽。
func scanBytesBE(buf []byte, base uintptr, dst []float32) []Hit {
	if len(buf) < 48 {
		return nil
	}
	n := toF32BE(buf, dst)
	var out []Hit
	for i := 0; i+12 <= n; i++ {
		x, y, z := dst[i], dst[i+1], dst[i+2]
		if !posRangeOK(x, y, z) {
			continue
		}
		if !rotOKF(dst[i+3 : i+12]) {
			continue
		}
		out = append(out, Hit{base + uintptr(i)*4, x, y, z, 0, 0})
	}
	return out
}

// groupAndRank 命中分组打分：struct>0 优先 → struct 降序 → dist 升序 → copies 降序。
// （Cemu 版无存档锚点，dist 恒 0，等价于 struct → copies。）
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
		if math.Abs(float64(a.Dist-b.Dist)) > 15.0 {
			return a.Dist < b.Dist
		}
		if a.Struct != b.Struct {
			return a.Struct > b.Struct
		}
		return a.Copies > b.Copies
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

// rotOK 读进程内存后检查旋转矩阵特征（对 Hit 候选做二次确认，读回大端转小端）。
func rotOK(h uintptr, addr uintptr) bool {
	d := readMem(h, addr+12, 36)
	if len(d) < 36 {
		return false
	}
	fl := make([]float32, 12)
	toF32BE(d[:36], fl[:9])
	return rotOKF(fl[:9])
}

// structuralScan 全量结构扫描（worker pool，每 worker 复用转换缓冲）。
func structuralScan(h uintptr, blocks []struct{ Base, Size uintptr }, logf func(string)) []Hit {
	t0 := time.Now()
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
	var hits []Hit
	wg := sync.WaitGroup{}
	ch := make(chan struct{ base, size uintptr })
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dst := make([]float32, chunkSize/4)
			for j := range ch {
				buf := readMem(h, j.base, int(j.size))
				if buf == nil {
					continue
				}
				local := scanBytesBE(buf, j.base, dst)
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
