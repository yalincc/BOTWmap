// Package locate = Enavi 的定位核心。
//
// ★ 设计哲学（2026-10-20 老大定调：不要再有一堆小 bug）：
//
//	「宁可少功能，也不要多状态。」
//
// xnavi 的 18 项问题里，绝大多数不是"算法不聪明"，而是"状态机太复杂"：
// 探针池、半死槽快速换锁、候选分级、乒乓淘汰、失败组登记……每多一个机制，
// 就多一类互相干扰的 bug。Enavi 这里只保留三件事：
//
//	1. 锁一个地址，然后死读它（10Hz，每次 12 字节）；
//	2. 只有三种「明确失效」才重扫：连续读失败 / 单拍跳变 >300m / 进神庙本地尺度；
//	3. 重扫时以「最后已知位置」为中心开窄窗，不搞候选排名、不搞试探换锁。
//
// 地址来源两条路，优先第一条：
//   - known 缓存（同一次 Eden 会话内有效）→ 0.2 秒锁定
//   - 位置历史缓冲定位（poshist.Scan）→ 2~8 秒（冷启动 / 缓存失效时）
//
// ★ 2026-10-20 起改用「位置历史缓冲」而不是「存档锚点 + 窄窗扫描」：
// 位置历史缓冲是 BOTW 运行时自己的玩家位置数组（poshist 包），带朝向四元数指纹，
// 在 12GB 内存里唯一。好处：
//   - 不依赖存档（玩家没存档也能定位）
//   - 坐标是**实时**的，不是存档里的旧值
//   - 传送后立即正确（缓冲里就是当前帧）
//   - 玩家静止时保留最后位置，不会归零
package locate

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"botwmap/enavi/internal/known"
	"botwmap/enavi/internal/poshist"
	"botwmap/enavi/internal/saveanchor"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

// 阈值（集中在这里，改判据只改一处）。
const (
	teleportDist = 300.0 // 单拍跳变 = 传送
	maxTickJump  = 150.0 // 单拍位移上限（防野指针）
	winNear      = 120.0 // 窄窗半径（米）
	winFar       = 400.0 // 兜底窗口
	readFailMax  = 5     // 连续读失败几次判定失效
	moveDist     = 0.5   // 视为"动了"的门槛
	moveConfirmN = 3     // 连续几拍动 → 已验证
)

// Update 一次采样结果。
type Update struct {
	OK       bool
	Verified bool
	Pos      scan.Vec3
	Addr     uintptr
	Source   string // known | scan | player | hold | shrine | lost
	Note     string
}

// Tracker 玩家位置跟踪器。
type candSlot struct {
	addr uintptr
	init scan.Vec3
}

type Tracker struct {
	proc    *winmem.Process
	regions []winmem.Region
	anchor  scan.Vec3 // 存档锚点（冷启动用）
	pid     uint32
	started int64 // Eden 进程启动时间

	mu       sync.RWMutex
	cur      scan.Vec3
	addr     uintptr
	ok       bool
	verified bool
	moveN    int
	fails    int
	shrine   bool // 是否在神庙内（挂起跟随）
	cands    []candSlot // 待验证备选槽（用于驿站等多 NPC 场景走动仲裁）

	scanBusy int32
	scans    int
	saves    int
}

// Stats 诊断信息。
type Stats struct {
	Addr   uintptr
	Scans  int
	Saves  int
	Busy   bool
	Anchor scan.Vec3
}

// New 建立跟踪器。顺序：known 缓存 → 存档锚点窄窗扫描。
func New(p *winmem.Process) (*Tracker, error) {
	d, _, err := p.FindDram()
	if err != nil {
		return nil, err
	}
	regions := scan.HeapRegions(p, d, 64<<10)
	if len(regions) == 0 {
		return nil, errors.New("User Heap 为空：Eden 未载入游戏")
	}
	t := &Tracker{
		proc:    p,
		regions: regions,
		pid:     p.PID,
		started: p.StartedAt(),
	}

	// 锚点（存档）——冷启动扫描中心，也是 known 的交叉校验
	if anchors, _ := saveanchor.Find(p.ExePath()); len(anchors) > 0 {
		t.anchor = scan.Vec3{X: anchors[0].Pos[0], Y: anchors[0].Pos[1], Z: anchors[0].Pos[2]}
	}

	// ① known 缓存：同一 Eden 会话内，直接读，不进扫描
	if e, ok := known.Load().Pick(t.pid, t.started); ok && e.Verified {
		if buf, err := p.Read(e.Addr, 64); err == nil && len(buf) >= 60 {
			if pos, _, okRot := scan.TransformLoose(buf, 0); okRot {
				anchorOK := !t.anchor.Valid() || pos.Dist2D(t.anchor) < 800
				if anchorOK {
					t.addr = e.Addr
					t.cur = pos
					t.ok = true
					t.verified = true
					return t, nil
				}
			}
		}
	}

	// ② 存档锚点窗口扫描（以存档为中心，在近场找带 Actor 旋转矩阵的真实玩家坐标槽）
	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) == 0 {
		return nil, errors.New("未找到可用存档锚点，请在游戏内按 + 存个档（或等待自动存档）")
	}
	found := false
	for i := 0; i < len(anchors) && i < 3; i++ {
		anc := scan.Vec3{X: anchors[i].Pos[0], Y: anchors[i].Pos[1], Z: anchors[i].Pos[2]}
		if t.rescan(anc, winNear) {
			t.anchor = anc
			found = true
			break
		}
	}
	if !found {
		if t.rescan(t.anchor, winFar) {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("存档锚点 (%.1f, %.1f, %.1f) 窗口内未找到活跃玩家 Actor（请确认已载入游戏世界，若仍在加载画面请稍候）", t.anchor.X, t.anchor.Y, t.anchor.Z)
	}
	return t, nil
}

// Anchor 存档锚点（日志用）。
func (t *Tracker) Anchor() scan.Vec3 { return t.anchor }

// Tick 采样一次（调用方按 100ms 驱动）。
func (t *Tracker) Tick() Update {
	t.mu.RLock()
	addr, cur, okCur := t.addr, t.cur, t.ok
	verified, shrine := t.verified, t.shrine
	t.mu.RUnlock()

	if addr == 0 {
		t.rescanAsync(cur, winNear)
		return Update{OK: false, Source: "lost"}
	}

	pv, valid := poshist.ReadPos(t.proc, addr)
	v := scan.Vec3{X: pv[0], Y: pv[1], Z: pv[2]}
	if !valid {
		t.mu.Lock()
		t.fails++
		f := t.fails
		if f >= readFailMax {
			t.ok = false
			t.addr = 0 // 明确失效 → 允许重扫
		}
		t.mu.Unlock()
		if f >= readFailMax {
			t.rescanAsync(scan.Vec3{}, winFar) // 传空坐标，自动检查并跳转到最新传送存档点
			return Update{OK: false, Source: "lost", Note: "连续读失败，重扫中"}
		}
		// 单次失败多半是瞬态（加载中页保护切换），保持上一位置
		return Update{OK: okCur, Verified: verified, Pos: cur, Addr: addr, Source: "hold"}
	}

	// 多候选走动仲裁（防驿站等多 NPC 场景误锁静止 NPC）
	t.mu.RLock()
	cands := t.cands
	t.mu.RUnlock()
	if !verified && len(cands) > 1 {
		for _, c := range cands {
			if c.addr == addr {
				continue
			}
			pCand, okCand := poshist.ReadPos(t.proc, c.addr)
			if okCand {
				cPos := scan.Vec3{X: pCand[0], Y: pCand[1], Z: pCand[2]}
				dCand := cPos.Dist3D(c.init)
				if dCand >= moveDist && dCand < maxTickJump {
					t.mu.Lock()
					t.addr = c.addr
					t.cur = cPos
					t.verified = true
					t.moveN = moveConfirmN
					t.cands = nil
					t.persist(cPos, true)
					t.mu.Unlock()
					return Update{OK: true, Verified: true, Pos: cPos, Addr: c.addr, Source: "player", Note: "走动仲裁识别为真玩家"}
				}
			}
		}
	}

	// 神庙内部试炼场本地尺度判定（必须 |X|<100, |Z|<100 且高度 Y < 50）
	// 注意：中央平原之塔等大世界坐标也在原点附近（但高度 Y > 100m），不可误判为神庙！
	isShrineLocal := math.Abs(float64(v.X)) < 100 && math.Abs(float64(v.Z)) < 100 && v.Y < 50
	if shrine {
		if !isShrineLocal {
			// 出庙或传送：立即解除神庙挂起状态
			t.mu.Lock()
			t.shrine = false
			t.mu.Unlock()
		} else {
			return Update{OK: false, Pos: cur, Addr: addr, Source: "shrine", Note: "神庙内试炼场（本地坐标），红点钉在门口"}
		}
	} else if isShrineLocal && !cur.InShrine() && v.Dist3D(cur) > 100 {
		t.mu.Lock()
		t.shrine = true
		t.ok = false
		t.mu.Unlock()
		return Update{OK: false, Pos: cur, Addr: addr, Source: "shrine", Note: "神庙内试炼场（本地坐标），红点钉在门口"}
	}

	d := v.Dist3D(cur)

	// 传送：单拍 >300m
	if d > teleportDist {
		t.mu.Lock()
		t.cur = v
		t.verified = false
		t.moveN = 0
		t.fails = 0
		t.shrine = false // 传送必定解除神庙状态
		t.mu.Unlock()
		t.rescanAsync(v, winNear) // 围着落点重扫
		return Update{OK: true, Pos: v, Addr: addr, Source: "scan", Note: "传送跳变，重扫中"}
	}

	// 单拍位移异常大（野指针/锁错）
	if d > maxTickJump {
		t.mu.Lock()
		t.fails++
		t.mu.Unlock()
		return Update{OK: okCur, Verified: verified, Pos: cur, Addr: addr, Source: "hold", Note: "单拍位移过大，已忽略"}
	}

	// 正常跟随
	t.mu.Lock()
	t.cur = v
	t.ok = true
	t.fails = 0
	if d >= moveDist {
		t.moveN++
		if t.moveN >= moveConfirmN && !t.verified {
			t.verified = true
			t.persist(v, true) // 升级为已验证（不每拍写盘）
		}
	}
	upd := Update{OK: true, Verified: t.verified, Pos: v, Addr: addr, Source: "player"}
	t.mu.Unlock()
	return upd
}

// persist 把当前地址写进 known 缓存。
// verified=false 表示"锁上了但还没被走动验证过"，仍然值得缓存（下次直接读，省一次扫描）。
func (t *Tracker) persist(v scan.Vec3, verified bool) {
	store := known.Load()
	_ = store.Save(known.Entry{
		Addr:        t.addr,
		PID:         t.pid,
		ProcStarted: t.started,
		Pos:         [3]float32{v.X, v.Y, v.Z},
		Verified:    verified,
	})
	t.saves++
}

// Rescan 手动重扫。
func (t *Tracker) Rescan() {
	t.mu.Lock()
	t.shrine = false
	if anchors, _ := saveanchor.Find(t.proc.ExePath()); len(anchors) > 0 {
		t.anchor = scan.Vec3{X: anchors[0].Pos[0], Y: anchors[0].Pos[1], Z: anchors[0].Pos[2]}
	}
	t.mu.Unlock()
	t.rescanAsync(scan.Vec3{}, winFar)
}

// LeaveShrine 离开神庙（外部可调；出庙时清挂起标记）。
func (t *Tracker) LeaveShrine() {
	t.mu.Lock()
	t.shrine = false
	t.mu.Unlock()
}

// Stats 诊断。
func (t *Tracker) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Stats{
		Addr:   t.addr,
		Scans:  t.scans,
		Saves:  t.saves,
		Busy:   atomic.LoadInt32(&t.scanBusy) == 1,
		Anchor: t.anchor,
	}
}

func (t *Tracker) rescanAsync(center scan.Vec3, radius float64) {
	t.rescanLegacy(center, radius)
}

// rescanPosHist 重新定位位置历史缓冲。
func (t *Tracker) rescanPosHist() {
	if !atomic.CompareAndSwapInt32(&t.scanBusy, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&t.scanBusy, 0)
		var anchor [3]float32
		anchorValid := t.anchor.Valid()
		if anchorValid {
			anchor = [3]float32{t.anchor.X, t.anchor.Y, t.anchor.Z}
		}
		addr, err := poshist.ScanWithAnchor(t.proc, anchor, anchorValid)
		if err != nil {
			return
		}
		v, ok := poshist.ReadPos(t.proc, addr)
		if !ok {
			return
		}
		t.mu.Lock()
		t.addr = addr
		t.cur = scan.Vec3{X: v[0], Y: v[1], Z: v[2]}
		t.ok = true
		t.verified = true
		t.fails = 0
		t.moveN = 0
		t.mu.Unlock()
		t.scans++
		t.persist(scan.Vec3{X: v[0], Y: v[1], Z: v[2]}, true)
	}()
}

func (t *Tracker) rescanLegacy(center scan.Vec3, radius float64) {
	if !atomic.CompareAndSwapInt32(&t.scanBusy, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&t.scanBusy, 0)
		// 检查磁盘是否有最新的传送存档（BOTW 传送落地必自动存档）
		if anchors, _ := saveanchor.Find(t.proc.ExePath()); len(anchors) > 0 {
			latest := scan.Vec3{X: anchors[0].Pos[0], Y: anchors[0].Pos[1], Z: anchors[0].Pos[2]}
			if !center.Valid() || latest.Dist2D(center) > 200 {
				center = latest
				t.mu.Lock()
				t.anchor = latest
				t.mu.Unlock()
			}
		}
		t.rescan(center, radius)
	}()
}

// rescan 在 center 周围找坐标槽。排序：有旋转矩阵 > 非历史副本 > 距 center 近。
// 不做"候选池/试探换锁"：一次定一个，定了就死读。
func (t *Tracker) rescan(center scan.Vec3, radius float64) bool {
	if !center.Valid() {
		center = t.anchor
	}
	opt := scan.Options{Anchor: &center, Radius: float32(radius)}

	type cand struct {
		addr  uintptr
		p     scan.Vec3
		score float64
	}
	var best cand
	found := false
	var allCands []cand
	var mu sync.Mutex

	const chunk = 8 << 20
	const overlap = 256
	workers := 2
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range t.regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += uintptr(chunk) {
			n := chunk
			if remain := int(r.Size - off); remain < n {
				n = remain
			}
			if uintptr(n) < r.Size-off {
				n += overlap
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}
	if len(jobs) == 0 {
		return false
	}

	ch := make(chan job, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := t.proc.Read(j.addr, j.n)
				time.Sleep(8 * time.Millisecond) // 限速，防拖死/卡顿模拟器
				if err != nil || len(buf) < 12 {
					continue
				}
				hits := scan.FindTriples(buf, j.addr, opt)
				if len(hits) == 0 {
					continue
				}
				mu.Lock()
				for _, h := range hits {
					// ★ 核心铁律：必须有 Actor 3x3 正交旋转矩阵！
					// 静态存档数据、无 Actor 变换的静态结构一律跳过，绝不锁死在静态槽上
					if !h.Rot {
						continue
					}
					score := 1000.0 - h.Dist
					allCands = append(allCands, cand{addr: h.Addr, p: h.P, score: score})
					if !found || score > best.score {
						best = cand{addr: h.Addr, p: h.P, score: score}
						found = true
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	if !found {
		return false
	}
	sort.Slice(allCands, func(i, j int) bool {
		return allCands[i].score > allCands[j].score
	})
	var slots []candSlot
	for i := 0; i < len(allCands) && i < 5; i++ {
		slots = append(slots, candSlot{addr: allCands[i].addr, init: allCands[i].p})
	}

	t.mu.Lock()
	t.addr = best.addr
	t.cur = best.p
	t.ok = true
	t.verified = false
	t.moveN = 0
	t.fails = 0
	t.shrine = false
	t.scans++
	t.cands = slots
	t.mu.Unlock()
	return true
}

// Dist2D 两个点水平距离。
func Dist2D(a, b scan.Vec3) float64 {
	dx, dz := float64(a.X-b.X), float64(a.Z-b.Z)
	return math.Hypot(dx, dz)
}

// 让 time 包被用到（软 TTL 判断在 known 包内，这里保留以方便后续扩展）。
var _ = time.Now
