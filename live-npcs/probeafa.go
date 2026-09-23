// probeafa.go：M3' 逆向第二十步（修正版）——名字对象 hash → 局部反查 → 实例坐标提取。
// 已验证结构（probeaf9）：
//   - 名字对象（ActorInfo 原型）：字符串前 -0x20 有类型指针(0x0AF75140=NPC/0x0AF742C8=Animal)，+0x4C 处 4B = hash
//   - 实例对象：hash 引用点 +0x00 = hash，-0x30 处 3 float = 世界坐标（已验证 3397.1,227.7,2112.9 合法）
// 本步（修正）：只接受干净 Npc_ 名字（无 ':'/'.'/'/' 的资源名过滤掉），hash 取自 +0x4C 且验证非垃圾，
// 在玩家 ±32MB 内反查 hash（局部，不扫全内存→不卡），从引用点 -0x30 提取坐标。
// 用法：live-npcs.exe --probeafa

package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unsafe"
)

func runProbeAFA(h uintptr) {
	// 1) 找玩家实时坐标
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

	// 2) 玩家 ±32MB 扫名字对象（只收干净名字）
	const R = 32 << 20
	names := scanActorStringsRange(h, player.RepAddr-R, player.RepAddr+R)
	fmt.Printf("found %d raw strings\n", len(names))

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
	fmt.Printf("collect %d uniq hashes from %d clean names\n", len(hashSet), len(infos))

	// 3) 局部反查：玩家 ±32MB 内 4B ∈ hashSet
	type hit struct {
		addr uintptr
		hash uint32
	}
	hits := make([]hit, 0, 256)
	lo := player.RepAddr - R
	hi := player.RepAddr + R
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

	// 4) 从引用点 -0x30 提取坐标
	hashName := map[uint32]string{}
	for _, in := range infos {
		hashName[in.hash] = in.name
	}
	type inst struct {
		name string
		addr uintptr
		pos  [3]float32
	}
	var insts []inst
	used := map[uintptr]bool{}
	for _, hh := range hits {
		if used[hh.addr] {
			continue
		}
		used[hh.addr] = true
		name := hashName[hh.hash]
		buf := readMem(h, hh.addr-0x40, 0x40+0x60)
		if buf == nil {
			continue
		}
		x := *(*float32)(unsafe.Pointer(&buf[0x10])) // -0x30
		y := *(*float32)(unsafe.Pointer(&buf[0x14]))
		z := *(*float32)(unsafe.Pointer(&buf[0x18]))
		if !saneF(x) || !saneF(y) || !saneF(z) {
			continue
		}
		// 排除零附近垃圾坐标（实例应有真实位置）
		if math.Abs(float64(x)) < 1 && math.Abs(float64(y)) < 1 && math.Abs(float64(z)) < 1 {
			continue
		}
		insts = append(insts, inst{name: name, addr: hh.addr, pos: [3]float32{x, y, z}})
	}

	// 5) 去重输出：同一名字取离玩家最近的实例
	bestByName := map[string]inst{}
	for _, it := range insts {
		cur, ok := bestByName[it.name]
		if !ok {
			bestByName[it.name] = it
		} else {
			d0 := math.Hypot(float64(cur.pos[0]-px), math.Hypot(float64(cur.pos[1]-py), float64(cur.pos[2]-pz)))
			d1 := math.Hypot(float64(it.pos[0]-px), math.Hypot(float64(it.pos[1]-py), float64(it.pos[2]-pz)))
			if d1 < d0 {
				bestByName[it.name] = it
			}
		}
	}
	keys := make([]string, 0, len(bestByName))
	for k := range bestByName {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("== final npc table (dedup, nearest instance) ==")
	npcCount := 0
	for _, k := range keys {
		it := bestByName[k]
		dist := math.Hypot(float64(it.pos[0]-px), math.Hypot(float64(it.pos[1]-py), float64(it.pos[2]-pz)))
		fmt.Printf("  %-28s @0x%012X pos=(%.1f, %.1f, %.1f) dist=%.0f\n", k, it.addr, it.pos[0], it.pos[1], it.pos[2], dist)
		if strings.HasPrefix(k, "Npc_") {
			npcCount++
		}
	}
	fmt.Printf("\n%d located, %d are Npc_\n", len(bestByName), npcCount)
}

func saneF(v float32) bool {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return false
	}
	if math.Abs(float64(v)) > 50000 {
		return false
	}
	return true
}
