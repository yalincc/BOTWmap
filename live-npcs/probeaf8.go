// probeaf8.go：M3' 逆向第十八步——反查 ActorInfo hash 引用者。
// Npc_HatenoVillage003 hash=0x3445DE78，Animal_Deer_A hash=0x34477EC0。
// 实例对象可能存 hash（4B/8B），全内存反查 → 找到实例 → 挖坐标。
// 用法：live-npcs.exe --probeaf8

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF8(h uintptr) {
	hashes := []uint64{0x3445DE78, 0x34477EC0}
	for _, hv := range hashes {
		fmt.Printf("\n=== hash 0x%08X refs (8B & 4B) ===\n", hv)
		refs8 := scanRefsTo(h, hv)
		fmt.Printf("  8B refs: %d\n", len(refs8))
		for _, r := range refs8 {
			fmt.Printf("    @0x%012X\n", r)
		}
		refs4 := scanRefs4To(h, uint32(hv))
		fmt.Printf("  4B refs: %d\n", len(refs4))
		for _, r := range refs4 {
			fmt.Printf("    @0x%012X\n", r)
		}
	}
}

// scanRefs4To 全 guest RAM 扫描 4B 值等于 target 的地址（4B 对齐）。
func scanRefs4To(h uintptr, target uint32) []uintptr {
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
				for i := 0; i+4 <= len(buf); i += 4 {
					v := *(*uint32)(unsafe.Pointer(&buf[i]))
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
