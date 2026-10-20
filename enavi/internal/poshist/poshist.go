// Package poshist = 玩家位置历史缓冲定位（2026-10-20 实测定案）。
//
// # 这是什么
//
// BOTW 1.6.0 运行时，**玩家位置历史缓冲**长这样：
//
//	基址 + 0x00  elem0  （空，未使用）
//	基址 + 0x28  elem1  ★ 当前帧：位置 + 朝向四元数
//	基址 + 0x50  elem2     上一帧
//	基址 + 0x78  elem3  （空）
//	基址 + 0xA0  elem4  （空）
//
// 每个元素 0x28（40）字节：
//
//	+0x00 : Vector3f 位置 (X, alt, Z)      ← 12 字节
//	+0x0C : float  w ∈ (0.90, 1.00)       ← 朝向四元数 w
//	+0x10 : float  x ∈ (-0.5, 0.5)
//	+0x14 : float  y ∈ (-0.5, 0.5)
//	+0x18 : float  朝向角（单位：度）
//	+0x1C : 0
//	+0x20 : 指针
//	+0x24 : 标志
//
// # 怎么找到它
//
// 「四元数指纹 + 合法世界坐标」的组合在 12GB 内存里唯一（实测 ±0x200 区域内只有 2 个命中）。
// 定位流程：
//
//	1. 扫 UserHeap，找带四元数指纹的元素；
//	2. 往上回退到 0x28 对齐的元素起点；
//	3. 校验「下一个元素（+0x28）也是合法坐标 + 带指纹」→ 确认是同一个缓冲；
//	4. 候选地址写盘（known_player.json），同一会话直接用。
//
// # 语义（重要）
//
// - 玩家**移动**时 elem1 每帧更新 → 坐标实时；
// - 玩家**静止**时 elem1 保留最后位置（不会归零）→ 地图上的位置仍然正确；
// - 传送时 elem1 直接跳到新位置 → 天然支持传送。
package poshist

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

const (
	// CellSize 元素步长（字节）。
	CellSize = 0x28
	// PosSize 位置字段长度。
	PosSize = 12
	// CurrentCell 当前帧在基址里的偏移（实测 elem1 恒为当前帧，不轮转）。
	CurrentCell = 0x28
)

// Fingerprint 判据常量。
const (
	quatWMin = 0.90
	quatWMax = 1.00
	quatXY   = 0.5
	quatAng  = 180.0
)

// Scan 扫描并定位玩家位置历史缓冲。
//
// 返回 elem1（当前帧）的宿主地址；找不到返回 0。
func Scan(p *winmem.Process) (uintptr, error) {
	return ScanWithAnchor(p, [3]float32{}, false)
}

// ScanWithAnchor 定位位置历史缓冲；anchor 是存档坐标（可选，用于玩家静止时的兜底判定）。
func ScanWithAnchor(p *winmem.Process, anchor [3]float32, anchorValid bool) (uintptr, error) {
	staticCandidates = nil
	d, _, err := p.FindDram()
	if err != nil {
		return 0, err
	}
	regions := scan.HeapRegions(p, d, 256<<10)
	if len(regions) == 0 {
		return 0, fmt.Errorf("User Heap 为空：Eden 未载入游戏")
	}

	type cand struct {
		addr uintptr
		quat int // 连续带指纹的元素数
	}
	var cands []cand
	var mu sync.Mutex

	// 限速：绝不能把模拟器拖死（实测全速扫会卡住 Eden）。
	runChunksLimited(p, regions, 2, 10*time.Millisecond, func(addr uintptr, buf []byte) {
		var local []cand
		for off := 0; off+CellSize+0x1C <= len(buf); off += 4 {
			if !hasQuat(buf[off:]) {
				continue
			}
			p0 := readF3(buf[off:])
			if !worldCoordOK(p0[0], p0[1], p0[2]) {
				continue
			}
			// 下一个元素也是合法坐标 + 带指纹 → 确认是同一个缓冲
			nq := 1
			if off+CellSize+0x1C <= len(buf) {
				q := readF3(buf[off+CellSize:])
				if worldCoordOK(q[0], q[1], q[2]) && hasQuat(buf[off+CellSize:]) {
					dx := float64(q[0] - p0[0])
					dz := float64(q[2] - p0[2])
					if math.Hypot(dx, dz) < 200 {
						nq = 2
					}
				}
			}
			if nq < 2 {
				continue
			}
			local = append(local, cand{addr + uintptr(off), nq})
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		cands = append(cands, local...)
		mu.Unlock()
	})

	if len(cands) == 0 {
		return 0, fmt.Errorf("未找到位置历史缓冲（游戏可能在主菜单，或玩家完全静止）")
	}
	// 指纹多的优先；同级时取水平坐标更"像玩家"的（玩家不会在原点）
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].quat != cands[j].quat {
			return cands[i].quat > cands[j].quat
		}
		return true
	})

	// 候选排序：先按「离存档锚点近」排（玩家附近的东西最可能是玩家的缓冲），
	// 再按「四元数指纹连续数」排。
	if anchorValid {
		sort.SliceStable(cands, func(i, j int) bool {
			vi, _ := ReadPos(p, cands[i].addr)
			vj, _ := ReadPos(p, cands[j].addr)
			di := math.Hypot(float64(vi[0]-anchor[0]), float64(vi[2]-anchor[2]))
			dj := math.Hypot(float64(vj[0]-anchor[0]), float64(vj[2]-anchor[2]))
			return di < dj
		})
	}

	// 逐个候选做「二次确认」。
	//
	// ★ 关键：光「坐标合法」不够 —— 内存里有大量 NPC / 物体的位置表，
	//   它们的坐标也合法、也带朝向四元数，但**不会随玩家移动而变**。
	//   所以必须验证「这个槽会变」：
	//     隔一小段时间再读一次，位置变了 → 是玩家；
	//     没变 → 大概率是静态表（除非玩家本来就站着不动）。
	//   玩家静止的情况由「与存档坐标接近」来兜底。
	// 先把候选的初始值都读一遍（快），只留坐标合法的
	type probe struct {
		cur        uintptr
		v          [3]float32
		moved      bool
		dist       float64
		anchorDist float64
	}
	var probes []probe
	for _, c := range cands {
		base := c.addr - CurrentCell
		if base < CurrentCell {
			continue
		}
		cur := base + CurrentCell
		v, ok := ReadPos(p, cur)
		if !ok {
			continue
		}
		if !worldCoordOK(v[0], v[1], v[2]) {
			continue
		}
		probes = append(probes, probe{cur: cur, v: v})
	}
	if len(probes) == 0 {
		return 0, fmt.Errorf("找到 %d 个候选但都读不出合法坐标", len(cands))
	}

	// ─────────────────────────────────────────────────────────────
	// 判定顺序（★ 2026-10-21 01:10 修正，之前锁到 NPC 过）
	//
	// 旧逻辑：先找「会动的」→ 会动就返回。
	//   ✗ 错在哪：**NPC 也会动**。实测锁到一个 2221 米外的 NPC，
	//     它一直在自己路线图上跑，每 3 秒移动 35 米，跟玩家跑速一样。
	//
	// 新逻辑：**存档坐标是首要判据**，「会动」只是辅助确认。
	//   理由：玩家 01:06 刚存的档，存档坐标就是他的位置（误差 = 存档后走的距离，
	//   通常几十米）。而 NPC 可能在几千公里外的自己路线上。
	//
	//   ① 有存档参照时：按「离存档距离」排序
	//      · 最近的在 AnchorNearDist 内 → **直接锁定**（几乎必定是玩家）
	//      · 否则（玩家传送去了远处）→ 再看哪个会动，在会动的里挑离存档最近的
	//   ② 没存档参照时：退回「会动」判据（老逻辑）
	// ─────────────────────────────────────────────────────────────
	time.Sleep(MoveProbeWait)
	for i := range probes {
		pr := &probes[i]
		v2, ok2 := ReadPos(p, pr.cur)
		if !ok2 {
			continue
		}
		dx := float64(v2[0] - pr.v[0])
		dz := float64(v2[2] - pr.v[2])
		pr.dist = math.Hypot(dx, dz)
		pr.moved = pr.dist > 0.5 // 0.5m：挡住微抖对象（实测踩过）
	}

	if anchorValid {
		// 按离存档距离排序
		for i := range probes {
			probes[i].anchorDist = anchorDist(probes[i].v, anchor)
		}
		sort.SliceStable(probes, func(i, j int) bool {
			return probes[i].anchorDist < probes[j].anchorDist
		})

		// ① 最近的就在附近 → 直接锁（这是玩家，不用管它动不动）
		if probes[0].anchorDist <= AnchorNearDist {
			return probes[0].cur, nil
		}
		// ② 最近的也远 → 玩家大概传送了。在「会动」的里挑离存档最近的
		best := -1
		bestDist := math.Inf(1)
		for i, pr := range probes {
			if !pr.moved {
				continue
			}
			if pr.anchorDist < bestDist {
				bestDist = pr.anchorDist
				best = i
			}
		}
		if best >= 0 {
			return probes[best].cur, nil
		}
		// ③ 会动的也没有 → 取离存档最近的（玩家站着不动 + 传送了）
		if probes[0].anchorDist < 5000 {
			return probes[0].cur, nil
		}
		return 0, fmt.Errorf("候选都离存档 %.0f 米以上且不会动，拒绝误定位", probes[0].anchorDist)
	}

	// 没存档参照 → 退回「会动」判据
	for _, pr := range probes {
		if pr.moved {
			return pr.cur, nil
		}
	}
	return 0, fmt.Errorf("找到 %d 个候选但都不会动（且无存档参照），拒绝误定位", len(probes))
}

// AnchorNearDist 离存档多近算「就是玩家」。
// 玩家存档后正常走动几十米；超过这个数说明是别的东西。
const AnchorNearDist = 800.0

// anchorDist 到存档锚点的水平距离（米）
func anchorDist(v, anchor [3]float32) float64 {
	dx := float64(v[0] - anchor[0])
	dz := float64(v[2] - anchor[2])
	return math.Hypot(dx, dz)
}

// MoveProbeWait 判断「槽会不会变」时的等待间隔。
var MoveProbeWait = 1500 * time.Millisecond

// staticCandidates 静态候选（坐标合法但不变）。
var staticCandidates []uintptr

// nearAnchor 是否离存档锚点足够近。
func nearAnchor(v [3]float32, anchor [3]float32) bool {
	dx := float64(v[0] - anchor[0])
	dz := float64(v[2] - anchor[2])
	return math.Hypot(dx, dz) < 3000
}

// ReadPos 读一个位置（宿主地址处的 12 字节）。
func ReadPos(p *winmem.Process, addr uintptr) ([3]float32, bool) {
	buf, err := p.ReadTimeout(addr, PosSize, 500*time.Millisecond)
	if err != nil || len(buf) < PosSize {
		return [3]float32{}, false
	}
	return readF3(buf), true
}

// readF3 读 3 个 float。
func readF3(b []byte) [3]float32 {
	var v [3]float32
	for i := 0; i < 3 && (i+1)*4 <= len(b); i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// hasQuat 元素的 +0x0C 处是否是朝向四元数（决定性指纹）。
func hasQuat(b []byte) bool {
	if len(b) < 0x1C {
		return false
	}
	w := math.Float32frombits(binary.LittleEndian.Uint32(b[0x0C:]))
	x := math.Float32frombits(binary.LittleEndian.Uint32(b[0x10:]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(b[0x14:]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(b[0x18:]))
	for _, f := range []float32{w, x, y, z} {
		v := float64(f)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if w <= quatWMin || w > quatWMax {
		return false
	}
	if x < -quatXY || x > quatXY || y < -quatXY || y > quatXY {
		return false
	}
	if z < -quatAng || z > quatAng {
		return false
	}
	return true
}

// worldCoordOK 是否像世界坐标（海拉鲁）。
func worldCoordOK(x, alt, z float32) bool {
	for _, f := range []float32{x, alt, z} {
		v := float64(f)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if math.Abs(float64(x)) > 8000 || math.Abs(float64(z)) > 8000 {
		return false
	}
	if alt < -600 || alt > 6000 {
		return false
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	// ★ X 和 Z 不能同时为 0 ——
	//   实测踩过的坑：某些「位置表」的元素是 (0, 0, 720.8) 这种退化坐标，
	//   X/Z 都在轴上（世界坐标不会正好落在坐标轴上）。
	if x == 0 && z == 0 {
		return false
	}
	// ★★ 排除世界原点附近（|X|<10 且 |Z|<10）——
	//    实测踩过的坑：锁到 (1.04, 0.26, 0.55) 这种常量/单位向量对象。
	//    海拉鲁世界坐标范围约 ±7000，原点 (0,0) 在海外，
	//    玩家正常游玩**不可能**站在原点 10 米内。这是一条干净的物理判据。
	if math.Abs(float64(x)) < 10 && math.Abs(float64(z)) < 10 {
		return false
	}
	// 排除整数三元组（参数表的特征）
	if isIntegral(x) && isIntegral(z) {
		return false
	}
	return true
}

func isIntegral(f float32) bool {
	v := float64(f)
	return math.Abs(v-math.Round(v)) < 0.001
}

// runChunksLimited 分块读 + 限速（保护模拟器不被拖死）。
func runChunksLimited(p *winmem.Process, regions []winmem.Region, workers int, delay time.Duration, fn func(addr uintptr, buf []byte)) {
	const chunk = 8 << 20
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += uintptr(chunk) {
			n := chunk
			if remain := int(r.Size - off); n > remain {
				n = remain
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}
	if len(jobs) == 0 {
		return
	}
	ch := make(chan job, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				time.Sleep(delay)
				if err != nil || len(buf) < 16 {
					continue
				}
				fn(j.addr, buf)
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

// Save 落盘（记录 elem1 地址，供下次启动直接用）。
func Save(path string, elem1 uintptr, pos [3]float32) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "elem1=0x%X pos=%.3f,%.3f,%.3f at=%s\n",
		elem1, pos[0], pos[1], pos[2], time.Now().Format(time.RFC3339))
	return nil
}
