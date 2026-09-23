// npcscan.go：M3' 正式数据源——ActorInfo hash → 实例对象定位器。
//
// 已逆向确认的内存结构（probeaf9/afc/afd 实机验证）：
//   1) ActorInfo（原型/名字对象）：名字字符串 +0x4C 处 4B = actor hash（Npc_ 系列 0x344xxxxx）
//   2) 实例对象：内存中任意 4B == hash 的位置即"实例引用点"；
//      实例对象结构：+0x00=hash，+0x40 起 4x4 变换矩阵，+0x80/+0x84/+0x88 = 世界坐标 XYZ
//   3) 组件对象（Weapon_R/Pod_A 等）也有 hash 引用但无变换矩阵 → +0x80 读出来非 sane → 自动过滤
//
// 本实现只跟踪 Npc_ 前缀（用户要求：只要 NPC，不要动物/敌人）。
// 扫描范围固定为 0x016B 运行时区（已提交块自动跳过），不依赖玩家槽作锚点
// （玩家槽偶发漂移会导致窗口偏移 → 之前 9↔1 个 NPC 抖动）。

package main

import (
	"fmt"
	"math"
	"time"
	"unsafe"
)

// npcScan 一次扫描结果。
type npcScan struct {
	player  *EntityGroup
	npcs    []Npc // 已按名字去重、距离过滤
	hits    int
	elapsed time.Duration
}

const (
	npcRegionLo = 0x016B12000000 // 运行时 actor 区（实测实例 0x016B16~0x016B1A，玩家槽 0x016B14）
	npcRegionHi = 0x016B20000000
	npcMaxDist  = 500.0 // 只显示玩家 ±500m 内 NPC
)

// clipBlocks 返回 guest 地址区间 [lo,hi) 内已提交的块（复用 findPlayerInRange 逻辑）。
func clipBlocks(h uintptr, lo, hi uintptr) []struct{ Base, Size uintptr } {
	var blocks []struct{ Base, Size uintptr }
	for _, b := range guestBlocks(h, minBlockMB) {
		bl := b.Base
		br := bl + b.Size
		if br <= lo || bl >= hi {
			continue
		}
		if bl < lo {
			bl = lo
		}
		if br > hi {
			br = hi
		}
		if br > bl {
			blocks = append(blocks, struct{ Base, Size uintptr }{bl, br - bl})
		}
	}
	return blocks
}

// scanNpcInstances 主扫描：定位玩家 → 扫 0x016B 区 Npc_ ActorInfo hash → 反查 → 实例坐标。
func scanNpcInstances(h uintptr) *npcScan {
	t0 := time.Now()
	res := &npcScan{}

	// 0) 0x016B 区已提交块（只扫这些，跳过 4GB 地址空间里未提交的空白）
	blocks := clipBlocks(h, npcRegionLo, npcRegionHi)

	// 1) 玩家（限定 0x016B 运行时区；仅用于距离过滤）
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		return res
	}
	refs := [][3]float32{{anchors[0].Pos[0], anchors[0].Pos[1], anchors[0].Pos[2]}}
	player := findPlayerInRange(h, refs, npcRegionLo, npcRegionHi, 200)
	if player == nil {
		groups, _, _ := findEntitiesNear(h, refs, 200)
		for _, g := range groups {
			if g.Struct >= 6 {
				player = g
				break
			}
		}
	}
	if player == nil {
		return res
	}
	res.player = player
	px, py, pz := player.X, player.Y, player.Z

	// 2) 用全局 ActorInfo 缓存取 Npc_ hash 集合
	//    （名字对象全内存低频收集见 actorinfo.go；0x016B 区只是实例区，
	//    名字对象大多驻留 0x016A/0x020C 等资源区，只扫 0x016B 会漏 NPC）
	hashName, ready := actorHashSnapshot()
	if !ready {
		return res // 首次全内存收集未完成，等下一帧
	}
	hashSet := make(map[uint32]bool, len(hashName))
	for hv := range hashName {
		hashSet[hv] = true
	}

	// 3) 已提交块内反查 hash
	type hit struct {
		addr uintptr
		hash uint32
	}
	var hits []hit
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(4 << 20)
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if buf == nil {
				off += n
				continue
			}
			for i := 0; i+4 <= len(buf); i += 4 {
				v := *(*uint32)(unsafe.Pointer(&buf[i]))
				if hashSet[v] {
					hits = append(hits, hit{addr: b.Base + off + uintptr(i), hash: v})
				}
			}
			off += n
		}
	}
	res.hits = len(hits)

	// 4) 实例坐标：+0x80/+0x84/+0x88；按名字收集全部引用点坐标，
	//    聚类取"引用点数最多的簇"（主簇）作为该 NPC 位置。
	//    原因（probeafe 实机诊断）：一个 actor 被多个组件持有者引用，同一位置会有
	//    十几份坐标副本（真位置）；而 hash 偶尔落在其他实例对象体内，+0x80 会读出
	//    相邻对象的坐标（伪点，如 Amira @0x016B16D48958 读出 Village003 的坐标）。
	//    取最近实例会让伪点/多候选互相切换 → 位置跳动；聚类主簇可压制孤立伪点。
	coordsByName := map[string][][3]float32{}
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
		buf := readMem(h, hh.addr, 0x40+0x88)
		if buf == nil {
			continue
		}
		x := *(*float32)(unsafe.Pointer(&buf[0x80]))
		y := *(*float32)(unsafe.Pointer(&buf[0x84]))
		z := *(*float32)(unsafe.Pointer(&buf[0x88]))
		if !saneF(x) || !saneF(y) || !saneF(z) {
			continue
		}
		if math.Abs(float64(x)) > 12000 || math.Abs(float64(y)) > 12000 || math.Abs(float64(z)) > 12000 {
			continue
		}
		// 近零平移 = 未激活/管理器对象的单位矩阵（平移 (0,0,0)/(1,0,0)/(0,0,1) 等），
		// 不是真实例坐标，直接剔除（BOTW 世界坐标原点在海域，真实 NPC 距原点 >100m）。
		if math.Hypot(float64(x), math.Hypot(float64(y), float64(z))) < 5.0 {
			continue
		}
		// 旋转矩阵判别：+0x40 起 3x3 必须是正交旋转（行范数≈1、列点积≈0）。
		// 剔除"hash 落在其他实例对象体内"的伪引用（如 Clavia 的引用点落在 Toma
		// 对象体内，+0x80 读出 Toma 坐标 → 红点跳到别人家）。
		if !rotSane(buf) {
			continue
		}
		coordsByName[name] = append(coordsByName[name], [3]float32{x, y, z})
	}

	// 5) 输出列表：每个名字取主簇质心，再按玩家距离过滤
	type inst struct {
		pos  [3]float32
		dist float64
	}
	bestByName := map[string]inst{}
	for name, coords := range coordsByName {
		cx, cy, cz, count := majorityCluster(coords, 2.0)
		if count < 1 {
			continue
		}
		pos := [3]float32{cx, cy, cz}
		d := math.Hypot(float64(cx-px), math.Hypot(float64(cy-py), float64(cz-pz)))
		if d > npcMaxDist {
			continue
		}
		cur, ok := bestByName[name]
		if !ok || d < cur.dist {
			bestByName[name] = inst{pos: pos, dist: d}
		}
	}

	for name, it := range bestByName {
		res.npcs = append(res.npcs, Npc{
			Name:    name,
			Label:   labelOf(name),
			LabelEn: labelEnOf(name),
			X:       it.pos[0], Y: it.pos[1], Z: it.pos[2],
			MX: int(it.pos[0]*2) + 12000, MY: int(it.pos[2]*2) + 10000,
			Addr: 0,
		})
	}
	res.elapsed = time.Since(t0)
	return res
}

// rotSane 检查引用点 +0x40 起 3x3 是否像正交旋转矩阵（允许各向同性缩放 ≤3）。
// 实例对象的变换矩阵是"旋转+平移"；组件/体内伪引用读出的数据不是矩阵 → 剔除。
func rotSane(buf []byte) bool {
	if len(buf) < 0x40+12*4 {
		return false
	}
	m := make([]float32, 12)
	for k := 0; k < 12; k++ {
		m[k] = *(*float32)(unsafe.Pointer(&buf[0x40+k*4]))
		if !saneF(m[k]) {
			return false
		}
	}
	row := [3][3]float32{
		{m[0], m[1], m[2]},
		{m[4], m[5], m[6]},
		{m[8], m[9], m[10]},
	}
	for r := 0; r < 3; r++ {
		n := math.Sqrt(float64(row[r][0]*row[r][0] + row[r][1]*row[r][1] + row[r][2]*row[r][2]))
		if n < 0.3 || n > 3.0 {
			return false // 行范数离谱 → 不是旋转矩阵
		}
	}
	for c1 := 0; c1 < 3; c1++ {
		for c2 := c1 + 1; c2 < 3; c2++ {
			dot := float64(row[0][c1]*row[0][c2] + row[1][c1]*row[1][c2] + row[2][c1]*row[2][c2])
			if math.Abs(dot) > 0.5 {
				return false // 列不正交 → 不是旋转矩阵
			}
		}
	}
	return true
}

// majorityCluster 坐标贪心聚类（ε 米），返回引用点数最多簇的质心与点数。
func majorityCluster(coords [][3]float32, eps float64) (float32, float32, float32, int) {
	type cl struct {
		pts [][3]float32
	}
	var cls []*cl
	for _, c := range coords {
		placed := false
		for _, g := range cls {
			// 簇质心
			var sx, sy, sz float64
			for _, p := range g.pts {
				sx, sy, sz = sx+float64(p[0]), sy+float64(p[1]), sz+float64(p[2])
			}
			n := float64(len(g.pts))
			dx := float64(c[0]) - sx/n
			dy := float64(c[1]) - sy/n
			dz := float64(c[2]) - sz/n
			if dx*dx+dy*dy+dz*dz <= eps*eps {
				g.pts = append(g.pts, c)
				placed = true
				break
			}
		}
		if !placed {
			cls = append(cls, &cl{pts: [][3]float32{c}})
		}
	}
	best := cls[0]
	for _, g := range cls[1:] {
		if len(g.pts) > len(best.pts) {
			best = g
		}
	}
	var sx, sy, sz float64
	for _, p := range best.pts {
		sx, sy, sz = sx+float64(p[0]), sy+float64(p[1]), sz+float64(p[2])
	}
	n := float64(len(best.pts))
	return float32(sx / n), float32(sy / n), float32(sz / n), len(best.pts)
}

// trackNpcsV2 服务循环：用 scanNpcInstances 替换启发式扫描（M3'）。
func trackNpcsV2(h uintptr) {
	prev := map[string][3]float32{} // name → 上一帧坐标
	go refreshActorInfo(h) // 启动即全内存收集（后台，~15s），此后每 60s 自动刷新
	for {
		if needActorInfoRefresh() {
			go refreshActorInfo(h) // 后台刷新，不阻塞主扫描
		}
		t0 := time.Now()
		scan := scanNpcInstances(h)
		if scan.player == nil {
			fmt.Println("[live-npcs] player not found; waiting...")
			time.Sleep(3 * time.Second)
			continue
		}
		// moving：与上一帧同名坐标比较
		cur := map[string][3]float32{}
		for i := range scan.npcs {
			np := &scan.npcs[i]
			xyz := [3]float32{np.X, np.Y, np.Z}
			cur[np.Name] = xyz
			if old, ok := prev[np.Name]; ok {
				dx, dy, dz := xyz[0]-old[0], xyz[1]-old[1], xyz[2]-old[2]
				d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
				if d > 0.5 {
					np.Moving = true
				}
			}
		}
		prev = cur

		state.mu.Lock()
		state.list = scan.npcs
		state.ok = true
		state.updated = time.Now()
		state.player = Npc{
			X: scan.player.X, Y: scan.player.Y, Z: scan.player.Z,
			MX: int(scan.player.X*2) + 12000, MY: int(scan.player.Z*2) + 10000,
			Addr: scan.player.RepAddr,
		}
		state.addr = scan.player.RepAddr
		state.mu.Unlock()

		elapsed := time.Since(t0)
		fmt.Printf("[live-npcs] %d npc (%d hits) in %s\n", len(scan.npcs), scan.hits, elapsed.Round(time.Millisecond))
		time.Sleep(refreshEvery)
	}
}
