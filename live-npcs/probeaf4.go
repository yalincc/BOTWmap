// probeaf4.go：M3' 逆向第十四步——dump 名字对象数组条目 + 类型指针内容。
// 0x3919BE50/0x3919BEC0/0x3919BF30/0x3919BFA0 间隔 0x70 → 疑似 actor 条目数组。
// 用法：live-npcs.exe --probeaf4

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF4(h uintptr) {
	// 1) dump 名字对象 0x3919BCA4 上下文 ±0x300（看看是不是连续数组）
	fmt.Println("-- context around 0x3919BCA4 (guest) ±0x300, u32 view --")
	host := g2h(0x3919BCA4)
	buf := readMem(h, host-0x40, 0x300+0x40)
	if buf == nil {
		fmt.Println("unreadable")
		return
	}
	for off := 0; off < 0x300+0x40; off += 0x10 {
		var hex, ascii string
		for i := off; i < off+0x10; i++ {
			hex += fmt.Sprintf("%02X ", buf[i])
			if buf[i] >= 32 && buf[i] <= 126 {
				ascii += string(buf[i])
			} else {
				ascii += "."
			}
		}
		fmt.Printf("  %+05X: %s |%s|\n", off-0x40, hex, ascii)
	}

	// 2) 追踪 0x3919BE50 等目标的内容（可能是下一个名字对象）
	fmt.Println("\n-- follow array entries --")
	for _, g := range []uintptr{0x3919BE50, 0x3919BEC0, 0x3919BF30, 0x3919BFA0} {
		th := g2h(g)
		tb := readMem(h, th, 0x70)
		if tb == nil {
			fmt.Printf("  guest 0x%X -> host 0x%012X UNREADABLE\n", g, th)
			continue
		}
		ascii := ""
		for i := 0; i < 0x70 && tb[i] >= 32 && tb[i] <= 126; i++ {
			ascii += string(tb[i])
		}
		u0 := *(*uint32)(unsafe.Pointer(&tb[0]))
		u1 := *(*uint32)(unsafe.Pointer(&tb[4]))
		fmt.Printf("  guest 0x%X -> host 0x%012X: u32[0]=0x%08X u32[1]=0x%08X ascii=%q\n", g, th, u0, u1, ascii)
	}

	// 3) dump 类型指针 guest 0x0AF742C8 (动物) / 0x0AF75140 (NPC) 附近更大范围
	fmt.Println("\n-- dump type ptr region 0x0AF73AF0 ±0x2000 (guest) --")
	for _, g := range []uintptr{0x0AF73AF0, 0x0AF742C8, 0x0AF75140} {
		th := g2h(g)
		fmt.Printf("  guest 0x%X -> host 0x%012X\n", g, th)
	}
	// 扫 0x0AF72000..0x0AF78000 找非零区
	fmt.Println("\n-- scan 0x0AF72000..0x0AF7A000 for nonzero regions --")
	start := g2h(0x0AF72000)
	end := g2h(0x0AF7A000)
	for a := start; a < end; a += 0x1000 {
		b := readMem(h, a, 0x1000)
		if b == nil {
			continue
		}
		nonzero := false
		for i := 0; i < 0x1000; i++ {
			if b[i] != 0 {
				nonzero = true
				break
			}
		}
		if nonzero {
			// 找第一个非零偏移
			fi := 0
			for i := 0; i < 0x1000; i++ {
				if b[i] != 0 {
					fi = i
					break
				}
			}
			fmt.Printf("  0x%012X: nonzero at +0x%X\n", a, fi)
		}
	}
}
