// probeaf.go：M3' 逆向第十一步——探测 0x0AF7... 区（名字字符串内指针指向的区域）。
// 0x0AF742C8(动物) / 0x0AF75140(NPC) / 0x0AF70DB8(昆虫) / 0x0AF73AF0(全局) —— 疑似 ActorInfo 表。
// 用法：live-npcs.exe --probeaf

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF(h uintptr) {
	// 1) 检查 0x0AF7... 区可读性
	for _, a := range []uintptr{0x0AF7000000, 0x0AF73AF0, 0x0AF742C8, 0x0AF75140, 0x0AF70DB8, 0x0AF74000, 0x0AF8000000, 0x0AF6000000, 0x0B00000000, 0x0AF740C8} {
		buf := readMem(h, a, 0x40)
		if buf == nil {
			fmt.Printf("0x%012X: UNREADABLE\n", a)
			continue
		}
		fmt.Printf("0x%012X: ", a)
		for i := 0; i < 32; i++ {
			fmt.Printf("%02X ", buf[i])
		}
		fmt.Println()
	}

	// 2) 从名字字符串上下文读完整结构：名字字符串前 0x30 字节 = 4 个字段
	fmt.Println("\n-- name-string header fields (from npcscope hits) --")
	type nameCase struct {
		strAddr  uintptr
		label    string
	}
	cases := []nameCase{
		{0x016B15B9BCA4, "Animal_Deer_A"},
		{0x016B15D88EDC, "Npc_HiddenKorokGround"},
		{0x016B15C063F4, "Npc_HatenoVillage003"},
		{0x016B159ED904, "Npc_Road_008"},
		{0x016B15C043A4, "Animal_Insect_G"},
		{0x016B1509C87F, "Npc_RevivalFairy"},
		{0x016B15B3130C, "Animal_Sheep_A"},
	}
	for _, c := range cases {
		buf := readMem(h, c.strAddr-0x30, 0x30+64)
		if buf == nil {
			fmt.Printf("  %s @0x%012X: unreadable\n", c.label, c.strAddr)
			continue
		}
		fmt.Printf("  %s @0x%012X:\n", c.label, c.strAddr)
		for off := 0; off < 0x30; off += 8 {
			v := *(*uint64)(unsafe.Pointer(&buf[off]))
			fmt.Printf("    -0x%02X: 0x%016X\n", 0x30-off, v)
		}
	}

	// 3) dump 0x0AF73AF0 和 0x0AF742C8 的 0x100 结构
	fmt.Println("\n-- dump 0x0AF73AF0 (global same) 0x100 --")
	for _, a := range []uintptr{0x0AF73AF0, 0x0AF742C8, 0x0AF75140, 0x0AF70DB8} {
		buf := readMem(h, a, 0x100)
		if buf == nil {
			fmt.Printf("  0x%012X unreadable\n", a)
			continue
		}
		fmt.Printf("  0x%012X:\n", a)
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
}
