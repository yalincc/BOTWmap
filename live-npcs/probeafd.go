// probeafd.go：M3' 逆向第二十三步——对比 dump 实例引用点 vs 组件引用点。
// Village003 实例 @0x016B16D488A8 有坐标(-0x30=(3357,225,2165))；Village002 @0x016B16D472B8 待验。
// 目的：找出"实例引用点"与"组件引用点"的结构差异 → 精确定位所有 Npc 实例。
// 用法：live-npcs.exe --probeafd

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAFD(h uintptr) {
	// 已知实例: Village003 @0x016B16D488A8；候选: Village002 @0x016B16D472B8 / @0x016B16D4BD28
	for _, t := range []uintptr{
		0x016B16D488A8, // Village003 实例(已知含坐标)
		0x016B16D472B8, // Village002?
		0x016B16D4BD28, // Village002?
		0x016B16D81DC0, // Village002?
		0x016B174A7228, // Village018? (之前 +0x5C 读到 (3429,236,2093))
		0x016B17D60C58, // Village018? (距离玩家近)
		0x016B17DDA1B0, // Village001?
		0x016B16159070, // Village004?
	} {
		buf := readMem(h, t-0x50, 0x50+0xC0)
		if buf == nil {
			fmt.Printf("== 0x%012X UNREADABLE ==\n", t)
			continue
		}
		fmt.Printf("== ref @0x%012X ==\n", t)
		for off := 0; off < 0x50+0xC0; off += 0x10 {
			var hex, ascii string
			for i := off; i < off+0x10; i++ {
				hex += fmt.Sprintf("%02X ", buf[i])
				if buf[i] >= 32 && buf[i] <= 126 {
					ascii += string(buf[i])
				} else {
					ascii += "."
				}
			}
			// 附加 float 解读（每 4B）
			f1 := *(*float32)(unsafe.Pointer(&buf[off]))
			f2 := *(*float32)(unsafe.Pointer(&buf[off+4]))
			f3 := *(*float32)(unsafe.Pointer(&buf[off+8]))
			f4 := *(*float32)(unsafe.Pointer(&buf[off+12]))
			fmt.Printf("  %+04X: %s |%s| f=(%.2f %.2f %.2f %.2f)\n", off-0x50, hex, ascii, f1, f2, f3, f4)
		}
	}
}
