// probeaf7.go：M3' 逆向第十七步——dump ActorInfo +0x80 指针链 & 找实例引用。
// Npc_HatenoVillage003 名字对象 +0x80 起指针：0x392065A0/0x39206610/0x39206680/0x392066F0（guest 0x39 区）。
// 用法：live-npcs.exe --probeaf7

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF7(h uintptr) {
	// 1) dump 0x392065A0 / 0x39206610 指向的内容
	fmt.Println("-- dump ActorInfo sub-ptrs (Npc_HatenoVillage003) --")
	for _, g := range []uintptr{0x392065A0, 0x39206610, 0x39206680, 0x392066F0} {
		th := g2h(g)
		buf := readMem(h, th, 0x80)
		if buf == nil {
			fmt.Printf("  guest 0x%X -> host 0x%012X UNREADABLE\n", g, th)
			continue
		}
		fmt.Printf("  guest 0x%X -> host 0x%012X:\n", g, th)
		for off := 0; off < 0x80; off += 0x10 {
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

	// 2) 全内存反查：谁引用名字字符串地址（host 0x016B15C063F4 / guest 0x392063F4）
	fmt.Println("\n-- reverse-lookup refs to Npc_HatenoVillage003 name str --")
	// guest 值 0x392063F4 作为 4B 引用；host 值 0x016B15C063F4 作为 8B 引用
	for _, v := range []uint64{0x392063F4, 0x016B15C063F4, 0x00000000392063F4} {
		refs := scanRefsTo(h, v)
		fmt.Printf("  value 0x%016X: %d refs\n", v, len(refs))
		for _, r := range refs {
			fmt.Printf("    @0x%012X\n", r)
		}
	}
}

// scanRefsTo 全 guest RAM 扫描 8B 值等于 target 的地址。
func scanRefsTo(h uintptr, target uint64) []uintptr {
	blocks := guestBlocks(h, minBlockMB)
	var out []uintptr
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(8 << 20)
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if buf != nil {
				for i := 0; i+8 <= len(buf); i += 4 {
					v := *(*uint64)(unsafe.Pointer(&buf[i]))
					if v == target {
						out = append(out, b.Base+off+uintptr(i))
					}
				}
			}
			off += n
		}
	}
	return out
}
