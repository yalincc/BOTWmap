// 本文件 = 按静态分析结论持续读玩家坐标（验证用）。
//
// 链路（PLAN.md §12.3）：
//	PlayerInfo 单例 → [+0x560] = actor
//	actor → [+0xC0] = 属性表（每项 0x50）
//	属性项 → [+0x20] 类型 / [+0x28] 值数组（每项 0x98）
//	值项 → [+0x10] = float（坐标 = 3 个连续 float: X, alt, Z）
//
// 定位 PlayerInfo 单例：先扫模块区里的全局指针（候选），
//	再用「对象结构自证」筛出真正的 PlayerInfo：
//	  对象+0x560 → actor（在堆）
//	  actor+0xC0 → 属性表（在堆）
//	  属性表第 idx 项类型 == type
//	筛出后每 tick 读一次坐标。
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
	"strings"
	"sync"
	"time"

	"botwmap/enavi/internal/winmem"
)

const (
	piActorOff    = 0x560
	actorCountOff = 0xB8
	actorTableOff = 0xC0
	attrStride    = 0x50
	attrTypeOff   = 0x20
	attrValsOff   = 0x28
	valStride     = 0x98
	valFloatOff   = 0x10
)

var (
	posAttrIndex = 3
	posAttrType  = 2
	posScanAll   = false
	verboseAttrs = false
	knownPtrs    = flag.String("ptr", "", "已知 PlayerInfo 单例指针的宿主地址（逗号分隔）；给了就跳过扫描")
	ptrMode      = flag.Bool("ptrmode", false, "只用 -ptr 指定的地址读，不扫描")
)

// piRefT 一个 PlayerInfo 候选的引用。
type piRefT struct {
	ptrHost uintptr // 单例指针所在处（宿主地址）
	ptrVal  uintptr // 单例对象地址（宿主地址）
	actor   uintptr // actor 对象地址
	tbl     uintptr // actor 的属性表地址
}

// cmdWatchPos 持续读坐标。
func cmdWatchPos(p *winmem.Process) {
	hr("持续读取玩家坐标（静态分析链路）")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[watch] %v\n", err)
		return
	}

	// ---- 阶段 0：已知地址直读（零扫描，绝不影响游戏）----
	if *knownPtrs != "" {
		var known []piRefT
		for _, s := range strings.Split(*knownPtrs, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			hv, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
			if err != nil {
				printf("[watch] 地址 %q 解析失败\n", s)
				continue
			}
			known = append(known, piRefT{uintptr(hv), uintptr(hv), 0, 0})
		}
		printf("[watch] 已知地址模式：%d 个指针（零扫描）\n", len(known))
		watchKnown(p, d, known)
		return
	}

	// ---- 阶段 1：定位 PlayerInfo 单例 ----
	var found []piRefT

	// 候选区：模块区（.data/.bss）+ UserHeap（单例实例本身在堆上时，
	// 其"谁指向它"的指针可能在堆里的管理器里）
	var regions []winmem.Region
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.Readable() && r.Size >= 0x1000 {
			regions = append(regions, r)
		}
		return false
	})
	if posScanAll {
		regions = p.Committed(64 << 10)
	}
	printf("[watch] 候选区 %d 个\n", len(regions))

	type cand struct{ host, ptr uintptr }
	var cands []cand
	seen := map[uintptr]bool{}
	var mu sync.Mutex

	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		for off := 0; off+8 <= len(buf); off += 8 {
			v := binary.LittleEndian.Uint64(buf[off:])
			if v == 0 {
				continue
			}
			for _, gv := range []uintptr{uintptr(v), uintptr(v) + d.ArenaLow} {
				if gv < d.HeapLo() || gv >= d.HeapHi() || gv%0x200 == 0 {
					continue
				}
				h := addr + uintptr(off)
				mu.Lock()
				if !seen[h] {
					seen[h] = true
					cands = append(cands, cand{h, gv})
				}
				mu.Unlock()
			}
		}
	})
	printf("[watch] 候选指针 %d 个，开始结构自证…\n", len(cands))

	for _, c := range cands {
		actorRaw, ok := readPtrAt(p, c.ptr, piActorOff)
		if !ok || actorRaw == 0 {
			continue
		}
		actor, ok := heapAddr(d, actorRaw)
		if !ok {
			continue
		}
		tblRaw, ok := readPtrAt(p, actor, actorTableOff)
		if !ok || tblRaw == 0 {
			continue
		}
		tbl, ok := heapAddr(d, tblRaw)
		if !ok {
			continue
		}
		// 遍历属性表，找类型匹配且值像坐标的项
		for ai := 0; ai < 16; ai++ {
			item := tbl + uintptr(ai)*attrStride
			typ, ok := readU32(p, item, attrTypeOff)
			if !ok {
				break
			}
			if uint32(typ) != uint32(posAttrType) {
				continue
			}
			valsRaw, ok := readPtrAt(p, item, attrValsOff)
			if !ok || valsRaw == 0 {
				continue
			}
			vals, ok := heapAddr(d, valsRaw)
			if !ok {
				continue
			}
			if _, ok := readVec3(p, vals+valFloatOff); !ok {
				continue
			}
			// 去重：同一个 actor 只留一条
			dup := false
			for _, ff := range found {
				if ff.actor == actor {
					dup = true
					break
				}
			}
			if dup {
				break
			}
			found = append(found, piRefT{c.host, c.ptr, actor, tbl})
			printf("[watch] ★ PlayerInfo 候选：单例指针 host=0x%X gva=0x%X  actor=0x%X  属性表=0x%X\n",
				c.host, c.ptr-d.ArenaLow, actor, tbl)
			break
		}
		if len(found) >= 4 {
			break
		}
	}
	if len(found) == 0 {
		printf("[watch] 没找到 PlayerInfo（游戏可能没进主菜单）\n")
		return
	}

	// ---- 阶段 2：持续读 ----
	// 输出同时写文件（管道缓冲会吃掉实时输出）
	outPath := filepath.Join("report", fmt.Sprintf("watchpos-%s.txt", time.Now().Format("20060102-150405")))
	of, _ := os.Create(outPath)
	if of != nil {
		defer of.Close()
	}
	emit := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		if of != nil {
			of.WriteString(line + "\n")
			of.Sync()
		}
	}
	emit("\n[watch] 开始持续读取 → %s（Ctrl+C 退出）", outPath)
	emit("%-10s %-18s %s", "时刻", "候选指针 host", "坐标 (X, alt, Z)")
	tk := time.Now()
	for {
		for _, f := range found {
			actorRaw, _ := readPtrAt(p, f.ptrVal, piActorOff)
			if actorRaw == 0 {
				continue
			}
			actor, ok := heapAddr(d, actorRaw)
			if !ok {
				continue
			}
			tblRaw, _ := readPtrAt(p, actor, actorTableOff)
			if tblRaw == 0 {
				continue
			}
			tbl, ok := heapAddr(d, tblRaw)
			if !ok {
				continue
			}
			item := tbl + uintptr(posAttrIndex)*attrStride
			valsRaw, _ := readPtrAt(p, item, attrValsOff)
			if valsRaw == 0 {
				continue
			}
			vals, ok := heapAddr(d, valsRaw)
			if !ok {
				continue
			}
			pos, ok := readVec3(p, vals+valFloatOff)
			if !ok {
				continue
			}
			emit("%-10s 0x%08X  项%d (%.2f, %.2f, %.2f) %s",
				tk.Format("15:04:05"), f.ptrHost, posAttrIndex, pos[0], pos[1], pos[2],
				coordMark(pos))
			// 打印该 actor 属性表的所有项 —— 用于确认哪个索引才是坐标
			if verboseAttrs {
				for ai := 0; ai < 16; ai++ {
					it := tbl + uintptr(ai)*attrStride
					tp, ok2 := readU32(p, it, attrTypeOff)
					if !ok2 {
						break
					}
					vpRaw, _ := readPtrAt(p, it, attrValsOff)
					if vpRaw == 0 {
						continue
					}
					vp, ok2 := heapAddr(d, vpRaw)
					if !ok2 {
						continue
					}
					v3, ok2 := readVec3(p, vp+valFloatOff)
					if !ok2 {
						continue
					}
					emit("             项%-2d 类型=%-3d (%.2f, %.2f, %.2f)%s",
						ai, tp, v3[0], v3[1], v3[2], coordMark(v3))
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// heapAddr 把指针值（可能是 guest VA，也可能是宿主地址）校正成宿主地址。
// 指针在 Eden 内存里存成 guest VA（0x21xx…）时必须加 arenaLow —— 漏了会读错地方。
// watchKnown 用已知地址持续读坐标（零扫描，每 2 秒只读几个字节）。
func watchKnown(p *winmem.Process, d winmem.Dram, known []piRefT) {
	outPath := filepath.Join("report", fmt.Sprintf("watchknown-%s.txt", time.Now().Format("20060102-150405")))
	of, _ := os.Create(outPath)
	if of != nil {
		defer of.Close()
	}
	emit := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		if of != nil {
			of.WriteString(line + "\n")
			of.Sync()
		}
	}
	emit("[watch] 持续读 → %s", outPath)
	emit("%-10s %-18s %-14s %s", "时刻", "指针 host", "actor", "坐标 (X, alt, Z)")
	tk := time.Now()
	for {
		for _, k := range known {
			actorRaw, _ := readPtrAt(p, k.ptrVal, piActorOff)
			if actorRaw == 0 {
				emit("%-10s 0x%08X  <actor 为空，可能已重载>", tk.Format("15:04:05"), k.ptrHost)
				continue
			}
			actor, ok := heapAddr(d, actorRaw)
			if !ok {
				continue
			}
			tblRaw, _ := readPtrAt(p, actor, actorTableOff)
			if tblRaw == 0 {
				continue
			}
			tbl, ok := heapAddr(d, tblRaw)
			if !ok {
				continue
			}
			item := tbl + uintptr(posAttrIndex)*attrStride
			valsRaw, _ := readPtrAt(p, item, attrValsOff)
			if valsRaw == 0 {
				continue
			}
			vals, ok := heapAddr(d, valsRaw)
			if !ok {
				continue
			}
			pos, ok := readVec3(p, vals+valFloatOff)
			if !ok {
				continue
			}
			emit("%-10s 0x%08X  0x%08X  (%.2f, %.2f, %.2f) %s",
				tk.Format("15:04:05"), k.ptrHost, actor, pos[0], pos[1], pos[2], coordMark(pos))
		}
		time.Sleep(2 * time.Second)
	}
}

func heapAddr(d winmem.Dram, h uintptr) (uintptr, bool) {
	if h < d.HeapLo() || h >= d.HeapHi() {
		h += d.ArenaLow
	}
	if h >= d.HeapLo() && h < d.HeapHi() {
		return h, true
	}
	return 0, false
}

func coordMark(v [3]float32) string {
	if v[0] == 0 && v[1] == 0 && v[2] == 0 {
		return "  (全零 → 游戏未进主菜单?)"
	}
	if math.Abs(float64(v[0])) > 8000 || math.Abs(float64(v[2])) > 8000 {
		return "  (越界)"
	}
	return "  ✓"
}

// readTO = 带超时的读（Eden 忙时不会永久挂住）
const readTO = 800 * time.Millisecond

func readVec3(p *winmem.Process, addr uintptr) ([3]float32, bool) {
	buf, err := p.ReadTimeout(addr, 12, readTO)
	if err != nil || len(buf) < 12 {
		return [3]float32{}, false
	}
	var v [3]float32
	for i := 0; i < 3; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return v, true
}

func readPtrAt(p *winmem.Process, base uintptr, off uintptr) (uintptr, bool) {
	buf, err := p.ReadTimeout(base+off, 8, readTO)
	if err != nil || len(buf) < 8 {
		return 0, false
	}
	v := binary.LittleEndian.Uint64(buf)
	return uintptr(v), v != 0
}

func readU32(p *winmem.Process, base uintptr, off uintptr) (uint32, bool) {
	buf, err := p.ReadTimeout(base+off, 4, readTO)
	if err != nil || len(buf) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(buf), true
}

var _ = os.WriteFile
var _ = filepath.Join
var _ = sort.Ints
