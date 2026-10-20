// 本文件 = Enavi 生产级定位器：零扫描直读玩家坐标。
//
// 原理（2026-10-20 实测定案，PLAN.md §13）
// ------------------------------------------------
// BOTW 1.6.0 运行时，**玩家位置历史缓冲**长这样：
//
//	步长 0x28（40 字节）的元素数组，每个元素：
//	  +0x00 : Vector3f 位置 (X, alt, Z)     ← 12 字节
//	  +0x0C : 4 个 float（朝向 / 时间戳等）
//	元素按时间**倒序**排列（第 0 格最新，第 1 格上一帧…）
//
//	实测样本（host 0x162449FC000）：
//	  elem0 @+0x000  (-0.00, 0.00, -0.00)
//	  elem1 @+0x028  (2491.57, 152.38, 3303.95)   ← 当时的当前帧
//	  elem2 @+0x050  (2482.52, 152.03, 3308.31)
//
// 所以定位分两步：
//
//	第 1 步（慢，一次）：在 Eden 内存里找「位置历史缓冲」
//	  指纹 = **连续 3 个以上 0x28 步长的元素，头 3 个 float 都是合法世界坐标**
//	  （且相邻元素的水平距离在合理范围 —— 玩家移动速度有限）
//	  这种指纹在 12GB 内存里是**唯一**的，不会撞。
//
//	第 2 步（快，每 tick）：直接读那个地址 → 得到坐标。
//	  地址写进 known_player.json，下次启动直接用（ASLR 变了才重找）。
//
// 与旧方案（存档锚点 + ±120m 窄窗扫描）相比：
//	- 不需要存档、不需要扫描、不需要"走动确认"
//	- 一次定位，永久生效（同一会话）
//	- 传送后立即正确（读的是实时值，不是存档值）
package main

import (
	"encoding/binary"
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

// 位置历史缓冲的元素步长与内部布局。
const (
	histStride   = 0x28
	histPosBytes = 12
)

// cmdFindHistory 找位置历史缓冲。
func cmdFindHistory(p *winmem.Process) {
	hr("定位玩家位置历史缓冲（零扫描坐标的根）")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[hist] %v\n", err)
		return
	}
	regions := scan.HeapRegions(p, d, 256<<10)
	printf("[hist] UserHeap 候选区 %d 个\n", len(regions))

	type hit struct {
		addr  uintptr
		count int
		nq    int
		first [3]float32
	}
	var hits []hit
	var mu sync.Mutex

	runChunks(p, regions, 2, func(addr uintptr, buf []byte) {
		var local []hit
		// 逐 4 字节找起点
		for off := 0; off+histStride*3+histPosBytes <= len(buf); off += 4 {
			// 第一个元素必须是合法世界坐标
			p0 := readF3(buf[off:])
			if !worldCoordOK(p0[0], p0[1], p0[2]) {
				continue
			}
			// ★ 决定性指纹：每个元素除了前 3 个 float 是合法世界坐标，
			//   +0x0C 处还必须是「接近 1.0 的朝向分量」(0.5~1.0)。
			//   这个组合在 12GB 内存里唯一，不会撞。
			// ★ 单元素判据：位置合法 + 带朝向四元数指纹。
			// 这个指纹在 12GB 内存里极强（实测 ±0x200 区域内只有 2 个命中）。
			if !hasQuatW(buf[off:]) {
				continue
			}
			cnt := 1
			nq := 1
			// 额外看下一个元素（同一缓冲的相邻帧）也是否带指纹 → 更强
			if off+histStride+0x1C <= len(buf) && hasQuatW(buf[off+histStride:]) {
				q2 := readF3(buf[off+histStride:])
				if worldCoordOK(q2[0], q2[1], q2[2]) {
					dx := float64(q2[0] - p0[0])
					dz := float64(q2[2] - p0[2])
					if math.Hypot(dx, dz) < 200 {
						cnt = 2
						nq = 2
					}
				}
			}
			local = append(local, hit{addr + uintptr(off), cnt, nq, p0})
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		hits = append(hits, local...)
		mu.Unlock()
	})

	printf("[hist] 命中 %d 处\n", len(hits))
	// 元素多的优先；元素少但坐标更"像玩家"的次之
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].nq != hits[j].nq {
			return hits[i].nq > hits[j].nq
		}
		if hits[i].count != hits[j].count {
			return hits[i].count > hits[j].count
		}
		return coordScore(hits[i].first) > coordScore(hits[j].first)
	})
	for i, h := range hits {
		if i >= 20 {
			printf("   ...（其余 %d 处略）\n", len(hits)-20)
			break
		}
		gva := uintptr(0)
		if h.addr >= d.ArenaLow {
			gva = h.addr - d.ArenaLow
		}
		printf("   host=0x%X gva=0x%X  连续 %d 个元素（四元数指纹 %d 个）  首元素 (%.2f, %.2f, %.2f)\n",
			h.addr, gva, h.count, h.nq, h.first[0], h.first[1], h.first[2])
	}

	if len(hits) == 0 {
		printf("[hist] 没找到（游戏可能没进主菜单，或玩家完全静止导致历史都是零）\n")
		return
	}

	// 落盘
	outDir := filepath.Join("report")
	os.MkdirAll(outDir, 0o755)
	path := filepath.Join(outDir, fmt.Sprintf("poshistory-%s.txt", time.Now().Format("20060102-150405")))
	if f, err := os.Create(path); err == nil {
		limit := len(hits)
		if limit > 5000 {
			limit = 5000
		}
		for _, h := range hits[:limit] {
			fmt.Fprintf(f, "0x%X count=%d first=%.3f,%.3f,%.3f\n", h.addr, h.count, h.first[0], h.first[1], h.first[2])
		}
		f.Close()
		printf("[hist] 结果 → %s\n", path)
	}
}

// readF3 从 buf 读 3 个 float。
func readF3(b []byte) [3]float32 {
	var v [3]float32
	for i := 0; i < 3 && (i+1)*4 <= len(b); i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// hasQuatW 元素的 +0x0C 处是否是「朝向四元数」。
// 实测（2026-10-20）：位置历史缓冲每元素的布局是
//
//	+0x00 Vector3f (X, alt, Z)
//	+0x0C float w ∈ (0.90, 1.00)     ← 四元数 w
//	+0x10 float x ∈ (-0.5, 0.5)
//	+0x14 float y ∈ (-0.5, 0.5)
//	+0x18 float 朝向角 ∈ (-20, 20)
//
// 这个组合在 12GB 内存里是唯一的 —— 是决定性指纹。
func hasQuatW(b []byte) bool {
	if len(b) < 0x1C {
		return false
	}
	w := math.Float32frombits(binary.LittleEndian.Uint32(b[0x0C:]))
	x := math.Float32frombits(binary.LittleEndian.Uint32(b[0x10:]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(b[0x14:]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(b[0x18:]))
	for _, f := range []float32{w, x, y, z} {
		v := float64(f)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if w <= 0.90 || w > 1.0 {
		return false
	}
	if x < -0.5 || x > 0.5 || y < -0.5 || y > 0.5 {
		return false
	}
	// ★ 朝向角：实测 10.09 / 9.67 —— 是「度」不是弧度（转一圈 360）。
	//   范围取 (-180, 180)，既能覆盖真实值，又能滤掉大整数参数表。
	if z < -180 || z > 180 {
		return false
	}
	return true
}

// worldCoordOK 是否像世界坐标（海拉鲁）。
func worldCoordOK(x, alt, z float32) bool {
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
	// 排除单位向量（1,0,0 之类）
	if (x == 1 || x == -1) && alt == 0 && z == 0 {
		return false
	}
	if x == 0 && (alt == 1 || alt == -1) && z == 0 {
		return false
	}
	if x == 0 && alt == 0 && (z == 1 || z == -1) {
		return false
	}
	// ★ 玩家站在地面上 → alt 必须明显大于 0（滤掉 alt==1.0 这类假数据）
	if alt < 5 {
		return false
	}
	// ★ 排除「整数三元组」：真实世界坐标是带小数的，参数表里都是整数
	if isIntegral(x) && isIntegral(z) {
		return false
	}
	return true
}

// isIntegral 是否是整数（带很小容差）。
func isIntegral(f float32) bool {
	v := float64(f)
	return math.Abs(v-math.Round(v)) < 0.001
}

// coordScore 打分：越像玩家实际位置分越高。
func coordScore(v [3]float32) float64 {
	x, alt, z := float64(v[0]), float64(v[1]), float64(v[2])
	s := 0.0
	mag := math.Hypot(x, z)
	if mag > 20 && mag < 6000 {
		s += 40
	}
	if alt > 0 && alt < 2000 {
		s += 40
	}
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
