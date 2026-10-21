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
//   - 存档锚点窄窗扫描 → 2~3 秒（冷启动 / 缓存失效时）
//
// ★★ 定位源 = 林克的 3D 主变换（sead::Matrix34f）：
//   +0x00 = 坐标 (X, alt, Z)，+0x0C 起 = 3x3 正交旋转矩阵。
//   「有 3x3 正交旋转矩阵」是**活跃 3D Actor 的充要特征** ——
//   静态存档内存缓冲没有这个矩阵，硬拦掉它就绝不会锁死在退出点。
//
// ★ 曾经的「位置历史缓冲」路线（poshist 包，0x28 步长 + 四元数指纹）已证伪：
//   2026-10-21 实测 —— +0x28/+0x50 是「60~90 秒推进一次的历史采样循环展示缓冲」，
//   与玩家当前动作解耦，不是实时位置源。
//   指纹判据本身也错：w = cos(θ/2)，玩家一转身 w 就离开 (0.9, 1.0]，
//   正确判据是「四元数单位模长」（朝向无关）。
//   详见 docs/spec/ENAVI-HANDBOOK.md §6。
package locate

import (
	"errors"
	"fmt"
	"math"
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
	// 神庙本地尺度判据（§3.5 物理特征鉴别表）：
	//   地下神庙试炼场 = |X|<100 && |Z|<100 && **Y < 50**
	//   平原之塔 X≈-215 Z≈250 → Z 超出 100，Y≈250 也超 50 → 不误判
	//   中央平原地面遗迹 Y≈100~130 → Y 超 50 → 不误判
	shrineXY    = 100.0 // 神庙本地坐标的 X/Z 绝对值上限（米）
	shrineMaxY  = 50.0  // 神庙本地坐标的海拔上限（米）
	saveFreshS  = 120   // 磁盘存档"新鲜"判定：mtime 距今秒数（自动存档落地后约 1~2 秒写入）
)

// isShrineScale 是否为「神庙试炼场本地尺度」坐标。
//
// ★ 为什么不用 scan.InShrine()（阈值 300）：
//   300 是「是否世界尺度」的粗判据（live-cemu / xnavi 用它区分庙内庙外），
//   但用它做「是不是神庙」的判定会把中央海拉鲁平原上的平原之塔、地面遗迹
//   一并误判成神庙（它们的坐标也在原点附近）。真正的物理区分是**海拔**：
//   神庙在地下（Y < 50），大世界地表在 Y > 100。
func isShrineScale(v scan.Vec3) bool {
	return math.Abs(float64(v.X)) < shrineXY &&
		math.Abs(float64(v.Z)) < shrineXY &&
		float64(v.Y) < shrineMaxY
}

// Update 一次采样结果。
type Update struct {
	OK       bool
	Verified bool
	Pos      scan.Vec3
	Addr     uintptr
	Source   string // known | scan | player | hold | shrine | lost
	Note     string
	Shrine   bool // 是否处于神庙挂起状态（红点钉在门口）
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

	// 锚点（存档）——冷启动扫描中心，也是 known 的交叉校验。
	// ★ 用 saveanchor.Best 挑「有进度且最新」的那一档，排除空档/新档。
	anchors, _ := saveanchor.Find(p.ExePath())
	best := saveanchor.Best(anchors)
	if best != nil {
		t.anchor = scan.Vec3{X: best.Pos[0], Y: best.Pos[1], Z: best.Pos[2]}
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
	if len(anchors) == 0 {
		return nil, errors.New("未找到可用存档锚点，请在游戏内按 + 存个档（或等待自动存档）")
	}
	// 候选中心：优先「有进度」的那一档；再退而用其余槽（最多试 3 个），
	// 万一玩家正在玩的是另一个档（罕见但存在）。
	centers := []scan.Vec3{}
	if best != nil {
		centers = append(centers, scan.Vec3{X: best.Pos[0], Y: best.Pos[1], Z: best.Pos[2]})
	}
	for _, a := range anchors {
		if best != nil && a.Path == best.Path {
			continue
		}
		centers = append(centers, scan.Vec3{X: a.Pos[0], Y: a.Pos[1], Z: a.Pos[2]})
	}
	found := false
	for i := 0; i < len(centers) && i < 3; i++ {
		if t.rescan(centers[i], winNear) {
			t.anchor = centers[i]
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
func (t *Tracker) Anchor() scan.Vec3 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.anchor
}

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
	isShrineLocal := isShrineScale(v)
	if shrine {
		if !isShrineLocal {
			// 出庙（or 传送出庙）：解除神庙挂起。
			// ★ 关键：出庙这一拍坐标从「庙内本地尺度」跳回「世界尺度」，
			//   跳变幅度天然超过 teleportDist —— 但它不是传送，是正常出庙。
			//   所以这里直接接受新坐标并就地更新 t.cur，**不再走传送重扫分支**，
			//   否则每出一次庙就要白白全堆扫 2~3 秒。
			t.mu.Lock()
			t.shrine = false
			t.cur = v
			t.ok = true
			t.fails = 0
			t.mu.Unlock()
			return Update{OK: true, Verified: verified, Pos: v, Addr: addr, Source: "player", Note: "已离开神庙，恢复跟随"}
		}
		// 还在庙里：红点钉在入口（cur 保持进入庙前的世界坐标，天然就是门口）。
		// ★ ok 必须为 true —— 前端 live.js 用 `p.ok` 判「服务在线」，
		//   ok=false 会被判成离线 → 连续 3 次后停止轮询 → 红点直接消失（2026-10-21 实测事故）。
		//   对照兄弟项目 live-cemu：神庙模式下同样是 OK=true / Verified=true。
		return Update{OK: true, Verified: true, Pos: cur, Addr: addr, Source: "shrine", Shrine: true, Note: "神庙内试炼场（本地坐标），红点钉在门口"}
	} else if isShrineLocal && !isShrineScale(cur) && v.Dist3D(cur) > 100 {
		// 进庙：世界尺度 → 神庙本地尺度。位置钉在进入前的 cur（= 门口）。
		t.mu.Lock()
		t.shrine = true
		t.ok = true
		t.mu.Unlock()
		return Update{OK: true, Verified: true, Pos: cur, Addr: addr, Source: "shrine", Shrine: true, Note: "神庙内试炼场（本地坐标），红点钉在门口"}
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
		t.cands = nil   // ★ 旧候选池跨传送残留 → 仲裁可能选到千里外的旧 Actor
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
//
// ★ 保留当前中心（t.cur），**不**把 anchor 重置为存档点：
//   玩家可能已经传送了几公里，存档点还在老地方 —— 用它当扫描中心会扫不到人。
//   存档锚点只在「冷启动、还不知道玩家在哪」时才有意义。
func (t *Tracker) Rescan() {
	t.mu.Lock()
	t.shrine = false
	center := t.cur
	t.mu.Unlock()
	t.rescanAsync(center, winFar)
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

// rescanLegacy 执行一次重扫（存档锚点窗口扫描）。
//
// ★ 中心点的选取规则（2026-10-21 修正）：
//
//	传入的 center 是「我们已经知道的玩家位置」，可信度分两种：
//	  · 冷启动时 center 无效 → 只能靠磁盘存档锚点当中心；
//	  · 传送/失效重扫时 center = 真实落点 → 比存档可信。
//
//	旧逻辑只要「存档点距 center > 200m」就无条件用存档点覆盖 center，
//	结果传送后扫的是老存档位置（玩家在新地方）→ 白扫 2~3 秒还锁不上。
//
//	现在的规则：**只有磁盘存档是"新鲜"的（自动存档刚写过，≤120 秒）才用它当中心**。
//	BOTW 每次传送落地必定触发一次自动存档，所以新鲜存档 ≈ 玩家刚落地的位置；
//	而老存档（几小时前）完全可能是玩家早已离开的地方，不该当中心。
func (t *Tracker) rescanLegacy(center scan.Vec3, radius float64) {
	if !atomic.CompareAndSwapInt32(&t.scanBusy, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&t.scanBusy, 0)

		// 找磁盘存档：优先"有进度且最新"的槽（排除空档/新档）。
		anchors, _ := saveanchor.Find(t.proc.ExePath())
		if len(anchors) > 0 {
			if best := saveanchor.Best(anchors); best != nil {
				latest := scan.Vec3{X: best.Pos[0], Y: best.Pos[1], Z: best.Pos[2]}
				fresh := time.Since(best.MTime) <= saveFreshS*time.Second
				switch {
				case !center.Valid():
					// 冷启动：没有中心 → 只能拿存档锚点当扫描中心。
					center = latest
					t.mu.Lock()
					t.anchor = latest
					t.mu.Unlock()
				case fresh && latest.Dist2D(center) > 200:
					// 存档是新鲜的（刚落地的自动存档）→ 相信它。
					center = latest
					t.mu.Lock()
					t.anchor = latest
					t.mu.Unlock()
				default:
					// 存档不新鲜 → 尊重传入的真实落点。
				}
			}
		}
		t.rescan(center, radius)
	}()
}

// rescan 在 center 周围找坐标槽。
//
// 排序口径（2026-10-21 修正）：走 scan.Group ——
//  1. 先按坐标（0.1m 取整）分组去重：同一实体在内存里有多个副本地址，
//     旧逻辑每个副本各占一个候选位 → 5 个候选槽被同一个实体的副本占满，
//     多候选走动仲裁彻底失效（驿站场景锁不到真林克）。
//  2. 组内按「有 3x3 正交旋转矩阵」优先（Actor 充要特征），
//  3. 再按「距 center 近」优先。
//
// ★ 核心铁律：必须有 Actor 3x3 正交旋转矩阵！
//   静态存档数据、无 Actor 变换的静态结构一律跳过，绝不锁死在静态槽上。
//
// 不做"候选池/试探换锁"：一次定一个，定了就死读。
func (t *Tracker) rescan(center scan.Vec3, radius float64) bool {
	if !center.Valid() {
		t.mu.RLock()
		center = t.anchor
		t.mu.RUnlock()
	}
	opt := scan.Options{Anchor: &center, Radius: float32(radius)}

	var allHits []scan.Hit
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
				allHits = append(allHits, hits...)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	// ★ 核心铁律：必须有 Actor 3x3 正交旋转矩阵！
	// 静态存档数据、无 Actor 变换的静态结构一律跳过，绝不锁死在静态槽上。
	rotHits := make([]scan.Hit, 0, len(allHits))
	for _, h := range allHits {
		if !h.Rot {
			continue
		}
		rotHits = append(rotHits, h)
	}
	if len(rotHits) == 0 {
		return false
	}

	// 坐标分组去重（有旋转矩阵优先 + 距中心近）。
	groups := scan.Group(rotHits)
	if len(groups) == 0 {
		return false
	}
	best := groups[0]

	var slots []candSlot
	for i := 0; i < len(groups) && i < 5; i++ {
		slots = append(slots, candSlot{addr: groups[i].Addr, init: groups[i].P})
	}

	t.mu.Lock()
	t.addr = best.Addr
	t.cur = best.P
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
