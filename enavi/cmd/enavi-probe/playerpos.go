// 本文件 = 按静态分析结论，在 Eden 内存里「零扫描」读玩家坐标。
//
// 静态分析结论（见 PLAN.md §12）：
//
//	PlayerInfo 单例 → [+0x560] = actor
//	actor           → [+0xB8]  = 属性个数
//	actor           → [+0xC0]  = 属性表指针（每项 0x50）
//	属性项          → [+0x20]  = 属性类型代号
//	属性项          → [+0x28]  = 值数组指针（每项 0x98）
//	值项            → [+0x10]  = float 值（坐标 = 3 个连续 float: X, alt, Z）
//
//	PlayerInfo::getPlayerPos 查询的属性索引 = 3，类型 = 2（Vec3f）
//
// 定位 PlayerInfo 单例的办法（不靠符号，靠数据结构自证）：
//	候选 = Eden 模块区里所有「指向一个堆对象的指针」；
//	对该对象检查：+0x560 处的指针指向的对象，其 +0xC0 指向的属性表里
//	第 3 项类型为 2、值数组 +0x10 处是 3 个合理坐标 → 命中即为 PlayerInfo。
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

	"botwmap/enavi/internal/winmem"
)

// 结构偏移常量见 watchpos.go（统一维护）

// cmdLocatePlayer 找 PlayerInfo 并读坐标。
func cmdLocatePlayer(p *winmem.Process) {
	hr("按静态分析结论直读玩家坐标（零扫描）")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[pos] %v\n", err)
		return
	}
	printf("[pos] arenaLow=0x%X\n", d.ArenaLow)

	// 候选来源：模块区 + UserHeap 里所有「指向堆对象的指针」。
	// PlayerInfo 单例是静态全局变量，它本身在 .bss/.data（模块区）里。
	// 所以先扫**模块区的可写段**（.data/.bss），找候选指针。
	var regions []winmem.Region
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.Readable() && r.Size >= 0x1000 {
			regions = append(regions, r)
		}
		return false
	})
	var bytes uint64
	for _, r := range regions {
		bytes += uint64(r.Size)
	}
	printf("[pos] 候选区 %d 个 / %s（模块区：.data/.bss 里的全局指针）\n", len(regions), gb(bytes))

	type cand struct {
		host uintptr // 指针所在处
		ptr  uintptr // 指针值
	}
	var cands []cand
	seen := map[uintptr]bool{}
	var mu sync.Mutex

	// 逐指针宽度扫描：候选指针必须落在 UserHeap 里（对象在堆上）
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		for off := 0; off+8 <= len(buf); off += 8 {
			v := binary.LittleEndian.Uint64(buf[off:])
			if v == 0 {
				continue
			}
			// guest VA 口径：指针可能存 guest VA，也可能存宿主地址
			for _, gv := range []uintptr{uintptr(v), uintptr(v) + d.ArenaLow} {
				if gv < d.HeapLo() || gv >= d.HeapHi() {
					continue
				}
				// 排除明显不是堆对象的（小偏移、页对齐等）
				if gv%0x200 == 0 {
					continue
				}
				h := addr + uintptr(off)
				mu.Lock()
				if seen[h] {
					mu.Unlock()
					continue
				}
				seen[h] = true
				cands = append(cands, cand{h, gv})
				mu.Unlock()
			}
		}
	})
	printf("[pos] 候选指针 %d 个\n", len(cands))
	if len(cands) == 0 {
		return
	}

	// 逐个验证：对象 +0x560 → actor → actor+0xC0 → 属性表 → 第 3 项
	type hitRec struct {
		c        cand
		actor    uintptr
		attrHost uintptr
		pos      [3]float32
	}
	var hits []hitRec
	var checked int
	var statNoActor, statActorNotHeap, statActorOK int
	var statNoTable, statTableNotHeap, statTableOK int
	var statTypeOK int
	statTypeSeen := map[uint32]int{}

	for _, c := range cands {
		if checked > 400000 {
			break
		}
		checked++
		// 读 PlayerInfo+0x560
		actorHost, ok := ppReadPtrAt(p, c.ptr, piActorOff)
		if !ok || actorHost == 0 {
			statNoActor++
			continue
		}
		// actor 必须在堆里（两种口径都试）
		actorHost2 := actorHost
		if actorHost2 < d.HeapLo() || actorHost2 >= d.HeapHi() {
			actorHost2 += d.ArenaLow
		}
		if actorHost2 < d.HeapLo() || actorHost2 >= d.HeapHi() {
			statActorNotHeap++
			continue
		}
		statActorOK++
		// 读 actor+0xC0（属性表）
		tblHost, ok := ppReadPtrAt(p, actorHost2, actorTableOff)
		if !ok || tblHost == 0 {
			statNoTable++
			continue
		}
		if tblHost < d.HeapLo() || tblHost >= d.HeapHi() {
			tblHost += d.ArenaLow
		}
		if tblHost < d.HeapLo() || tblHost >= d.HeapHi() {
			statTableNotHeap++
			continue
		}
		statTableOK++
		// 属性表第 3 项
		item := tblHost + uintptr(posAttrIndex)*attrStride
		typ, ok := ppReadU32(p, item, attrTypeOff)
		if !ok {
			continue
		}
		statTypeSeen[uint32(typ)]++
		if uint32(typ) != uint32(posAttrType) {
			continue
		}
		statTypeOK++
		// 值数组
		valsHost, ok := ppReadPtrAt(p, item, attrValsOff)
		if !ok || valsHost == 0 {
			continue
		}
		if valsHost < d.HeapLo() || valsHost >= d.HeapHi() {
			valsHost += d.ArenaLow
		}
		if valsHost < d.HeapLo() || valsHost >= d.HeapHi() {
			continue
		}
		// 值 +0x10 起 3 个 float
		buf, err := p.Read(valsHost+valFloatOff, 12)
		if err != nil || len(buf) < 12 {
			continue
		}
		var pos [3]float32
		for i := 0; i < 3; i++ {
			pos[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
		}
		// 合理性：坐标范围
		if !coordOK(pos) {
			_ = pos
			// 记下来：属性表找到了、类型也对，但值不像坐标 → 打印全部属性项
			if statTypeOK <= 12 {
				printf("\n  ◇ 候选表 host=0x%X  第%d项 类型=2 值=(%.3f, %.3f, %.3f)\n",
					tblHost, posAttrIndex, pos[0], pos[1], pos[2])
				// 打印该表所有项
				for ai := 0; ai < 12; ai++ {
					it := tblHost + uintptr(ai)*attrStride
					tp, _ := ppReadU32(p, it, attrTypeOff)
					vp, _ := ppReadPtrAt(p, it, attrValsOff)
					if vp == 0 {
						continue
					}
					if vp < d.HeapLo() || vp >= d.HeapHi() {
						vp += d.ArenaLow
					}
					if vp < d.HeapLo() || vp >= d.HeapHi() {
						continue
					}
					vb, e := p.Read(vp+valFloatOff, 12)
					if e != nil || len(vb) < 12 {
						continue
					}
					var f3 [3]float32
					for i := 0; i < 3; i++ {
						f3[i] = math.Float32frombits(binary.LittleEndian.Uint32(vb[i*4:]))
					}
					mark := ""
					if coordOK(f3) {
						mark = "  ★像坐标"
					}
					printf("      项%-2d 类型=%-3d 值=(%.3f, %.3f, %.3f) %s\n",
						ai, tp, f3[0], f3[1], f3[2], mark)
				}
			}
			continue
		}
		hits = append(hits, hitRec{c, actorHost, valsHost, pos})
		if len(hits) >= 8 {
			break
		}
	}

	printf("\n[pos] 分层诊断：\n")
	printf("   无 actor(+0x560)=0      : %d\n", statNoActor)
	printf("   actor 不在 UserHeap     : %d\n", statActorNotHeap)
	printf("   actor 在堆里 ✓          : %d\n", statActorOK)
	printf("   属性表指针无效          : %d\n", statNoTable)
	printf("   属性表不在堆里          : %d\n", statTableNotHeap)
	printf("   属性表在堆里 ✓          : %d\n", statTableOK)
	printf("   类型==2(命中)           : %d\n", statTypeOK)
	fmt.Printf("   见过的类型代号: ")
	keys := make([]int, 0, len(statTypeSeen))
	for k := range statTypeSeen {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("%d×%d ", k, statTypeSeen[uint32(k)])
	}
	fmt.Println()
	printf("[pos] 检查 %d 个候选，命中 %d 个\n", checked, len(hits))
	for i, h := range hits {
		gva := uintptr(0)
		if h.c.ptr >= d.ArenaLow {
			gva = h.c.ptr - d.ArenaLow
		}
		printf("\n  ★ 命中 %d\n", i)
		printf("    PlayerInfo 单例指针  host=0x%X gva=0x%X\n", h.c.host, gva)
		printf("    actor               host=0x%X\n", h.actor)
		printf("    坐标值数组           host=0x%X\n", h.attrHost)
		printf("    坐标 = (%.2f, %.2f, %.2f)\n", h.pos[0], h.pos[1], h.pos[2])
	}

	if len(hits) > 0 {
		outDir := filepath.Join("report")
		os.MkdirAll(outDir, 0o755)
		name := filepath.Join(outDir, fmt.Sprintf("playerpos-%s.txt", time.Now().Format("20060102-150405")))
		f, _ := os.Create(name)
		if f != nil {
			for _, h := range hits {
				fmt.Fprintf(f, "playerInfoPtrHost=0x%X actorHost=0x%X valsHost=0x%X pos=%.3f,%.3f,%.3f\n",
					h.c.host, h.actor, h.attrHost, h.pos[0], h.pos[1], h.pos[2])
			}
			f.Close()
			printf("\n[pos] 结果已写入 %s\n", name)
		}
	}
}

// readPtrAt 读 (base+off) 处的 64 位指针，返回宿主地址。
// 指针可能是 guest VA（0x21xx…）也可能是宿主地址，两种都试。
func ppReadPtrAt(p *winmem.Process, base uintptr, off uintptr) (uintptr, bool) {
	buf, err := p.Read(base+off, 8)
	if err != nil || len(buf) < 8 {
		return 0, false
	}
	v := binary.LittleEndian.Uint64(buf)
	return uintptr(v), v != 0
}

// readU32 读 (base+off) 处的 32 位无符号数。
func ppReadU32(p *winmem.Process, base uintptr, off uintptr) (uint32, bool) {
	buf, err := p.Read(base+off, 4)
	if err != nil || len(buf) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(buf), true
}

// coordOK 坐标合理性判断。
func coordOK(v [3]float32) bool {
	if math.IsNaN(float64(v[0])) || math.IsNaN(float64(v[1])) || math.IsNaN(float64(v[2])) {
		return false
	}
	if math.Abs(float64(v[0])) > 8000 || math.Abs(float64(v[2])) > 8000 {
		return false
	}
	if v[1] < -600 || v[1] > 6000 {
		return false
	}
	// 三个值不能全为 0
	if v[0] == 0 && v[1] == 0 && v[2] == 0 {
		return false
	}
	return true
}

var _ = sort.Ints
