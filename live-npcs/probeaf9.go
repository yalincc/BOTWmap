// probeaf9.go：M3' 逆向第十九步——dump hash 引用点，验证是否为 actor 实例对象。
// hash 0x3445DE78 (Npc_HatenoVillage003) 引用 @0x016B16527D50/0x016B16527E08/0x016B16527ED8（连续三点）。
// 若为实例对象，附近应有：坐标 float + 名字指针 + 更多 hash 字段。
// 用法：live-npcs.exe --probeaf9

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF9(h uintptr) {
	targets := []uintptr{
		0x016B15C06440, // 名字对象自身 hash 字段
		0x016B16157B30,
		0x016B1615C5A0,
		0x016B161911F0,
		0x016B16527D50,
		0x016B16527E08,
		0x016B16527ED8,
		0x016B16D488A8,
		0x016B19CD2F00,
		0x016B19CD2FB0,
	}
	for _, t := range targets {
		buf := readMem(h, t-0x40, 0x40+0x80)
		if buf == nil {
			fmt.Printf("== 0x%012X UNREADABLE ==\n", t)
			continue
		}
		fmt.Printf("== hash ref @0x%012X ==\n", t)
		for off := 0; off < 0x40+0x80; off += 0x10 {
			var hex, ascii string
			for i := off; i < off+0x10; i++ {
				hex += fmt.Sprintf("%02X ", buf[i])
				if buf[i] >= 32 && buf[i] <= 126 {
					ascii += string(buf[i])
				} else {
					ascii += "."
				}
			}
			fmt.Printf("  %+04X: %s |%s|\n", off-0x40, hex, ascii)
		}
	}
}

var _ = unsafe.Pointer(nil)
