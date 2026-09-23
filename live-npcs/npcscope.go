// npcscope.go：M3' 逆向第十步——以玩家实时坐标槽为中心，±16MB 范围扫 Npc_ 名字字符串，
// 输出名字及其前后 0x20 字节上下文，定位"运行时 actor 名字"是否内嵌在对象中。
// 用法：live-npcs.exe --npcscope

package main

import (
	"fmt"
	"strings"
)

func runNpcScope(h uintptr) {
	// 先找玩家实时坐标槽（用 findEntitiesNear 定位，或用存档锚点扫）
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no save anchor!")
		return
	}
	var refs [][3]float32
	for _, a := range anchors {
		refs = append(refs, [3]float32{a.Pos[0], a.Pos[1], a.Pos[2]})
	}
	fmt.Printf("save anchor: mem=(%.1f, %.1f, %.1f)\n", refs[0][0], refs[0][1], refs[0][2])

	// 窗口扫描找玩家槽
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
	fmt.Printf("player slot @0x%012X (%.1f, %.1f, %.1f) struct=%d\n",
		playerAddr, player.X, player.Y, player.Z, player.Struct)

	// 以玩家槽为中心 ±16MB 扫名字
	const R = 16 << 20
	start := playerAddr - R
	end := playerAddr + R
	fmt.Printf("scanning Npc_ strings in 0x%012X..0x%012X ...\n", start, end)
	names := scanActorStringsRange(h, start, end)
	fmt.Printf("found %d Npc_/Enemy_/Animal_ strings\n", len(names))

	// 分组：按名字去重，统计每个名字出现的地址和间隔
	seen := map[string][]uintptr{}
	for _, n := range names {
		seen[n.Name] = append(seen[n.Name], n.Addr)
	}
	fmt.Println("\n-- unique names & addresses --")
	for name, addrs := range seen {
		fmt.Printf("  %s x%d", name, len(addrs))
		for _, a := range addrs {
			fmt.Printf("  0x%012X", a)
		}
		fmt.Println()
	}

	// 对每个名字 dump 前后 0x40 字节，看结构
	fmt.Println("\n-- context around first 20 names --")
	n := 0
	for name, addrs := range seen {
		if n >= 20 {
			break
		}
		n++
		buf := readMem(h, addrs[0]-0x20, 0x20+len(name)+0x20)
		if buf == nil {
			fmt.Printf("  %s @0x%012X: unreadable\n", name, addrs[0])
			continue
		}
		hex := ""
		ascii := ""
		for i := 0; i < len(buf); i++ {
			hex += fmt.Sprintf("%02X ", buf[i])
			if buf[i] >= 32 && buf[i] <= 126 {
				ascii += string(buf[i])
			} else {
				ascii += "."
			}
		}
		fmt.Printf("  %s @0x%012X:\n    %s\n    %s\n", name, addrs[0], hex, ascii)
	}
}

// StrHit 一个字符串命中。
type StrHit struct {
	Addr uintptr
	Name string
}

// scanActorStringsRange 在 [start,end] 内扫 Npc_/Enemy_/Animal_ 前缀字符串。
func scanActorStringsRange(h uintptr, start, end uintptr) []StrHit {
	const chunk = 4 << 20
	var out []StrHit
	for a := start; a < end; a += uintptr(chunk) {
		n := uintptr(chunk)
		if a+n > end {
			n = end - a
		}
		buf := readMem(h, a, int(n))
		if buf == nil {
			continue
		}
		for i := 0; i+4 < len(buf); i++ {
			c0 := buf[i]
			if c0 != 'N' && c0 != 'E' && c0 != 'A' {
				continue
			}
			// 检查前缀 Npc_ / Enemy_ / Animal_
			ok := false
			if c0 == 'N' && i+3 < len(buf) && buf[i+1] == 'p' && buf[i+2] == 'c' && buf[i+3] == '_' {
				ok = true
			} else if c0 == 'E' && i+5 < len(buf) && string(buf[i:i+6]) == "Enemy_" {
				ok = true
			} else if c0 == 'A' && i+6 < len(buf) && string(buf[i:i+7]) == "Animal_" {
				ok = true
			}
			if !ok {
				continue
			}
			// 收集直到非打印字符
			j := i
			for j < len(buf) && j-i < 96 && buf[j] >= 32 && buf[j] <= 126 {
				j++
			}
			if j-i < 5 || j-i > 96 {
				continue
			}
			s := string(buf[i:j])
			if strings.ContainsAny(s, "/\\.") {
				continue // 资源路径
			}
			out = append(out, StrHit{Addr: a + uintptr(i), Name: s})
		}
	}
	return out
}
