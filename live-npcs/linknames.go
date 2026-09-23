// linknames.go：M3' 逆向第三步——把"实体候选槽"与"名字字符串"通过指针关联。
// 假设：actor 对象内某偏移处有指向名字字符串（Npc_xxx）的 8B 指针。
// 做法：1) 全内存扫名字字符串建 map；2) 窗口扫描实体候选；
//       3) 对每个候选组读结构 ±win 内所有 8B 值，命中名字 map → 关联成功。
// 用法：live-npcs.exe --linknames [--link-win=N]（结构探查字节数，默认 0x8000）

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

func runLinkNames(h uintptr) {
	win := 0x8000
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--link-win=") {
			if n, err := strconv.ParseInt(a[len("--link-win="):], 0, 32); err == nil {
				win = int(n)
			}
		}
	}

	// 1) 名字 map
	blocks := guestBlocks(h, minBlockMB)
	fmt.Printf("scanning name strings...\n")
	hits := scanActorStrings(h, blocks)
	nameAt := map[uintptr]string{}
	for _, hh := range hits {
		nameAt[hh.Addr] = hh.Name
	}
	fmt.Printf("name map: %d entries\n", len(nameAt))

	// 2) 窗口扫描实体候选（锚点 ±400m）
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no anchor")
		return
	}
	refs := [][3]float32{anchors[0].Pos}
	fmt.Printf("scanning entities near anchor (%.1f,%.1f,%.1f)...\n", refs[0][0], refs[0][1], refs[0][2])
	groups, _, secs := findEntitiesNear(h, refs, 400)
	fmt.Printf("%d entity groups in %.1fs\n", len(groups), secs)
	player := findPlayerGroup(groups, anchors)

	// 3) 关联：对每个组，读 RepAddr ±win 内所有 8B 值，查名字 map
	type link struct {
		group *EntityGroup
		name  string
		ptrAt uintptr // 名字指针所在地址
	}
	var links []link
	nolink := 0
	for _, g := range groups {
		if player != nil && g == player {
			continue
		}
		base := g.RepAddr
		if base == 0 {
			base = g.Addrs[0]
		}
		lo, hi := base-uintptr(win), base+uintptr(win)
		if lo > base { // 下溢保护
			lo = 0
		}
		addr := lo
		foundName := ""
		foundAt := uintptr(0)
		for addr < hi && foundName == "" {
			n := 64 << 10
			if hi-addr < uintptr(n) {
				n = int(hi - addr)
			}
			buf := readMem(h, addr, n)
			if buf != nil {
				for i := 0; i+8 <= len(buf); i += 8 {
					v := *(*uint64)(unsafe.Pointer(&buf[i]))
					if v%8 == 0 {
						if nm, ok := nameAt[uintptr(v)]; ok && strings.HasPrefix(nm, "Npc_") {
							foundName = nm
							foundAt = addr + uintptr(i)
							break
						}
					}
				}
			}
			addr += uintptr(n)
		}
		if foundName != "" {
			links = append(links, link{g, foundName, foundAt})
		} else {
			nolink++
		}
	}

	fmt.Printf("\n== linked entities: %d, no-name candidates: %d ==\n", len(links), nolink)
	for i, l := range links {
		if i >= 50 {
			fmt.Printf("  ... (%d more)\n", len(links)-50)
			break
		}
		off := int(l.ptrAt - l.group.RepAddr)
		fmt.Printf("  %-28s @0x%012X (ptr at +0x%X, struct=%d) pos=(%.1f,%.1f,%.1f)\n",
			l.name, l.group.RepAddr, off, l.group.Struct, l.group.X, l.group.Y, l.group.Z)
	}
}
