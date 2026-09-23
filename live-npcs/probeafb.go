// probeafb.go：M3' 逆向第二十一步——精修坐标提取。
// 发现：hash 引用点 = actor 实例对象的 hash 字段（+0x00）；但坐标矩阵相对引用点偏移不固定
// （probeaf9 @0x016B16D488A8：3x4 矩阵平移在 +0x5C/+0x6C/+0x7C = (3396.5,230.5,2162.2) 合理）。
// 本步：对每个 hash 引用点，±0x200 扫描全部 4B 对齐 float 三元组，筛选 sane 且与玩家距离<500m 的，
// 输出 (name, ref, offset, pos, dist)，观察坐标稳定偏移。
// 用法：live-npcs.exe --probeafb

package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unsafe"
)

func runProbeAFB(h uintptr) {
	// 1) 玩家
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no save anchor!")
		return
	}
	refs := [][3]float32{{anchors[0].Pos[0], anchors[0].Pos[1], anchors[0].Pos[2]}}
	groups, _, _ := findEntitiesNear(h, refs, 200)
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
	px, py, pz := player.X, player.Y, player.Z
	fmt.Printf("player pos (%.1f, %.1f, %.1f) slot=0x%012X\n", px, py, pz, player.RepAddr)

	// 2) 名字对象 + hash
	const R = 32 << 20
	names := scanActorStringsRange(h, player.RepAddr-R, player.RepAddr+R)
	seen := map[string]uintptr{}
	for _, n := range names {
		if strings.ContainsAny(n.Name, ":/") {
			continue
		}
		if _, ok := seen[n.Name]; !ok {
			seen[n.Name] = n.Addr
		}
	}
	type nameInfo struct {
		name string
		addr uintptr
		hash uint32
	}
	infos := make([]nameInfo, 0, len(seen))
	hashSet := make(map[uint32]bool)
	for name, addr := range seen {
		buf := readMem(h, addr, 0x60)
		if buf == nil {
			continue
		}
		hv := *(*uint32)(unsafe.Pointer(&buf[0x4C]))
		if hv == 0 || hv == 0x96000000 {
			continue
		}
		infos = append(infos, nameInfo{name: name, addr: addr, hash: hv})
		hashSet[hv] = true
	}
	fmt.Printf("%d clean names, %d uniq hashes\n", len(infos), len(hashSet))

	// 3) 局部反查
	lo := player.RepAddr - R
	hi := player.RepAddr + R
	type hit struct {
		addr uintptr
		hash uint32
	}
	hits := make([]hit, 0, 512)
	chunk := 4 << 20
	for a := lo; a < hi; a += uintptr(chunk) {
		n := uintptr(chunk)
		if a+n > hi {
			n = hi - a
		}
		buf := readMem(h, a, int(n))
		if buf == nil {
			continue
		}
		for i := 0; i+4 <= len(buf); i += 4 {
			v := *(*uint32)(unsafe.Pointer(&buf[i]))
			if hashSet[v] {
				hits = append(hits, hit{addr: a + uintptr(i), hash: v})
			}
		}
	}
	fmt.Printf("local hash hits: %d\n\n", len(hits))

	// 4) 每个 hit ±0x200 扫坐标三元组
	hashName := map[uint32]string{}
	for _, in := range infos {
		hashName[in.hash] = in.name
	}
	type cand struct {
		name   string
		ref    uintptr
		off    int
		pos    [3]float32
		dist   float64
		hash   uint32
	}
	// 去重：同 ref 只留最近的一个
	byRef := map[uintptr]cand{}
	for _, hh := range hits {
		name := hashName[hh.hash]
		buf := readMem(h, hh.addr-0x200, 0x400+0x40)
		if buf == nil {
			continue
		}
		best := cand{name: name, ref: hh.addr, hash: hh.hash, dist: 1e9}
		for off := 0; off+12 <= 0x400; off += 4 {
			x := *(*float32)(unsafe.Pointer(&buf[off]))
			y := *(*float32)(unsafe.Pointer(&buf[off+4]))
			z := *(*float32)(unsafe.Pointer(&buf[off+8]))
			if !saneF(x) || !saneF(y) || !saneF(z) {
				continue
			}
			// 世界坐标范围粗筛（BOTW 地图约 ±10000）
			if math.Abs(float64(x)) > 12000 || math.Abs(float64(y)) > 12000 || math.Abs(float64(z)) > 12000 {
				continue
			}
			d := math.Hypot(float64(x-px), math.Hypot(float64(y-py), float64(z-pz)))
			if d < best.dist {
				best.dist = d
				best.pos = [3]float32{x, y, z}
				best.off = off - 0x200
			}
		}
		if best.dist < 500 {
			if cur, ok := byRef[hh.addr]; !ok || best.dist < cur.dist {
				byRef[hh.addr] = best
			}
		}
	}

	// 5) 输出：同名字取最近，打印偏移分布
	fmt.Println("== per-name nearest instance (dist<500m) ==")
	byName := map[string]cand{}
	for _, c := range byRef {
		cur, ok := byName[c.name]
		if !ok || c.dist < cur.dist {
			byName[c.name] = c
		}
	}
	keys := make([]string, 0, len(byName))
	for k := range byName {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	offCount := map[int]int{}
	npcCount := 0
	for _, k := range keys {
		c := byName[k]
		offCount[c.off]++
		fmt.Printf("  %-24s hash=0x%08X ref@0x%012X off=%+4d pos=(%.1f, %.1f, %.1f) dist=%.0f\n",
			k, c.hash, c.ref, c.off, c.pos[0], c.pos[1], c.pos[2], c.dist)
		if strings.HasPrefix(k, "Npc_") {
			npcCount++
		}
	}
	fmt.Printf("\n%d located, %d Npc_. offset histogram: ", len(byName), npcCount)
	offs := make([]int, 0, len(offCount))
	for o := range offCount {
		offs = append(offs, o)
	}
	sort.Ints(offs)
	for _, o := range offs {
		fmt.Printf("%+d:%d ", o, offCount[o])
	}
	fmt.Println()
}
