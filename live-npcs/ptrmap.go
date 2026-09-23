// ptrmap.go：M3' 逆向辅助——玩家槽周围 8B 指针指向 JIT 代码段（vtable 候选）。
// Ryujinx ASLR：NSO 代码被 JIT 到 host 内存（0x036B00000000..0x037000000000，prot=0x20）。
// 目的：定位 actor 对象头（vtable 指针）→ 名字字段 → ActorCreator 链表。
// 用法：live-npcs.exe --ptrmap

package main

import (
	"fmt"
	"os"
	"strings"
	"unsafe"
)

const (
	jitLo = uintptr(0x036000000000) // JIT 代码段范围（保守扩大）
	jitHi = uintptr(0x037000000000)
)

func runPtrMap(h uintptr) {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--ptr-win=") {
			// 保留参数解析（兼容）；扫描范围固定 0x400000 前向
		}
	}
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no anchor")
		return
	}
	groups, _, secs := findEntitiesNear(h, [][3]float32{anchors[0].Pos}, 400)
	player := findPlayerGroup(groups, anchors)
	if player == nil {
		fmt.Println("player not found")
		return
	}
	base := player.RepAddr
	fmt.Printf("player slot: 0x%012X pos=(%.1f,%.1f,%.1f) (%.1fs)\n", base, player.X, player.Y, player.Z, secs)

	// 扫描玩家槽往前 0x400000 找 vtable（指向 JIT 代码段）
	lo := uintptr(0)
	if base > 0x400000 {
		lo = base - 0x400000
	}
	hi := base + 0x2000
	fmt.Printf("scanning 0x%012X..0x%012X for vtable ptr (JIT 0x%X..0x%X)...\n", lo, hi, jitLo, jitHi)

	type vhit struct {
		at  uintptr
		val uint64
	}
	var vhits []vhit
	addr := lo
	for addr < hi {
		n := 64 << 10
		if hi-addr < uintptr(n) {
			n = int(hi - addr)
		}
		buf := readMem(h, addr, n)
		if buf != nil {
			for i := 0; i+8 <= len(buf); i += 8 {
				v := *(*uint64)(unsafe.Pointer(&buf[i]))
				if v >= uint64(jitLo) && v < uint64(jitHi) {
					vhits = append(vhits, vhit{addr + uintptr(i), v})
				}
			}
		}
		addr += uintptr(n)
	}
	fmt.Printf("vtable-like ptrs (JIT): %d\n", len(vhits))
	shown := 0
	for _, v := range vhits {
		off := int(v.at - base)
		buf := readMem(h, v.at+8, 128)
		head := ""
		if buf != nil {
			for i := 0; i < 16 && i < len(buf); i++ {
				head += fmt.Sprintf("%02X ", buf[i])
			}
		}
		fmt.Printf("  vtable @0x%012X (off=%+d) -> 0x%012X  next:[%s]\n", v.at, off, v.val, head)
		shown++
		if shown >= 40 {
			fmt.Println("  ... (truncated)")
			break
		}
	}
}
