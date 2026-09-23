// probeafc.go：M3' 逆向第二十二步——正式实例定位器。
// 已验证（probeaf9/afb）：
//   名字对象(ActorInfo)：字符串+0x00，-0x20 处 4B=类型指针(0x0AF75140=NPC/0x0AF742C8=Animal)，+0x4C=hash
//   实例对象：+0x00=hash，+0x5C/+0x6C/+0x7C = 世界坐标（4x4 矩阵列主序平移列，Npc_HatenoVillage003 实例实测）
// 本步：完整流水线 → 只输出 Npc_ 且坐标合理（距离<1000m）。
// 用法：live-npcs.exe --probeafc

package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unsafe"
)

func runProbeAFC(h uintptr) {
	// 1) 玩家（限定 0x016B 运行时区，避免资源表区误命中）
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no save anchor!")
		return
	}
	refs := [][3]float32{{anchors[0].Pos[0], anchors[0].Pos[1], anchors[0].Pos[2]}}
	var player *EntityGroup
	// 优先 0x016B 区
	player = findPlayerInRange(h, refs, 0x016B00000000, 0x016C00000000, 200)
	if player == nil {
		// fallback 全量
		groups, _, _ := findEntitiesNear(h, refs, 200)
		for _, g := range groups {
			if g.Struct >= 6 {
				player = g
				break
			}
		}
	}
	if player == nil {
		fmt.Println("player slot not found!")
		return
	}
	px, py, pz := player.X, player.Y, player.Z
	fmt.Printf("player pos (%.1f, %.1f, %.1f) slot=0x%012X\n", px, py, pz, player.RepAddr)

	// 2) 名字对象：不按名字去重，逐位置验证 hash（+0x4C，ActorInfo hash 特征 0x344xxxxx）
	type nameInfo struct {
		name string
		addr uintptr
		hash uint32
		typ  uint32
	}
	const R = 32 << 20
	rawNames := scanActorStringsRange(h, player.RepAddr-R, player.RepAddr+R)
	seen := map[string]bool{}
	infos := make([]nameInfo, 0, len(rawNames))
	hashSet := make(map[uint32]bool)
	debugShow := 0
	for _, n := range rawNames {
		if strings.ContainsAny(n.Name, ":/") {
			continue
		}
		buf := readMem(h, n.Addr, 0x60)
		if buf == nil {
			continue
		}
		hv := *(*uint32)(unsafe.Pointer(&buf[0x4C])) // +0x4C
		if debugShow < 30 {
			fmt.Printf("  dbg %-24s @0x%012X hash=0x%08X\n", n.Name, n.Addr, hv)
			debugShow++
		}
		// ActorInfo hash 特征：0x34400000..0x34500000
		if hv < 0x34400000 || hv >= 0x34500000 {
			continue
		}
		if seen[n.Name] {
			continue // 每名字只保留一个 ActorInfo
		}
		seen[n.Name] = true
		infos = append(infos, nameInfo{name: n.Name, addr: n.Addr, hash: hv, typ: 0})
		hashSet[hv] = true
	}
	fmt.Printf("%d valid ActorInfo (%d uniq hashes)\n", len(infos), len(hashSet))
	for _, in := range infos {
		fmt.Printf("  info %-24s @0x%012X hash=0x%08X\n", in.name, in.addr, in.hash)
	}

	// 3) 局部反查 hash
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
	fmt.Printf("local hash hits: %d\n", len(hits))
	hashName := map[uint32]string{}
	for _, in := range infos {
		hashName[in.hash] = in.name
	}
	for _, hh := range hits {
		if hashName[hh.hash] == "" {
			continue
		}
		fmt.Printf("  hit %-24s hash=0x%08X @0x%012X\n", hashName[hh.hash], hh.hash, hh.addr)
	}
	fmt.Println()

	// 4) 实例：hash 引用点 +0x80 处连续坐标（实例对象 4x4 矩阵平移列；组件对象无矩阵）
	type inst struct {
		name string
		addr uintptr
		off  int
		pos  [3]float32
		dist float64
	}
	var insts []inst
	seenRef := map[uintptr]bool{}
	for _, hh := range hits {
		if seenRef[hh.addr] {
			continue
		}
		seenRef[hh.addr] = true
		name := hashName[hh.hash]
		if name == "" {
			continue
		}
		buf := readMem(h, hh.addr, 0x40+0x80)
		if buf == nil {
			continue
		}
		x := *(*float32)(unsafe.Pointer(&buf[0x80])) // +0x80
		y := *(*float32)(unsafe.Pointer(&buf[0x84])) // +0x84
		z := *(*float32)(unsafe.Pointer(&buf[0x88])) // +0x88
		if !saneF(x) || !saneF(y) || !saneF(z) {
			continue
		}
		if math.Abs(float64(x)) > 12000 || math.Abs(float64(y)) > 12000 || math.Abs(float64(z)) > 12000 {
			continue
		}
		d := math.Hypot(float64(x-px), math.Hypot(float64(y-py), float64(z-pz)))
		if d > 500 {
			continue
		}
		insts = append(insts, inst{name: name, addr: hh.addr, off: 0x80, pos: [3]float32{x, y, z}, dist: d})
	}

	// 5) 只保留 Npc_，去重取最近
	byName := map[string]inst{}
	for _, it := range insts {
		if !strings.HasPrefix(it.name, "Npc_") {
			continue
		}
		cur, ok := byName[it.name]
		if !ok || it.dist < cur.dist {
			byName[it.name] = it
		}
	}
	keys := make([]string, 0, len(byName))
	for k := range byName {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("== Npc_ instances (dedup nearest) ==")
	for _, k := range keys {
		it := byName[k]
		fmt.Printf("  %-24s @0x%012X off=%+4d pos=(%.1f, %.1f, %.1f) dist=%.0f\n",
			k, it.addr, it.off, it.pos[0], it.pos[1], it.pos[2], it.dist)
	}
	fmt.Printf("\n%d Npc_ located\n", len(byName))
}

// findPlayerInRange 只在 [lo,hi] 的 host 区间内窗口扫描玩家槽。
func findPlayerInRange(h uintptr, refs [][3]float32, lo, hi, window float32) *EntityGroup {
	var blocks []struct{ Base, Size uintptr }
	for _, b := range guestBlocks(h, minBlockMB) {
		bl := uintptr(b.Base)
		br := bl + b.Size
		sl, sr := uintptr(lo), uintptr(hi)
		if br <= sl || bl >= sr {
			continue
		}
		// 裁剪到区间内
		if bl < sl {
			bl = sl
		}
		if br > sr {
			br = sr
		}
		if br > bl {
			blocks = append(blocks, struct{ Base, Size uintptr }{bl, br - bl})
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	hits := scanWindowHits(h, blocks, refs, window)
	groups := groupHits(h, hits, 500)
	for _, g := range groups {
		if g.Struct >= 6 {
			return g
		}
	}
	return nil
}
