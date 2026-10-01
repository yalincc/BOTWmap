package main

// pointer.go — TOTK 位置指针扫描（指针扫描定位方案 S1，2026-10-01）
//
// 实证基础（归档/docs-过程文档-20260929/Xnavi TOTK指针扫描定位方案-v1.md）：
//   1. 精确指针扫描（v==镜像地址）全内存 0 命中（diagnostic.go 长期实测）
//   2. 同组 8~30 个副本坐标相近 → 坐标槽位是"位置历史环形缓冲"的成员
//   3. 会话内地址稳定（0xABBC91A9060 跟随 5 分钟+跨传送实证）
// 因此本文件做**范围指针扫描**：找"指向缓冲区基址范围 [M-0x4000, M+0x10] 的指针"，
// 解引用后统计环内与当前镜像同值的槽位，验证环形缓冲假设并为 S2 建链采数据。
//
// TOTK 专属（Ryujinx 小端）；BOTW/Cemu 不调用本文件任何函数。

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	// Q18: 0x4000(16KB) 实测 0 命中（21:11 会话 build failed: no range pointers）——
	// 环缓冲可能远大于 16KB（全 actor 位置池），加宽到 1MB；代价是候选数上升，
	// 由解引用+槽位匹配过滤
	ptrScanRangeBack = 0x100000          // 指针目标值下界：M-1MB（环基址到当前槽的最大距离）
	ptrScanRangeFwd  = 0x10              // 上界：M+16B
	ptrScanMaxLog    = 8                 // 日志最多列出的基址数
	ptrRingWindow    = 0x4000            // S1 报告的默认解引用窗口
)

type ptrHit struct {
	PtrAddr uintptr // 指针所在的内存地址
	Target  uintptr // 指针值（疑似环基址，落在 [M-0x4000, M+0x10) 内）
}

// ptrAgg 范围指针按目标基址聚合后的统计（一个 agg ≈ 一个疑似环缓冲）。
type ptrAgg struct {
	Base     uintptr
	PtrCount int
	PtrAddrs []uintptr
	SlotHits int       // 环窗口内与当前镜像坐标近似的槽位数
	ExactOff int       // 与当前镜像完全一致的槽偏移（-1=无）
	Mirror   uintptr   // 建链时的镜像地址（base+MirrorOff 关系）
	Sample   [3]float32
}

// pointerScanCollect 全内存范围指针扫描 + 解引用分析（S1/S2 共用）。
// 返回按指针数量降序的基址聚合列表（只保留 slotHits>0 或 ptrs>=2 的）。
func pointerScanCollect(mirror uintptr, coords [3]float32) []*ptrAgg {
	p := currentPlatform()
	if p == nil || !p.LittleEndian() || mirror == 0 {
		return nil
	}
	h := p.Handle()
	if h == 0 {
		return nil
	}
	blocks := p.Blocks(minBlockMB)
	lo, hi := int64(mirror)-ptrScanRangeBack, int64(mirror)+ptrScanRangeFwd

	// 1) 全内存范围指针扫描（复用 diagnostic.go 的分块读模式）
	var hits []ptrHit
	for _, b := range blocks {
		var off uintptr
		for off < b.Size {
			n := uintptr(chunkSize)
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if len(buf) < 8 {
				off += n
				continue
			}
			for i := 0; i+8 <= len(buf); i += 8 {
				v := int64(binary.LittleEndian.Uint64(buf[i : i+8]))
				if v >= lo && v < hi {
					hits = append(hits, ptrHit{PtrAddr: b.Base + off + uintptr(i), Target: uintptr(v)})
				}
			}
			off += n
		}
	}
	if len(hits) == 0 {
		return nil
	}

	// 2) 按目标基址聚合（±0x40 视为同一基址，指针可能指向环内任意成员）
	type aggT struct {
		base     uintptr
		count    int
		ptrAddrs []uintptr
	}
	var aggs []*aggT
	for _, hit := range hits {
		found := false
		for _, a := range aggs {
			if hit.Target >= a.base-0x40 && hit.Target <= a.base+0x40 {
				a.count++
				a.ptrAddrs = append(a.ptrAddrs, hit.PtrAddr)
				if hit.Target < a.base {
					a.base = hit.Target
				}
				found = true
				break
			}
		}
		if !found {
			aggs = append(aggs, &aggT{base: hit.Target, count: 1, ptrAddrs: []uintptr{hit.PtrAddr}})
		}
	}
	sort.Slice(aggs, func(i, j int) bool { return aggs[i].count > aggs[j].count })

	// 3) 解引用各基址：读环窗口，统计与当前镜像近似的槽位
	var out []*ptrAgg
	for _, a := range aggs {
		pa := &ptrAgg{Base: a.base, PtrCount: a.count, PtrAddrs: a.ptrAddrs, ExactOff: -1, Mirror: mirror, Sample: coords}
		win := readMem(h, a.base, ptrRingWindow)
		if len(win) >= 12 {
			for i := 0; i+12 <= len(win); i += 4 { // 4 字节对齐搜三元组
				x := float32(math.Float32frombits(binary.LittleEndian.Uint32(win[i:])))
				if math.Abs(float64(x-coords[0])) > 1.0 {
					continue
				}
				y := float32(math.Float32frombits(binary.LittleEndian.Uint32(win[i+4:])))
				z := float32(math.Float32frombits(binary.LittleEndian.Uint32(win[i+8:])))
				if math.Abs(float64(y-coords[1])) > 1.0 || math.Abs(float64(z-coords[2])) > 1.0 {
					continue
				}
				pa.SlotHits++
				if x == coords[0] && y == coords[1] && z == coords[2] {
					pa.ExactOff = i
				}
			}
		}
		if pa.SlotHits > 0 || pa.PtrCount >= 2 {
			out = append(out, pa)
		}
	}
	return out
}

// pointerScanReport S1 数据采集报告（锁定确认后后台跑一次）。
func pointerScanReport(mirror uintptr, coords [3]float32) {
	go func() {
		defer func() { recover() }()
		t0 := time.Now()
		aggs := pointerScanCollect(mirror, coords)
		if aggs == nil {
			fmt.Printf("  [ptrscan] mirror=0x%X: 0 range pointers (%.1fs) — 环基址假设存疑\n",
				mirror, time.Since(t0).Seconds())
			return
		}
		fmt.Printf("  [ptrscan] mirror=0x%X coords=(%.1f,%.1f,%.1f): %d bases (%.1fs)\n",
			mirror, coords[0], coords[1], coords[2], len(aggs), time.Since(t0).Seconds())
		for i, a := range aggs {
			if i >= ptrScanMaxLog {
				break
			}
			fmt.Printf("  [ptrscan]   base=0x%X ptrs=%d slotHits=%d exactOff=%#x (mirrorOff=%#x)\n",
				a.Base, a.PtrCount, a.SlotHits, a.ExactOff, a.Mirror-a.Base)
		}
		fmt.Printf("  [ptrscan] done in %.1fs — slotHits>0 的基址即候选环缓冲，S2 据此建链\n",
			time.Since(t0).Seconds())
	}()
}
