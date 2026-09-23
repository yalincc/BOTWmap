// probe010.go：M3' 逆向第九步——探测 0x0100... 区域（objscan 发现玩家对象内有指针指向它）。
// 用法：live-npcs.exe --probe010

package main

import (
	"fmt"
	"unsafe"
)

func runProbe010(h uintptr) {
	// 玩家对象 0x016B09AC9B80 内 0x0AA0 起有指向 0x00000100... 的指针序列
	// 先确认该区域可读性 + 内容
	base := uintptr(0x000001000191DFC7) &^ 0xFFF
	fmt.Printf("probing 0x0000010000000000 region...\n")
	bases := []uintptr{
		0x0000010000000000, 0x0000010001000000, 0x0000010001900000,
		base, base + 0x1000, base + 0x10000, 0x0000010002000000,
		0x0000010004000000, 0x0000010010000000,
	}
	for _, b := range bases {
		buf := readMem(h, b, 64)
		if buf == nil {
			fmt.Printf("0x%012X: UNREADABLE\n", b)
			continue
		}
		hex := ""
		ascii := ""
		for i := 0; i < 32; i++ {
			hex += fmt.Sprintf("%02X ", buf[i])
			if buf[i] >= 32 && buf[i] <= 126 {
				ascii += string(buf[i])
			} else {
				ascii += "."
			}
		}
		fmt.Printf("0x%012X: %s |%s|\n", b, hex, ascii)
	}

	// 从玩家对象里完整读出指针序列（objscan 显示 0x0AA0 起）
	fmt.Println("\n-- ptrs inside player obj @0x016B09AC9B80 +0x0A90.. --")
	pbuf := readMem(h, 0x016B09AC9B80+0x0A80, 0x2000)
	if pbuf == nil {
		fmt.Println("  obj unreadable")
		return
	}
	for off := 0; off+8 <= len(pbuf); off += 8 {
		v := *(*uint64)(unsafe.Pointer(&pbuf[off]))
		if v >= 0x0000010000000000 && v < 0x0000020000000000 {
			fmt.Printf("  +0x%04X  -> 0x%012X\n", 0x0A80+off, v)
		}
	}
}
