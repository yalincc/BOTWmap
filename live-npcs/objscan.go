// objscan.go：M3' 逆向第六步——从玩家 actor 对象出发，在它周围找相邻对象群。
// 目标：①精确找玩家坐标偏移；②找同 vtable / 相邻 vtable 的实例（其他 actor）；
// ③找名字字符串引用（指向 0x016A6B2... 名字区的指针）或内嵌名字。
// 用法：live-npcs.exe --objscan=0x016B09AC9B80 [--len=0x8000]

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

func runObjScan(h uintptr, addrArg string) {
	addr, err := strconv.ParseUint(strings.TrimPrefix(addrArg, "0x"), 16, 64)
	if err != nil {
		fmt.Printf("bad addr: %v\n", err)
		return
	}
	length := 0x8000
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--len=") {
			if n, err := strconv.ParseUint(a[len("--len="):], 0, 64); err == nil {
				length = int(n)
			}
		}
	}
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no anchor")
		return
	}
	playerPos := anchors[0].Pos

	lo := uintptr(addr) - 0x100
	hi := uintptr(addr) + uintptr(length)
	buf := readMem(h, lo, int(hi-lo))
	if buf == nil {
		fmt.Printf("unreadable 0x%012X..0x%012X\n", lo, hi)
		return
	}
	fmt.Printf("scanning 0x%012X..0x%012X (len=0x%X) around anchor object\n\n", lo, hi, len(buf))

	// 1) 玩家坐标精确偏移
	fmt.Println("-- player coords (X=%.1f Z=%.1f) occurrences --"[:0], "")
	fmt.Printf("-- player coords (X=%.1f Z=%.1f) --\n", playerPos[0], playerPos[2])
	for off := 0; off+12 <= len(buf); off += 4 {
		x := float32le(buf[off : off+4])
		z := float32le(buf[off+8 : off+12])
		if x > playerPos[0]-3 && x < playerPos[0]+3 && z > playerPos[2]-3 && z < playerPos[2]+3 {
			y := float32le(buf[off+4 : off+8])
			fmt.Printf("  +0x%04X  pos=(%.1f, %.1f, %.1f)\n", off, x, y, z)
		}
	}

	// 2) 所有 vtable 样指针（0x7100..0x7200 guest 代码）+ 其他指针分类
	fmt.Println("\n-- pointer-like 8B values (0x7100/0x7200 code, 0x016A name, guest heap) --")
	for off := 0; off+8 <= len(buf); off += 8 {
		v := *(*uint64)(unsafe.Pointer(&buf[off]))
		rel := ""
		if v >= 0x7100000000 && v < 0x7200000000 {
			rel = fmt.Sprintf(" CODE(0x%X)", v&0xFFFF)
		} else if v >= 0x016A00000000 && v < 0x016B00000000 {
			rel = " NAME?"
		} else if v >= 0x010000000000 && v < 0x020000000000 {
			rel = " GUEST"
		}
		if rel != "" {
			fmt.Printf("  +0x%04X  0x%016X %s\n", off, v, rel)
		}
	}

	// 3) ASCII 字符串
	fmt.Println("\n-- ascii strings --")
	for off := 0; off < len(buf)-4; off++ {
		if buf[off] >= 32 && buf[off] <= 126 {
			end := off
			for end < len(buf) && buf[end] >= 32 && buf[end] <= 126 {
				end++
			}
			if end-off >= 4 {
				fmt.Printf("  +0x%04X  %q\n", off, string(buf[off:end]))
				off = end - 1
			}
		}
	}

	// 4) 相邻 vtable 聚类（每 16B 内多次出现 CODE 指针 = 对象）
	fmt.Println("\n-- object cluster candidates (CODE ptr every <=0x100) --")
	lastCode := -1
	for off := 0; off+8 <= len(buf); off += 8 {
		v := *(*uint64)(unsafe.Pointer(&buf[off]))
		if v >= 0x7100000000 && v < 0x7200000000 {
			if lastCode >= 0 && off-lastCode <= 0x100 {
				fmt.Printf("  obj @+0x%04X (0x%012X) vtable=0x%016X\n", off, lo+uintptr(off), v)
			}
			lastCode = off
		} else {
			lastCode = -1
		}
	}
}
