// refslot.go：M3' 逆向第七步——从玩家坐标槽地址反查"谁引用它"。
// 玩家坐标槽（float 数据，RepAddr）是位置组件；全内存找 8B 值 == RepAddr（或 ±变体）
// 的指针 → 持有位置组件的父对象（actor 本体或其组件容器）。
// 用法：live-npcs.exe --refslot

package main

import (
	"fmt"
	"unsafe"
)

func runRefSlot(h uintptr) {
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

	// 目标集合：坐标槽本身 + 常见变体（组件结构可能引用结构头而非坐标）
	targets := map[uintptr]string{
		base:                "slot",
		base - 0x10:         "slot-0x10",
		base + 0x10:         "slot+0x10",
		base - 0x20:         "slot-0x20",
		base + 0x20:         "slot+0x20",
		base - 0x40:         "slot-0x40",
		base + 0x40:         "slot+0x40",
		base - 0x80:         "slot-0x80",
		base + 0x80:         "slot+0x80",
		base - 0x100:        "slot-0x100",
		base + 0x100:        "slot+0x100",
		base - 0x200:        "slot-0x200",
		base + 0x200:        "slot+0x200",
		base - 0x400:        "slot-0x400",
		base + 0x400:        "slot+0x400",
		base - 0x800:        "slot-0x800",
		base + 0x800:        "slot+0x800",
	}
	blocks := guestBlocks(h, minBlockMB)
	fmt.Printf("scanning %d blocks for refs to %d slot addresses...\n", len(blocks), len(targets))

	type ref struct {
		at   uintptr
		val  uintptr
		tag  string
	}
	var refs []ref
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(8 << 20)
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if buf != nil {
				for i := 0; i+8 <= len(buf); i += 8 {
					v := *(*uint64)(unsafe.Pointer(&buf[i]))
					if tag, ok := targets[uintptr(v)]; ok {
						refs = append(refs, ref{b.Base + off + uintptr(i), uintptr(v), tag})
					}
				}
			}
			off += n
		}
	}
	fmt.Printf("refs found: %d\n", len(refs))
	// 输出前 60 个（按地址升序）
	for i := 0; i < len(refs); i++ {
		for j := i + 1; j < len(refs); j++ {
			if refs[j].at < refs[i].at {
				refs[i], refs[j] = refs[j], refs[i]
			}
		}
	}
	shown := 0
	for _, r := range refs {
		fmt.Printf("  ref @0x%012X -> 0x%012X (%s)\n", r.at, r.val, r.tag)
		shown++
		if shown >= 60 {
			break
		}
	}
	if len(refs) == 0 {
		fmt.Println("  (none — slot may be referenced via offset, try other anchor variants)")
	}
}
