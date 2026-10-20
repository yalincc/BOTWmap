// 本文件 = 扫 Eden 内存里所有 actor 的属性表，找出「装着坐标」的槽。
//
// 背景（PLAN.md §12.3）：BOTW 的 actor 用「属性表」存各种运行时值：
//
//	actor +0xB8 = 属性个数
//	actor +0xC0 = 属性表指针（每项 0x50）
//	项 +0x20 = 类型代号（2 = Vec3f）
//	项 +0x28 = 值数组指针（每项 0x98）
//	值 +0x10 = float 值
//
// 玩家坐标 = 玩家 actor 的某个 Vec3f 属性。
// 本命令的做法：
//  1. 全进程扫「8 字节指针」，凡是指向 UserHeap 的都当候选 actor；
//  2. 对每个候选，检查它是不是「actor」：
//     +0xB8 处的 u32 在 1..64（属性个数合理）且
//     +0xC0 处的指针在 UserHeap（属性表）；
//  3. 对真 actor，遍历属性表，找类型==2 且值像坐标（|X|,|Z|≤8000，alt∈[-600,6000]）的项；
//  4. 输出所有命中，并按「值的量级合理」排序。
//
// 用法：enavi-probe -cmd scanactors [-pid N]
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

// scanReadTO 扫���时的单次读超时。
const scanReadTO = 500 * time.Millisecond

func cmdScanActors(p *winmem.Process) {
	hr("扫描所有 actor 的属性表，找装着坐标的槽")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[actors] %v\n", err)
		return
	}
	printf("[actors] arenaLow=0x%X  UserHeap=0x%X..0x%X\n", d.ArenaLow, d.HeapLo(), d.HeapHi())

	// 候选 actor：所有指向 UserHeap 的指针
	// 默认只扫 UserHeap（玩家/actor 都在堆上），全进程扫太重会拖死模拟器。
	var regions []winmem.Region
	if *scanAll {
		regions = p.Committed(64 << 10)
	} else {
		regions = scan.HeapRegions(p, d, 64<<10)
	}
	printf("[actors] 候选区 %d 个（%s）\n", len(regions),
		map[bool]string{true: "全进程（慢！）", false: "仅 UserHeap"}[*scanAll])

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
	printf("[actors] 候选指针 %d 个\n", len(cands))

	// 检查每个候选是不是 actor
	type vecHit struct {
		actor  uintptr
		idx    int
		tbl    uintptr
		vals   uintptr
		pos    [3]float32
	}
	var hits []vecHit
	actors := 0
	checked := 0
	for _, c := range cands {
		checked++
		if checked > 120000 {
			break
		}
		// +0xB8 属性个数
		cntBuf, err := p.ReadTimeout(c.val+actorCountOff, 4, scanReadTO)
		if err != nil || len(cntBuf) < 4 {
			continue
		}
		cnt := binary.LittleEndian.Uint32(cntBuf)
		if cnt == 0 || cnt > 64 {
			continue
		}
		// +0xC0 属性表指针
		tblBuf, err := p.ReadTimeout(c.val+actorTableOff, 8, scanReadTO)
		if err != nil || len(tblBuf) < 8 {
			continue
		}
		tblRaw := binary.LittleEndian.Uint64(tblBuf)
		tbl, ok := heapAddrOf(d, uintptr(tblRaw))
		if !ok {
			continue
		}
		actors++
		// 遍历属性项
		for i := 0; i < int(cnt) && i < 64; i++ {
			item := tbl + uintptr(i)*attrStride
			tBuf, err := p.ReadTimeout(item+attrTypeOff, 4, scanReadTO)
			if err != nil || len(tBuf) < 4 {
				break
			}
			typ := binary.LittleEndian.Uint32(tBuf)
			if typ != uint32(posAttrType) {
				continue
			}
			vBuf, err := p.ReadTimeout(item+attrValsOff, 8, scanReadTO)
			if err != nil || len(vBuf) < 8 {
				continue
			}
			vals, ok := heapAddrOf(d, uintptr(binary.LittleEndian.Uint64(vBuf)))
			if !ok {
				continue
			}
			fBuf, err := p.ReadTimeout(vals+valFloatOff, 12, scanReadTO)
			if err != nil || len(fBuf) < 12 {
				continue
			}
			var pos [3]float32
			for k := 0; k < 3; k++ {
				pos[k] = math.Float32frombits(binary.LittleEndian.Uint32(fBuf[k*4:]))
			}
			if !posLooksLikeCoord(pos) {
				continue
			}
			hits = append(hits, vecHit{c.val, i, tbl, vals, pos})
		}
	}

	printf("[actors] 检查 %d 个候选，识别出 actor %d 个\n", checked, actors)
	printf("[actors] 装着「像坐标的 Vec3f」的 actor %d 个\n", len(hits))

	// 按坐标量级排序（越像真实世界坐标越前）
	sort.Slice(hits, func(i, j int) bool {
		return saCoordScore(hits[i].pos) > coordScore(hits[j].pos)
	})
	for i, h := range hits {
		if i >= 30 {
			printf("   ...（其余 %d 个略）\n", len(hits)-30)
			break
		}
		printf("   actor=0x%X  项%-2d 表=0x%X 值=0x%X  (%.2f, %.2f, %.2f)\n",
			h.actor, h.idx, h.tbl, h.vals, h.pos[0], h.pos[1], h.pos[2])
	}

	// 落盘
	outDir := filepath.Join("report")
	os.MkdirAll(outDir, 0o755)
	path := filepath.Join(outDir, fmt.Sprintf("scanactors-%s.txt", time.Now().Format("20060102-150405")))
	f, err := os.Create(path)
	if err == nil {
		for _, h := range hits {
			fmt.Fprintf(f, "actor=0x%X idx=%d tbl=0x%X vals=0x%X pos=%.4f,%.4f,%.4f\n",
				h.actor, h.idx, h.tbl, h.vals, h.pos[0], h.pos[1], h.pos[2])
		}
		f.Close()
		printf("[actors] 结果已写入 %s\n", path)
	}
}

// heapAddrOf 把指针值校正成宿主地址（指针可能存 guest VA）。
func heapAddrOf(d winmem.Dram, h uintptr) (uintptr, bool) {
	if h < d.HeapLo() || h >= d.HeapHi() {
		h += d.ArenaLow
	}
	if h >= d.HeapLo() && h < d.HeapHi() {
		return h, true
	}
	return 0, false
}

// posLooksLikeCoord 坐标合理性。
func posLooksLikeCoord(v [3]float32) bool {
	if math.IsNaN(float64(v[0])) || math.IsNaN(float64(v[1])) || math.IsNaN(float64(v[2])) {
		return false
	}
	if math.Abs(float64(v[0])) > 8000 || math.Abs(float64(v[2])) > 8000 {
		return false
	}
	if v[1] < -600 || v[1] > 6000 {
		return false
	}
	if v[0] == 0 && v[1] == 0 && v[2] == 0 {
		return false
	}
	return true
}

// coordScore 给坐标打个「像真实世界坐标」的分。
func saCoordScore(v [3]float32) float64 {
	s := 0.0
	if v[0] != 0 || v[2] != 0 {
		s += 50 // 有水平位移
	}
	if v[1] > 0 {
		s += 20 // 高度为正（地面以上）
	}
	mag := math.Hypot(float64(v[0]), float64(v[2]))
	if mag > 10 && mag < 6000 {
		s += 30
	}
	return s
}
