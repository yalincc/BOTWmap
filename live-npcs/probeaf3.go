// probeaf3.go：M3' 逆向第十三步——跟踪名字对象内的指针链。
// 名字对象 @guest 0x3919BCA4 (Animal_Deer_A) +0x80 起有指针序列 0x3919BE50/0x3919BEC0/0x3919BF30...
// 这些可能是相邻 actor 名字对象（链表/数组）。dump 每个指针指向的内容。
// 用法：live-npcs.exe --probeaf3

package main

import (
	"fmt"
	"unsafe"
)

const gbase = uintptr(0x016ADCA00000)

func g2h(g uintptr) uintptr { return gbase + g }

func runProbeAF3(h uintptr) {
	// 1) 名字对象 +0x080 起的指针序列
	fmt.Println("-- name-obj @0x3919BCA4 +0x40..0xC0 raw (u32 fields) --")
	host := g2h(0x3919BCA4)
	buf := readMem(h, host, 0xC0+0x40)
	if buf == nil {
		fmt.Println("unreadable")
		return
	}
	for off := 0x40; off < 0xC0; off += 4 {
		v := *(*uint32)(unsafe.Pointer(&buf[off]))
		fmt.Printf("  +0x%03X: 0x%08X\n", off, v)
	}

	// 2) 跟踪 +0x80 起的 guest 指针（若值 <0x40000000 视为 guest addr）
	fmt.Println("\n-- following ptr chain from +0x80 --")
	for off := 0x80; off < 0xC0; off += 8 {
		v := *(*uint64)(unsafe.Pointer(&buf[off]))
		lo := uintptr(v & 0xFFFFFFFF)
		if lo > 0x100000 && lo < 0x10000000 {
			th := g2h(lo)
			tb := readMem(h, th, 0x60)
			if tb == nil {
				fmt.Printf("  +0x%03X: guest 0x%X -> host 0x%012X UNREADABLE\n", off, lo, th)
				continue
			}
			ascii := ""
			for i := 0; i < 0x40 && tb[i] >= 32 && tb[i] <= 126; i++ {
				ascii += string(tb[i])
			}
			// 前 3 个 u32
			u0 := *(*uint32)(unsafe.Pointer(&tb[0]))
			u1 := *(*uint32)(unsafe.Pointer(&tb[4]))
			u2 := *(*uint32)(unsafe.Pointer(&tb[8]))
			fmt.Printf("  +0x%03X: guest 0x%X -> host 0x%012X u32=%X/%X/%X ascii=%q\n",
				off, lo, th, u0, u1, u2, ascii)
		} else {
			fmt.Printf("  +0x%03X: 0x%016X (not guest-like)\n", off, v)
		}
	}

	// 3) 名字对象头部 -0x40 起再确认（u32 视角）
	fmt.Println("\n-- name-obj header -0x40..0x00 (u32 fields) --")
	hbuf := readMem(h, host-0x40, 0x40)
	if hbuf != nil {
		for off := 0; off < 0x40; off += 4 {
			v := *(*uint32)(unsafe.Pointer(&hbuf[off]))
			fmt.Printf("  -0x%02X: 0x%08X\n", 0x40-off, v)
		}
	}
}
