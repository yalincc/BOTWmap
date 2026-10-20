// 本文件 = 双采样差分找「实时变化」的世界坐标槽。
//
// 动机
// ----
// 位置历史缓冲（findhistory）只在**玩家移动时**更新 —— 玩家静止时它是"最后位置"，
// 不是帧级实时坐标。要拿实时坐标，就得找**每帧都在变**的那个槽。
//
// 方法
// ----
// 1. 第一次采样：把 UserHeap 里所有「合法世界坐标三元组」的地址+值记下来；
// 2. 等一小段时间（玩家走动）；
// 3. 第二次采样：同地址再读一次；
// 4. 只有「两次都合法 且 值变了 且 变化量在合理范围」的槽才留下 ——
//    这排除了所有静态表、存档、配置数据。
//
// 判据：
//   - 位置合法（世界尺度）
//   - 两次值都合法
//   - 水平位移 > 0.05m（真的在变）且 < 500m（不是野指针/重分配）
//
// 用法：enavi-probe -cmd livecoord -pid N [-wait 3] [-secs 6]
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

var (
	liveWait  = flag.Int("wait", 3, "两次采样之间的间隔（秒）")
	liveSecs  = flag.Int("lsecs", 6, "总采样时长（秒）")
	liveTop   = flag.Int("ntop2", 40, "输出前 N 个")
)

func cmdLiveCoord(p *winmem.Process) {
	hr("双采样差分：找实时变化的世界坐标")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[live] %v\n", err)
		return
	}
	regions := scan.HeapRegions(p, d, 256<<10)
	printf("[live] UserHeap 候选区 %d 个\n", len(regions))

	// ---- 第一次采样 ----
	type slot struct {
		addr uintptr
		v    [3]float32
	}
	first := map[uintptr][3]float32{}
	var mu sync.Mutex

	sample := func() map[uintptr][3]float32 {
		out := map[uintptr][3]float32{}
		var lmu sync.Mutex
		runChunks(p, regions, 2, func(addr uintptr, buf []byte) {
			var local []slot
			for off := 0; off+12 <= len(buf); off += 4 {
				f0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
				f1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
				f2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
				if !liveCoordOK(f0, f1, f2) {
					continue
				}
				local = append(local, slot{addr + uintptr(off), [3]float32{f0, f1, f2}})
			}
			if len(local) == 0 {
				return
			}
			lmu.Lock()
			for _, s := range local {
				out[s.addr] = s.v
			}
			lmu.Unlock()
		})
		return out
	}

	printf("[live] 第一次采样…\n")
	first = sample()
	printf("[live] 第一次命中 %d 个槽\n", len(first))

	// 期间玩家要走动
	printf("[live] 等待 %d 秒（请让角色在游戏里走动）…\n", *liveWait)
	time.Sleep(time.Duration(*liveWait) * time.Second)

	printf("[live] 第二次采样…\n")
	second := sample()
	printf("[live] 第二次命中 %d 个槽\n", len(second))

	// ---- 差分 ----
	type diff struct {
		addr uintptr
		from [3]float32
		to   [3]float32
		d    float64
	}
	var diffs []diff
	mu.Lock()
	for a, v1 := range first {
		v2, ok := second[a]
		if !ok {
			continue
		}
		if !liveCoordOK(v2[0], v2[1], v2[2]) {
			continue
		}
		dx := float64(v2[0] - v1[0])
		dz := float64(v2[2] - v1[2])
		dist := math.Hypot(dx, dz)
		if dist < 0.05 || dist > 500 {
			continue
		}
		diffs = append(diffs, diff{a, v1, v2, dist})
	}
	mu.Unlock()

	printf("\n[live] ★ 两次采样都在变的世界坐标槽：%d 个\n", len(diffs))
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].d > diffs[j].d })
	for i, df := range diffs {
		if i >= *liveTop {
			printf("   ...（其余 %d 个略）\n", len(diffs)-*liveTop)
			break
		}
		gva := uintptr(0)
		if df.addr >= d.ArenaLow {
			gva = df.addr - d.ArenaLow
		}
		printf("   host=0x%X gva=0x%X  (%.2f, %.2f, %.2f) → (%.2f, %.2f, %.2f)  位移 %.2f m\n",
			df.addr, gva, df.from[0], df.from[1], df.from[2], df.to[0], df.to[1], df.to[2], df.d)
	}

	outDir := filepath.Join("report")
	os.MkdirAll(outDir, 0o755)
	path := filepath.Join(outDir, fmt.Sprintf("livecoord-%s.txt", time.Now().Format("20060102-150405")))
	if f, err := os.Create(path); err == nil {
		for _, df := range diffs {
			fmt.Fprintf(f, "0x%X %.4f %.4f %.4f → %.4f %.4f %.4f  d=%.3f\n",
				df.addr, df.from[0], df.from[1], df.from[2], df.to[0], df.to[1], df.to[2], df.d)
		}
		f.Close()
		printf("[live] 结果 → %s\n", path)
	}
}

// liveCoordOK 合法世界坐标（比 worldCoordOK 略宽：alt 允许较小值）。
func liveCoordOK(x, alt, z float32) bool {
	for _, f := range []float32{x, alt, z} {
		v := float64(f)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if math.Abs(float64(x)) > 8000 || math.Abs(float64(z)) > 8000 {
		return false
	}
	if alt < -600 || alt > 6000 {
		return false
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	// 排除整数三元组（参数表）
	if isIntegral(x) && isIntegral(z) {
		return false
	}
	return true
}
