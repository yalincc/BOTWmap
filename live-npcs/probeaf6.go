// probeaf6.go：M3' 逆向第十六步——在名字对象附近找实例坐标。
// 名字对象 = actor 类型原型（含 hash/类型指针），实例坐标应在对象附近或通过指针可达。
// 对每个 Npc_ 名字对象 ±0x1000 内找 posSane 坐标三元组。
// 用法：live-npcs.exe --probeaf6

package main

import (
	"fmt"
	"unsafe"
)

func runProbeAF6(h uintptr) {
	// 1) 玩家槽定位
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no save anchor!")
		return
	}
	var refs [][3]float32
	for _, a := range anchors {
		refs = append(refs, [3]float32{a.Pos[0], a.Pos[1], a.Pos[2]})
	}
	blocks := guestBlocks(h, minBlockMB)
	hits := scanWindowHits(h, blocks, refs, 200)
	groups := groupHits(h, hits, 100)
	var player *EntityGroup
	for _, g := range groups {
		if g.Struct >= 6 {
			player = g
			break
		}
	}
	if player == nil {
		fmt.Println("player slot not found!")
		return
	}
	playerAddr := player.RepAddr
	fmt.Printf("player slot @0x%012X (%.1f, %.1f, %.1f)\n", playerAddr, player.X, player.Y, player.Z)

	// 2) 玩家槽 ±16MB 扫 Npc_ 名字
	const R = 16 << 20
	names := scanActorStringsRange(h, playerAddr-R, playerAddr+R)
	fmt.Printf("found %d names in ±16MB\n", len(names))

	// 3) 对每个 Npc_ 名字对象 ±0x1000 找合法坐标
	seen := map[string]bool{}
	type coordHit struct {
		name  string
		addr  uintptr
		x, y, z float32
		d     float32
	}
	var coords []coordHit
	for _, n := range names {
		if seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		if len(n.Name) < 4 || n.Name[:4] != "Npc_" {
			continue
		}
		buf := readMem(h, n.Addr-0x1000, 0x2000)
		if buf == nil {
			continue
		}
		a := floats(buf)
		base := n.Addr - 0x1000
		for i := 0; i+3 <= len(a); i++ {
			x, y, z := a[i], a[i+1], a[i+2]
			if !posSane(x, y, z) {
				continue
			}
			// 与玩家距离
			dx, dy, dz := x-player.X, y-player.Y, z-player.Z
			d := float32(0)
			if dx < 0 {
				d -= dx
			} else {
				d += dx
			}
			if dy < 0 {
				d -= dy
			} else {
				d += dy
			}
			if dz < 0 {
				d -= dz
			} else {
				d += dz
			}
			ca := base + uintptr(i)*4
			coords = append(coords, coordHit{n.Name, ca, x, y, z, d})
		}
	}
	// 按名字输出（每个名字最多 3 个坐标）
	fmt.Println("\n-- coords near Npc_ name objects --")
	byName := map[string][]coordHit{}
	for _, c := range coords {
		if len(byName[c.name]) < 3 {
			byName[c.name] = append(byName[c.name], c)
		}
	}
	for name, cs := range byName {
		fmt.Printf("  %s:\n", name)
		for _, c := range cs {
			fmt.Printf("    @0x%012X (%.1f, %.1f, %.1f) dist=%.0f\n", c.addr, c.x, c.y, c.z, c.d)
		}
	}
	if len(byName) == 0 {
		fmt.Println("  (no coords found)")
	}

	// 4) dump Npc_HatenoVillage003 名字对象完整结构 ±0x200
	fmt.Println("\n-- full dump Npc_HatenoVillage003 obj --")
	for _, n := range names {
		if n.Name == "Npc_HatenoVillage003" {
			buf := readMem(h, n.Addr-0x80, 0x80+0x200)
			if buf == nil {
				fmt.Println("unreadable")
				break
			}
			for off := 0; off < 0x80+0x200; off += 0x10 {
				var hex, ascii string
				for i := off; i < off+0x10; i++ {
					hex += fmt.Sprintf("%02X ", buf[i])
					if buf[i] >= 32 && buf[i] <= 126 {
						ascii += string(buf[i])
					} else {
						ascii += "."
					}
				}
				fmt.Printf("  %+04X: %s |%s|\n", off-0x80, hex, ascii)
			}
			break
		}
	}

	// 5) 名字对象 -0x20 类型指针区域：找 0x0AF7 区真实映射基址
	fmt.Println("\n-- locate real mapping base for guest 0x0AF7/0x0B11 region --")
	for _, g := range []uintptr{0x0AF73AF0, 0x0AF742C8, 0x0AF75140, 0x0B110EB8, 0x0B1117E0} {
		// 尝试多个基址
		for _, b := range []uintptr{0x016A00000000, 0x016ADCA00000, 0x016B00000000, 0x016C00000000, 0x020C00000000, 0x020D00000000, 0x016000000000, 0x017000000000} {
			addr := b + g
			buf := readMem(h, addr, 0x20)
			if buf == nil {
				continue
			}
			nonzero := false
			for i := 0; i < 0x20; i++ {
				if buf[i] != 0 {
					nonzero = true
					break
				}
			}
			if nonzero {
				u0 := *(*uint32)(unsafe.Pointer(&buf[0]))
				fmt.Printf("  guest 0x%X readable @ host 0x%012X (base 0x%012X) u32[0]=0x%08X\n", g, addr, b, u0)
			}
		}
	}
}
