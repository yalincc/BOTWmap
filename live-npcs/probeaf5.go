// probeaf5.go：M3' 逆向第十五步——dump 0x0B11 区（名字对象的关联数据区）。
// 名字对象里 0x0B110EB8/0x0B1117E0/0x0B111838/0x0B110EE0 等大量指针指向 guest 0x0B11... 区。
// 用法：live-npcs.exe --probeaf5

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF5(h uintptr) {
	// 1) dump 0x0B110EB8 与 0x0B1117E0 的内容
	fmt.Println("-- dump guest 0x0B110EB8 / 0x0B1117E0 / 0x0B111838 --")
	for _, g := range []uintptr{0x0B110EB8, 0x0B1117E0, 0x0B111838, 0x0B1117E0 - 0x100, 0x0B110EE0} {
		th := g2h(g)
		buf := readMem(h, th, 0x100)
		if buf == nil {
			fmt.Printf("  guest 0x%X -> host 0x%012X UNREADABLE\n", g, th)
			continue
		}
		fmt.Printf("  guest 0x%X -> host 0x%012X:\n", g, th)
		for off := 0; off < 0x100; off += 0x10 {
			var hex, ascii string
			for i := off; i < off+0x10; i++ {
				hex += fmt.Sprintf("%02X ", buf[i])
				if buf[i] >= 32 && buf[i] <= 126 {
					ascii += string(buf[i])
				} else {
					ascii += "."
				}
			}
			fmt.Printf("    +0x%03X  %s |%s|\n", off, hex, ascii)
		}
	}

	// 2) 名字对象 +0x80 起的指针全部列出
	fmt.Println("\n-- all guest ptrs in name-obj @0x3919BCA4 (0x40..0x300) --")
	host := g2h(0x3919BCA4)
	buf := readMem(h, host-0x40, 0x300+0x40)
	if buf == nil {
		fmt.Println("unreadable")
		return
	}
	for off := 0; off < 0x300+0x40; off += 4 {
		v := *(*uint32)(unsafe.Pointer(&buf[off]))
		// guest 地址特征：0x0A..0x0F 或 0x35..0x39 区间，且非小整数
		if v > 0x01000000 && v < 0x20000000 && (v>>28) != 0x0 {
			fmt.Printf("  +0x%03X: 0x%08X\n", off-0x40, v)
		}
	}
}
