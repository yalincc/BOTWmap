// vtscan.go：M3' 逆向第五步——全内存扫描 vtable 指针（值 ∈ 0x7100000000..0x7110000000）。
// BOTW 所有 C++ 对象头部第一个 8B = vtable 指针（指向 guest 代码地址，Ryujinx 中不可读但值存在）。
// 找到对象后：检查对象内是否含玩家坐标 → 定位玩家 actor 对象 → 读名字/hash → ActorCreator 链表。
// 用法：live-npcs.exe --vtscan [--vt-lo=N --vt-hi=N] [--vt-top=N]

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

func runVtScan(h uintptr) {
	lo := uintptr(0x7100000000)
	hi := uintptr(0x7110000000)
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--vt-lo=") {
			if n, err := strconv.ParseUint(a[len("--vt-lo="):], 0, 64); err == nil {
				lo = uintptr(n)
			}
		}
		if strings.HasPrefix(a, "--vt-hi=") {
			if n, err := strconv.ParseUint(a[len("--vt-hi="):], 0, 64); err == nil {
				hi = uintptr(n)
			}
		}
	}
	anchors := readSaveAnchors()
	var playerPos [3]float32
	// 用实时玩家坐标（而不是存档锚点——玩家在移动）
	groups, _, _ := findEntitiesNear(h, [][3]float32{anchors[0].Pos}, 400)
	if pl := findPlayerGroup(groups, anchors); pl != nil {
		playerPos = [3]float32{pl.X, pl.Y, pl.Z}
		fmt.Printf("live player pos=(%.1f, %.1f, %.1f) @0x%012X\n", pl.X, pl.Y, pl.Z, pl.RepAddr)
	} else if len(anchors) > 0 {
		playerPos = anchors[0].Pos
		fmt.Printf("using save anchor pos=(%.1f, %.1f, %.1f)\n", playerPos[0], playerPos[1], playerPos[2])
	}
	blocks := guestBlocks(h, minBlockMB)
	fmt.Printf("scanning %d blocks for vtable ptr in 0x%X..0x%X...\n", len(blocks), lo, hi)

	type vhit struct {
		at  uintptr
		val uint64
	}
	var vhits []vhit
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(8 << 20)
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if buf != nil {
				for i := 0; i+8 <= len(buf); i += 8 {
					v := *(*uint64)(unsafe.Pointer(&buf[i]))
					if v >= uint64(lo) && v < uint64(hi) {
						vhits = append(vhits, vhit{b.Base + off + uintptr(i), v})
					}
				}
			}
			off += n
		}
	}
	fmt.Printf("vtable-like ptrs: %d\n", len(vhits))

	// 对每个 vtable 命中：检查其对象（±0x2000）内是否含玩家坐标
	fmt.Println("\n-- candidates whose object contains player coords --")
	fmt.Printf("player pos=(%.1f, %.1f, %.1f)\n", playerPos[0], playerPos[1], playerPos[2])
	nPlayer := 0
	for _, v := range vhits {
		// 对象基址 = vtable 位置（对象头第一字段）。检查 [at-0x100, at+0x2000]
		lo := uintptr(0)
		if v.at > 0x100 {
			lo = v.at - 0x100
		}
		hi := v.at + 0x2000
		buf := readMem(h, lo, int(hi-lo))
		if buf == nil {
			continue
		}
		if hasXYZ(buf, playerPos) {
			// 找对象内的名字字符串候选
			name := findAscii(buf, 0x40)
			fmt.Printf("  vtable @0x%012X -> 0x%012X  contains player pos  name_cand=%q\n", v.at, v.val, name)
			nPlayer++
		}
	}
	if nPlayer == 0 {
		fmt.Println("  (none found)")
	} else {
		fmt.Printf("  total: %d\n", nPlayer)
	}

	// 聚簇：对象 = vtable 位置；相邻 vtable 属于同一对象池
	// 按 0x1000 粒度统计
	buckets := map[uintptr]int{}
	for _, v := range vhits {
		buckets[v.at&^0xFFF]++
	}
	type bk struct {
		base  uintptr
		count int
	}
	var bks []bk
	for b, c := range buckets {
		bks = append(bks, bk{b, c})
	}
	for i := 0; i < len(bks); i++ {
		for j := i + 1; j < len(bks); j++ {
			if bks[j].count > bks[i].count {
				bks[i], bks[j] = bks[j], bks[i]
			}
		}
	}
	fmt.Println("\n-- top clusters (0x1000 buckets) --")
	shown := 0
	for _, b := range bks {
		if b.count < 4 {
			continue
		}
		// 检查簇内是否含玩家坐标附近值
		hasPlayer := clusterHasPos(h, b.base, playerPos)
		fmt.Printf("  0x%012X  vtable_count=%d  player_pos=%v\n", b.base, b.count, hasPlayer)
		shown++
		if shown >= 20 {
			break
		}
	}
	fmt.Printf("\n(total clusters>=4: %d)\n", len(bks))

	// dump 前 5 个簇的对象头内容（字符串+float+指针特征）
	fmt.Println("\n-- dump top cluster contents (first 5) --")
	dumped := 0
	for _, b := range bks {
		if b.count < 4 {
			continue
		}
		if dumped >= 5 {
			break
		}
		dumpCluster(h, b.base)
		dumped++
	}
}

// dumpCluster 打印对象头 0x400 字节：ASCII 字符串 + 有意义的 float
func dumpCluster(h uintptr, base uintptr) {
	buf := readMem(h, base, 0x400)
	if buf == nil {
		return
	}
	fmt.Printf("  -- cluster @0x%012X --\n", base)
	// ASCII 检测（4 字节以上可打印）
	for off := 0; off < len(buf)-4; off++ {
		if buf[off] >= 32 && buf[off] <= 126 {
			// 连续可打印
			end := off
			for end < len(buf) && buf[end] >= 32 && buf[end] <= 126 {
				end++
			}
			if end-off >= 4 {
				fmt.Printf("    +0x%03X  str: %q\n", off, string(buf[off:end]))
				off = end - 1
			}
		}
	}
	// 有意义的 float（|v|<20000 且非 0）
	for off := 0; off+4 <= len(buf); off += 4 {
		f := float32le(buf[off : off+4])
		if f != 0 && f > -20000 && f < 20000 && f == float32(int32(f)) && int32(f) != 0 {
			fmt.Printf("    +0x%03X  float=%.0f\n", off, f)
		}
	}
}

// clusterHasPos 在簇基址起 0x1000 内找坐标 float（|v-player|<2 的 X/Y/Z 三元组）
func clusterHasPos(h uintptr, base uintptr, player [3]float32) bool {
	buf := readMem(h, base, 0x1000)
	if buf == nil {
		return false
	}
	return hasXYZ(buf, player)
}

// hasXYZ 检查 buf 中是否同时出现 X 与 Z（±2m 容差），Y 较弱可验证
func hasXYZ(buf []byte, p [3]float32) bool {
	find := func(target float32) bool {
		for i := 0; i+4 <= len(buf); i += 4 {
			f := float32le(buf[i : i+4])
			d := f - target
			if d < 0 {
				d = -d
			}
			if d < 2 {
				return true
			}
		}
		return false
	}
	return find(p[0]) && find(p[2])
}

// findAscii 在 buf 中找最长可打印串（最短 minLen），返回截断文本
func findAscii(buf []byte, minLen int) string {
	best := ""
	cur := ""
	for _, c := range buf {
		if c >= 32 && c <= 126 {
			cur += string(c)
		} else {
			if len(cur) >= minLen && len(cur) > len(best) {
				best = cur
			}
			cur = ""
		}
	}
	if len(cur) >= minLen && len(cur) > len(best) {
		best = cur
	}
	if len(best) > 60 {
		best = best[:60]
	}
	return best
}
