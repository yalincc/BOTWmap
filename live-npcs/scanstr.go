// scanstr.go：M3' 逆向第二步——全内存扫描 actor 名字字符串。
// 特征：BOTW actor 名字 = "Npc_xxx"/"Enemy_xxx"/"Animal_xxx"/"GameROMPlayer" ASCII 字符串。
// 找到字符串地址后：1) 看分布 2) 反查谁引用它（指针链 → actor 对象）3) 字符串附近找坐标。
// 用法：live-npcs.exe --scanstr [--str-window=N]（找到字符串后顺带在 ±N 内找坐标 float）

package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// scanActorStrings 全 guest RAM 扫描名字字符串，输出命中。
// 返回命中列表 {Addr, Name}。
func scanActorStrings(h uintptr, blocks []struct{ Base, Size uintptr }) []struct {
	Addr uintptr
	Name string
} {
	var hits []struct {
		Addr uintptr
		Name string
	}
	seen := map[uintptr]bool{}
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(8 << 20) // 8MB 块
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if buf != nil {
				for i := 0; i < len(buf)-3; i++ {
					c := buf[i]
					if c < 32 || c > 126 {
						continue
					}
					// 前缀匹配 Npc_/Enemy_/Animal_/GameROM/Obj_/Dummy_（名字 ASCII）
					if !(isPrefix(buf[i:], "Npc_") || isPrefix(buf[i:], "Enemy_") ||
						isPrefix(buf[i:], "Animal_") || isPrefix(buf[i:], "GameROM") ||
						isPrefix(buf[i:], "Obj_") || isPrefix(buf[i:], "Dummy_") ||
						isPrefix(buf[i:], "Event_") || isPrefix(buf[i:], "FldObj_")) {
						continue
					}
					j := i
					for j < len(buf) && buf[j] >= 32 && buf[j] <= 126 && j-i < 128 {
						j++
					}
					if j-i < 4 || j-i > 100 {
						continue
					}
					sa := b.Base + off + uintptr(i)
					if !seen[sa] {
						seen[sa] = true
						hits = append(hits, struct {
							Addr uintptr
							Name string
						}{sa, string(buf[i:j])})
					}
					i = j
				}
			}
			off += n
		}
	}
	return hits
}

func isPrefix(b []byte, p string) bool {
	if len(b) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if b[i] != p[i] {
			return false
		}
	}
	return true
}

// findCoordsNearString 在字符串地址 ±win 内找"合法游戏坐标" float 三元组。
// 返回最接近参考点的坐标命中（地址）。
func findCoordsNearString(h uintptr, strAddr uintptr, win int, ref [3]float32) ([]struct {
	Addr uintptr
	X, Y, Z float32
}, error) {
	var out []struct {
		Addr uintptr
		X, Y, Z float32
	}
	lo := strAddr - uintptr(win)
	hi := strAddr + uintptr(win)
	addr := lo
	for addr < hi {
		n := 64 << 10
		if hi-addr < uintptr(n) {
			n = int(hi - addr)
		}
		buf := readMem(h, addr, n)
		if buf != nil {
			a := floats(buf)
			for i := 0; i+3 <= len(a); i++ {
				x, y, z := a[i], a[i+1], a[i+2]
				if posSane(x, y, z) {
					out = append(out, struct {
						Addr uintptr
						X, Y, Z float32
					}{addr + uintptr(i)*4, x, y, z})
				}
			}
		}
		addr += uintptr(n)
	}
	return out, nil
}

// runScanStr 主入口：扫描名字字符串，输出 + 找坐标 + 反查引用。
func runScanStr(h uintptr) {
	win := 0x1000
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--str-window=") {
			if n, err := strconv.ParseInt(a[len("--str-window="):], 0, 32); err == nil {
				win = int(n)
			}
		}
	}

	blocks := guestBlocks(h, minBlockMB)
	fmt.Printf("scanning %d guest blocks for actor name strings...\n", len(blocks))
	hits := scanActorStrings(h, blocks)
	fmt.Printf("found %d name strings\n", len(hits))

	// 分类统计
	npcN, enemyN, animalN, objN, otherN := 0, 0, 0, 0, 0
	for _, hh := range hits {
		switch {
		case strings.HasPrefix(hh.Name, "Npc_"):
			npcN++
		case strings.HasPrefix(hh.Name, "Enemy_"):
			enemyN++
		case strings.HasPrefix(hh.Name, "Animal_"):
			animalN++
		case strings.HasPrefix(hh.Name, "Obj_"), strings.HasPrefix(hh.Name, "FldObj_"):
			objN++
		default:
			otherN++
		}
	}
	fmt.Printf("  Npc_:%d  Enemy_:%d  Animal_:%d  Obj/FldObj:%d  other:%d\n",
		npcN, enemyN, animalN, objN, otherN)

	// 展示前 60 个（带地址）
	for i, hh := range hits {
		if i >= 60 {
			fmt.Printf("  ... (%d more)\n", len(hits)-60)
			break
		}
		fmt.Printf("  0x%012X  %s\n", hh.Addr, hh.Name)
	}

	// 坐标反查：取前 30 个名字字符串，找其 ±win 内坐标，打印与玩家锚点的关系
	anchors := readSaveAnchors()
	ref := [3]float32{3395.1, 227.7, 2115.9} // 上次锚点兜底
	if len(anchors) > 0 {
		ref = anchors[0].Pos
	}
	fmt.Printf("\n-- coords near name strings (window ±0x%X, ref=(%.1f,%.1f,%.1f)) --\n", win, ref[0], ref[1], ref[2])
	shown := 0
	for _, hh := range hits {
		coords, _ := findCoordsNearString(h, hh.Addr, win, ref)
		// 找离参考点最近的坐标
		bestD := float32(1e18)
		bestC := struct {
			Addr uintptr
			X, Y, Z float32
		}{}
		for _, c := range coords {
			dx, dy, dz := c.X-ref[0], c.Y-ref[1], c.Z-ref[2]
			d := dx*dx + dy*dy + dz*dz
			if d < bestD {
				bestD = d
				bestC = c
			}
		}
		if bestD < 1e17 {
			fmt.Printf("  %s @0x%012X -> coord 0x%012X (%.1f,%.1f,%.1f) d=%.0fm\n",
				hh.Name, hh.Addr, bestC.Addr, bestC.X, bestC.Y, bestC.Z, sqrtF32(bestD))
			shown++
		}
		if shown >= 30 {
			break
		}
	}
	if shown == 0 {
		fmt.Println("  (no coords found in string windows)")
	}
}

func sqrtF32(v float32) float32 {
	return float32(math.Sqrt(float64(v)))
}
