// probeaf2.go：M3' 逆向第十二步——修正 host 映射基址，读 guest 指针指向的内容。
// 对象内指针是 guest 虚拟地址：host = guest + 0x016B00000000（推测映射基址）。
// 用法：live-npcs.exe --probeaf2

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF2(h uintptr) {
	// 候选映射基址：0x016B00000000、0x016A00000000、0x020C00000000、0x020D00000000
	bases := []uintptr{0x016B00000000, 0x016A00000000, 0x020C00000000, 0x020D00000000, 0x016B00000000 - 0x23600000}
	targets := []uintptr{0x0AF73AF0, 0x0AF742C8, 0x0AF75140, 0x3919BCA4, 0x39388EDC}

	// 先找字符串映射：验证 self 指针 → 该 host 地址应存同名 "Animal_Deer_A"
	strHost := uintptr(0x016B15B9BCA4) // Animal_Deer_A 字符串 host 地址
	self := uintptr(0x3919BCA4)        // 对象内 self 指针（guest）
	fmt.Printf("verify: self=0x%X -> host str @0x%012X\n", self, strHost)
	for _, b := range bases {
		addr := b + self
		buf := readMem(h, addr, 0x40)
		if buf == nil {
			continue
		}
		ascii := ""
		for i := 0; i < 0x40 && buf[i] >= 32 && buf[i] <= 126; i++ {
			ascii += string(buf[i])
		}
		fmt.Printf("  base=0x%012X -> 0x%012X: %q\n", b, addr, ascii)
	}

	// dump guest 0x0AF73AF0 在正确基址下的结构
	fmt.Println("\n-- dump guest ptrs (base=0x016ADCA00000) --")
	base := uintptr(0x016ADCA00000)
	for _, t := range targets {
		addr := base + t
		buf := readMem(h, addr, 0x100)
		if buf == nil {
			fmt.Printf("  0x%012X (guest 0x%X): UNREADABLE\n", addr, t)
			continue
		}
		fmt.Printf("  0x%012X (guest 0x%X):\n", addr, t)
		for off := 0; off < 0x100; off += 0x10 {
			fmt.Printf("    +0x%03X  ", off)
			for i := off; i < off+0x10; i++ {
				fmt.Printf("%02X ", buf[i])
			}
			fmt.Print(" |")
			for i := off; i < off+0x10; i++ {
				if buf[i] >= 32 && buf[i] <= 126 {
					fmt.Printf("%c", buf[i])
				} else {
					fmt.Print(".")
				}
			}
			fmt.Println("|")
		}
	}

	// 从名字对象结构读完整字段：字符串 host 地址 -0x40 处
	fmt.Println("\n-- full name-object header (Animal_Deer_A @0x016B15B9BCA4) --")
	buf := readMem(h, strHost-0x40, 0x40+0x40)
	if buf != nil {
		for off := 0; off < 0x40; off += 8 {
			v := *(*uint64)(unsafe.Pointer(&buf[off]))
			fmt.Printf("  -0x%02X: 0x%016X\n", 0x40-off, v)
		}
	}

	// 验证：guest->host 映射自洽——名字字符串 guest 地址 + base = host 地址
	fmt.Println("\n-- mapping sanity --")
	base2 := uintptr(0x016ADCA00000)
	guestStr := uintptr(0x3919BCA4)
	fmt.Printf("guest 0x%X + base = 0x%012X (expect 0x%012X)\n", guestStr, base2+guestStr, strHost)
	fmt.Printf("guest 0x%X + base = 0x%012X (0x0AF73AF0 global)\n", 0x0AF73AF0, base2+0x0AF73AF0)
	fmt.Printf("guest 0x%X + base = 0x%012X (0x0AF742C8 animal)\n", 0x0AF742C8, base2+0x0AF742C8)
	fmt.Printf("guest 0x%X + base = 0x%012X (0x0AF75140 npc)\n", 0x0AF75140, base2+0x0AF75140)
}
