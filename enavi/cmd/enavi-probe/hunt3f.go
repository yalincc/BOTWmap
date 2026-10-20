// 本文件 = 暴力扫 UserHeap，找「3 个连续 float 像世界坐标」的所有位置。
//
// 为什么需要：属性表结构假设（+0xC0 / 0x50 步长 / 0x98 值步长）在玩家 actor 上
// 并不成立 —— 实测 findcoord 找到的全是 (1,0,0) 这类单位向量。
// 所以退一步：不预设结构，直接在内存里找「X, alt, Z 三个连续 float」。
//
// 判据（世界坐标的强特征）：
//   - 三个都是有限 float（不是 NaN / Inf）
//   - |X| ≤ 8000，|Z| ≤ 8000（海拉鲁世界尺度）
//   - alt ∈ [-600, 6000]（海拉鲁高度范围）
//   - 不是全零
//   - 不是「恰好一个分量为 ±1、其余为 0」这种单位向量/布尔值
//   - 水平距离 (√(X²+Z²)) 在 5 ~ 7000 之间（玩家不会在原点，也不会在世界尽头）
//
// 用法：enavi-probe -cmd hunt3f [-pid N] [-top 60]
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

var (
	huntTop  = flag.Int("ntop", 60, "输出前 N 个候选")
	huntSave = flag.String("save", "", "把命中地址存到这个文件（便于后续持续跟踪）")
)

func cmdHunt3f(p *winmem.Process) {
	hr("暴力扫描：找 3 连 float 的世界坐标")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[h3f] %v\n", err)
		return
	}
	regions := scan.HeapRegions(p, d, 256<<10)
	printf("[h3f] UserHeap 候选区 %d 个\n", len(regions))

	type hit struct {
		addr uintptr
		pos  [3]float32
	}
	var hits []hit
	var mu sync.Mutex

	runChunks(p, regions, 2, func(addr uintptr, buf []byte) {
		var local []hit
		for off := 0; off+12 <= len(buf); off += 4 {
			f0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
			f1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
			f2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
			if !worldCoord(f0, f1, f2) {
				continue
			}
			local = append(local, hit{addr + uintptr(off), [3]float32{f0, f1, f2}})
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		hits = append(hits, local...)
		mu.Unlock()
	})

	printf("[h3f] 命中 %d 处\n", len(hits))
	// 排序：水平距离中等 + alt 为正的最像玩家
	sort.Slice(hits, func(i, j int) bool { return h3fScore(hits[i].pos) > h3fScore(hits[j].pos) })
	for i, h := range hits {
		if i >= *huntTop {
			printf("   ...（其余 %d 处略）\n", len(hits)-*huntTop)
			break
		}
		gva := uintptr(0)
		if h.addr >= d.ArenaLow {
			gva = h.addr - d.ArenaLow
		}
		printf("   host=0x%X gva=0x%X  (%.2f, %.2f, %.2f)\n",
			h.addr, gva, h.pos[0], h.pos[1], h.pos[2])
	}

	if *huntSave != "" {
		os.MkdirAll(filepath.Dir(*huntSave), 0o755)
		if f, err := os.Create(*huntSave); err == nil {
			for _, h := range hits {
				fmt.Fprintf(f, "0x%X %.4f %.4f %.4f\n", h.addr, h.pos[0], h.pos[1], h.pos[2])
			}
			f.Close()
			printf("[h3f] 命中清单 → %s\n", *huntSave)
		}
	}
}

// worldCoord 三个 float 是否像世界坐标。
func worldCoord(f0, f1, f2 float32) bool {
	for _, f := range []float32{f0, f1, f2} {
		v := float64(f)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	x, alt, z := float64(f0), float64(f1), float64(f2)
	if math.Abs(x) > 8000 || math.Abs(z) > 8000 {
		return false
	}
	if alt < -600 || alt > 6000 {
		return false
	}
	if f0 == 0 && f1 == 0 && f2 == 0 {
		return false
	}
	// 排除单位向量 / 布尔值：一个分量 ±1，其余 0
	one := func(a, b, c float32) bool {
		if a == 1 && b == 0 && c == 0 {
			return true
		}
		if a == 0 && b == 1 && c == 0 {
			return true
		}
		if a == 0 && b == 0 && c == 1 {
			return true
		}
		return false
	}
	if one(f0, f1, f2) {
		return false
	}
	// 水平距离合理
	mag := math.Hypot(x, z)
	if mag < 5 || mag > 7000 {
		return false
	}
	return true
}

// h3fScore 打分。
func h3fScore(v [3]float32) float64 {
	x, alt, z := float64(v[0]), float64(v[1]), float64(v[2])
	s := 0.0
	mag := math.Hypot(x, z)
	if mag > 20 && mag < 6000 {
		s += 40
	}
	if alt > 0 && alt < 2000 {
		s += 40
	}
	// 三个分量都不为 0 更像真坐标
	nz := 0
	for _, f := range v {
		if f != 0 {
			nz++
		}
	}
	if nz >= 2 {
		s += 20
	}
	return s
}

var _ = strconv.Itoa
var _ = time.Now
