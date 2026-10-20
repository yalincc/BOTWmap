// 本文件 = 宽松版「找装着玩家坐标的属性项」。
//
// 为什么需要：进游戏后玩家 actor 的属性表与标题界面时不同 ——
// 类型代号不再简单是 2，且 Vec3f 项的分布也变了。
// 所以这里不预设类型，改成「结构 + 数值合理性」双重判据：
//
//	候选 actor：+0xB8 处的 u32 在 1..64（属性个数）且 +0xC0 指向堆
//	属性项：每项 0x50，+0x20 是类型代号，+0x28 是值数组指针
//	值项：每项 0x98，+0x10 起是 float
//	→ 遍历所有项的所有值槽，找出「3 个连续 float 且落在世界坐标范围内」的
//
// 用法：enavi-probe -cmd findcoord [-pid N] [-range 8000]
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

// findCoordRange 坐标水平范围（米）。
var findCoordRange = flag.Int("range", 8000, "坐标水平范围上限（米）")

func cmdFindCoord(p *winmem.Process) {
	hr("宽松扫描：找装着玩家坐标的属性项")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[fc] %v\n", err)
		return
	}

	// 候选 actor：扫 UserHeap 里的指针
	regions := scan.HeapRegions(p, d, 64<<10)
	printf("[fc] UserHeap 候选区 %d 个\n", len(regions))

	type ptrCand struct{ host, val uintptr }
	var cands []ptrCand
	seen := map[uintptr]bool{}
	var mu sync.Mutex

	runChunks(p, regions, 2, func(addr uintptr, buf []byte) {
		for off := 0; off+8 <= len(buf); off += 8 {
			v := binary.LittleEndian.Uint64(buf[off:])
			if v == 0 {
				continue
			}
			for _, gv := range []uintptr{uintptr(v), uintptr(v) + d.ArenaLow} {
				if gv < d.HeapLo() || gv >= d.HeapHi() {
					continue
				}
				h := addr + uintptr(off)
				mu.Lock()
				if !seen[h] {
					seen[h] = true
					cands = append(cands, ptrCand{h, gv})
				}
				mu.Unlock()
			}
		}
	})
	printf("[fc] 候选指针 %d 个\n", len(cands))

	type hit struct {
		actor uintptr
		idx   int
		slot  int // 值项里的第几个（0x98 步长）
		off   uintptr
		pos   [3]float32
	}
	var hits []hit
	actors := 0
	checked := 0
	lim := float64(*findCoordRange)

	for _, c := range cands {
		checked++
		if checked > 150000 {
			break
		}
		cntBuf, err := p.ReadTimeout(c.val+actorCountOff, 4, scanReadTO)
		if err != nil || len(cntBuf) < 4 {
			continue
		}
		cnt := binary.LittleEndian.Uint32(cntBuf)
		if cnt == 0 || cnt > 64 {
			continue
		}
		tblBuf, err := p.ReadTimeout(c.val+actorTableOff, 8, scanReadTO)
		if err != nil || len(tblBuf) < 8 {
			continue
		}
		tbl, ok := heapAddrOf(d, uintptr(binary.LittleEndian.Uint64(tblBuf)))
		if !ok {
			continue
		}
		actors++
		for i := 0; i < int(cnt) && i < 64; i++ {
			item := tbl + uintptr(i)*attrStride
			vBuf, err := p.ReadTimeout(item+attrValsOff, 8, scanReadTO)
			if err != nil || len(vBuf) < 8 {
				break
			}
			vals, ok := heapAddrOf(d, uintptr(binary.LittleEndian.Uint64(vBuf)))
			if !ok {
				continue
			}
			// 一个属性项的值数组里可能有多个值（步长 0x98），逐个试
			for slot := 0; slot < 4; slot++ {
				base := vals + uintptr(slot)*valStride + valFloatOff
				fBuf, err := p.ReadTimeout(base, 12, scanReadTO)
				if err != nil || len(fBuf) < 12 {
					break
				}
				var pos [3]float32
				for k := 0; k < 3; k++ {
					pos[k] = math.Float32frombits(binary.LittleEndian.Uint32(fBuf[k*4:]))
				}
				if !floatOK(pos, lim) {
					continue
				}
				hits = append(hits, hit{c.val, i, slot, base, pos})
			}
		}
	}

	printf("[fc] 检查 %d 个候选，识别 actor %d 个，找到坐标样值 %d 处\n", checked, actors, len(hits))
	// 按「离存档坐标近」排序最像玩家
	sort.Slice(hits, func(i, j int) bool { return coordScore2(hits[i].pos) > coordScore2(hits[j].pos) })
	for i, h := range hits {
		if i >= 40 {
			printf("   ...（其余 %d 处略）\n", len(hits)-40)
			break
		}
		printf("   actor=0x%X 项%-2d 槽%d 偏移=0x%X  (%.2f, %.2f, %.2f)\n",
			h.actor, h.idx, h.slot, h.off, h.pos[0], h.pos[1], h.pos[2])
	}

	outDir := filepath.Join("report")
	os.MkdirAll(outDir, 0o755)
	path := filepath.Join(outDir, fmt.Sprintf("findcoord-%s.txt", time.Now().Format("20060102-150405")))
	if f, err := os.Create(path); err == nil {
		for _, h := range hits {
			fmt.Fprintf(f, "actor=0x%X idx=%d slot=%d off=0x%X pos=%.4f,%.4f,%.4f\n",
				h.actor, h.idx, h.slot, h.off, h.pos[0], h.pos[1], h.pos[2])
		}
		f.Close()
		printf("[fc] 结果 → %s\n", path)
	}
}

// floatOK 三个 float 是否像世界坐标。
func floatOK(v [3]float32, lim float64) bool {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	if math.Abs(float64(v[0])) > lim || math.Abs(float64(v[2])) > lim {
		return false
	}
	if v[1] < -600 || v[1] > 6000 {
		return false
	}
	// 排除全零 / 全 -0.0（未初始化的槽）
	allZero := true
	for _, x := range v {
		if x != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return false
	}
	// 至少要有一个明显非零的量（滤掉 -0.000 这类噪声）
	mag := math.Hypot(float64(v[0]), float64(v[2]))
	if mag < 0.5 && math.Abs(float64(v[1])) < 0.5 {
		return false
	}
	return true
}

// coordScore2 打分：越像真实世界坐标分越高。
func coordScore2(v [3]float32) float64 {
	s := 0.0
	mag := math.Hypot(float64(v[0]), float64(v[2]))
	if mag > 50 && mag < 6000 {
		s += 60
	}
	if v[1] > 0 && v[1] < 2000 {
		s += 30
	}
	if v[0] != 0 || v[2] != 0 {
		s += 10
	}
	return s
}

var _ = strconv.Itoa
var _ = sync.Mutex{}
