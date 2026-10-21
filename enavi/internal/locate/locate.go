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
	"encoding/binary"
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

// ---------- ★ Actor 存活判据（2026-10-21 实测事故修复） ----------

// 事故复盘（老大实机测试，2026-10-21 11:53）：
//
//	11:53:08  正常 (1662.5, 116.7, 1921.6) verified=true
//	11:53:10  (0.0, 0.0, 1921.6)      ← 玩家进神庙，Actor 局部坐标被清零
//	11:53:12  (0.0, NaN, 0.0)         ← NaN！
//	（此后 8 拍全是 NaN，但程序认为"读成功了"）
//	11:53:24  锁到 (107.7, 1.0, 0.0)  ← 重扫后锁到假槽（距离玩家 2485m！）
//
//	根因：**只读 12 字节坐标、不做任何校验**。
//	  · 坐标槽被游戏清零/写成 NaN 时，ReadProcessMemory 仍然"成功"（内存可读），
//	    旧代码只看 err == nil → 把 NaN 当成合法坐标继续用；
//	  · scan.Vec3.Valid() 确实能挡 NaN，但旧代码路径**根本没调用它**。
//
// 所以定位的每一步都必须过两道闸：
//  ① 坐标必须合法（无 NaN/Inf、在世界范围、非近零、非参数表整数三元组）；
//  ② 同一 Actor 的 3x3 正交旋转矩阵必须还在（= 这还是那个活跃的 3D Actor）。
//
// ② 的实测依据（2026-10-21 探针）：
//
//	+0x18 处严格 3x3 正交 = true 的候选：36/48
//	+0x14 处 = 0，+0x0C 处 = 0（★ 手册里写的 +0x0C 是错的！实际偏移不固定）
//
//	→ 旋转矩阵在 pos 之后的偏移**不固定**（+0x14/+0x18/+0x1C/+0x20/+0x24/+0x2C 都有），
//	  所以必须**按布局无关**的方式找（scan.RotOffsetIn 扫 +12..+44），
//	  找到就说明这个 Actor 活着；找不到 = 死了/不是 Actor → 立即失效重扫。

// readActor 读一个 Actor 的坐标 + 验证它还活着。
//
// 返回 (pos, alive, ok)：
//   - ok=false   → 连 12 字节都读不出来（真正的读失败）
//   - alive=false → 读出来了，但坐标非法（NaN/清零/越界）或旋转矩阵没了
//     → 这是「Actor 死了」或「锁错槽」，必须失效重扫
func (t *Tracker) readActor(addr uintptr) (pos scan.Vec3, alive bool, ok bool) {
	buf, err := t.proc.ReadTimeout(addr, 64, 500*time.Millisecond)
	if err != nil || len(buf) < 12 {
		return scan.Vec3{}, false, false
	}
	p := scan.Vec3{
		X: math.Float32frombits(binary.LittleEndian.Uint32(buf[0:])),
		Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
		Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
	}
	// 闸①：坐标必须合法（NaN / Inf / 越界 / 近零 / 轴上退化 全挡掉）
	if !p.WorldCoordOK() {
		return p, false, true
	}
	// 闸②：3x3 正交旋转矩阵必须还在（Actor 活跃的充要特征）
	if len(buf) >= 60 {
		if _, _, hasRot := scan.TransformLoose(buf, 0); !hasRot {
			return p, false, true
		}
	}
	return p, true, true
}

// Logger 定位核心的日志出口（由 cmd/enavi 注入，落到 enavi.log）。
//
// 为什么不在包里直接用 log.Printf：核心的日志要同时进 enavi.log 和 stdout，
// 且格式带毫秒时间戳 —— 统一由上层注入，核心只管调。
var Logger func(format string, a ...any)

// logf 有 logger 就打，没有就静默（单测/探针复用本包时不会炸）。
func logf(format string, a ...any) {
	if Logger != nil {
		Logger(format, a...)
	}
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
	//
	// ★ 门禁比旧版严得多（2026-10-21 实测事故修复）：
	//   旧版只看「坐标合法 + 附近有 3x3 矩阵 + 距存档 <800m」，
	//   结果 known 里存了一条**死地址**（玩家进神庙后那个 Actor 被释放），
	//   而那块内存被引擎复用成了别的东西 → 秒锁到一个假坐标 → 之后再也定位不上。
	//   现在必须同时满足：
	//     ① 坐标通过 WorldCoordOK（挡 NaN/退化坐标/参数表）
	//     ② 3x3 正交旋转矩阵还在（= 这还是那个活跃 Actor）
	//     ③ 距存档锚点 < 800m
	//   任一不过 → 立即作废，走全堆扫描。
	if e, ok := known.Load().Pick(t.pid, t.started); ok && e.Verified {
		if buf, err := p.Read(e.Addr, 64); err == nil && len(buf) >= 60 {
			if pos, _, okRot := scan.TransformLoose(buf, 0); okRot && pos.WorldCoordOK() {
				anchorOK := !t.anchor.Valid() || pos.Dist2D(t.anchor) < 800
				if anchorOK {
					t.addr = e.Addr
					t.cur = pos
					t.ok = true
					t.verified = true
					logf("[locate] known 缓存命中（秒锁）：%#x pos=(%.1f, %.1f, %.1f)\n",
						e.Addr, pos.X, pos.Y, pos.Z)
					return t, nil
				}
			}
		}
		logf("[locate] known 缓存已失效（地址死了或坐标非法），改走扫描\n")
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
		// 还没锁上（或刚失效）。用最后已知位置当重扫中心；
		// 中心无效时 rescanLegacy 会回落到存档锚点，绝不会扫世界原点。
		t.rescanAsync(cur, winNear)
		return Update{OK: false, Source: "lost"}
	}

	// ★ 读 Actor（坐标 + 存活校验）—— 不再裸读 12 字节。
	//   Actor 死了（进神庙/传送/场景切换）时内存仍然可读，会读出 NaN 或清零，
	//   旧代码把它当合法坐标 → 红点乱飞、甚至锁到 2 公里外的假槽。
	v, alive, readOK := t.readActor(addr)
	if !readOK {
		// 真正的读失败（进程死了/页不可读）
		t.mu.Lock()
		t.fails++
		f := t.fails
		if f >= readFailMax {
			t.ok = false
			t.addr = 0 // 明确失效 → 允许重扫
		}
		t.mu.Unlock()
		if f >= readFailMax {
			// ★ 用「最后已知位置」当重扫中心，不要传空坐标。
			//   传空坐标 → 扫描退化成"扫世界原点周围"（实测 3057 万命中，
			//   排最前的全是原点附近的退化坐标）→ 精确地锁到垃圾槽。
			t.rescanAsync(cur, winFar)
			return Update{OK: false, Source: "lost", Note: "连续读失败，重扫中"}
		}
		// 单次失败多半是瞬态（加载中页保护切换），保持上一位置
		return Update{OK: okCur, Verified: verified, Pos: cur, Addr: addr, Source: "hold"}
	}
	if !alive {
		// ★ Actor 已死（坐标非法 或 3x3 旋转矩阵消失）→ 立刻失效重扫。
		//   不能像以前那样继续用这个地址 —— 那会一路 NaN 下去，
		//   甚至重扫时锁到 2 公里外的假槽。
		t.mu.Lock()
		t.fails = readFailMax // 直接判定失效，不再等 5 拍
		t.ok = false
		t.addr = 0
		t.verified = false
		t.cands = nil
		oldPos := t.cur
		t.mu.Unlock()
		logf("[locate] Actor 已失效（坐标非法或旋转矩阵消失）：旧位置 (%.1f, %.1f, %.1f) → 重扫中\n",
			oldPos.X, oldPos.Y, oldPos.Z)
		t.rescanAsync(oldPos, winNear) // 围着最后已知位置重扫
		return Update{OK: false, Pos: oldPos, Addr: addr, Source: "lost", Note: "Actor 已失效，重新定位中"}
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
//
// ★ verified 的含义（2026-10-21 修正）：
//
//	verified=true  = 已经走动确认过「这是真玩家」，复用时最可信；
//	verified=false = 只是「锁上了某个带 3x3 矩阵的 Actor」，还没走动证伪过。
//
//	两种都写盘（省一次扫描），但复用门禁不同：
//	· known.Pick 只接受 verified=true（避免把可能的 NPC 地址固化）；
//	· 所以 verified=false 的条目**只当参考**，不会秒锁。
//
//	★ 为什么站立不动也要写：
//	玩家站着不动 → 永远等不到"走动 3 拍"→ 永远 verified=false →
//	如果只在 verified 时写盘，那"站着不动"就永远不写盘 →
//	每次重启都要重扫 3~8 秒。写一条 verified=false 的参考，
//	下次启动至少能拿它做交叉校验（读出来坐标对得上就说明地址还活着）。
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

		// ★★ 中心兜底：绝不能拿「无效坐标」去扫。
		//   空中心 = (0,0,0) → 扫描窗口变成"世界原点周围 400m"，
		//   实测会捞到 **3057 万** 个命中、12.7 万个带 rot 的，
		//   而排在最前面的全是原点附近的退化坐标 (0,±0.1,0) / (0,0,±0.1) ——
		//   引擎参数表的一大片。旧代码按"离中心近"排序，会**精确地**选中这些垃圾，
		//   于是红点飞到世界原点（用户看到的就是"锁到 2 公里外的假槽"）。
		//
		//   现在：中心无效 → 用存档锚点；存档也没有 → 明确失败并报错，
		//   绝不静默扫原点。
		if !center.Valid() || !center.WorldCoordOK() {
			t.mu.RLock()
			a := t.anchor
			t.mu.RUnlock()
			if a.Valid() && a.WorldCoordOK() {
				center = a
			} else {
				logf("[locate] 重扫失败：没有可用的扫描中心（存档锚点与最后位置均无效）\n")
				return
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
	// ★ 中心必须是一个真实的玩家位置。
	//   空中心 (0,0,0) 会让扫描退化成"扫世界原点周围" —— 实测 3057 万命中，
	//   排在最前的全是原点附近的退化坐标。宁可扫不到，也不能锁到垃圾。
	if !center.Valid() || !center.WorldCoordOK() {
		t.mu.RLock()
		a := t.anchor
		t.mu.RUnlock()
		if !a.Valid() || !a.WorldCoordOK() {
			logf("[locate] 放弃扫描：中心与存档锚点都不是有效世界坐标\n")
			return false
		}
		center = a
	}
	opt := scan.Options{Anchor: &center, Radius: float32(radius)}

	var allHits []scan.Hit
	var mu sync.Mutex

	const chunk = 8 << 20
	const overlap = 256
	// 并发工人 4（原 2）：实测 Eden 读 2.7GB User Heap 时 2 个工人要 8 秒，
	// 4 个工人降到 4 秒左右，CPU 占用仍 < 8%，模拟器不掉帧。
	// 限速改成「每工人 4ms」而不是「每块 8ms」—— 原来的写法让 757 个分块
	// 串行累积出 6 秒纯 sleep，是定位慢的主因。
	workers := 4
	const sleepPerChunk = 4 * time.Millisecond
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
				time.Sleep(sleepPerChunk) // 限速，防拖死/卡顿模拟器
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
	//
	// ★★ 再加一道：坐标必须通过 WorldCoordOK（严于 Valid）——
	//   实测踩到过参数表里的退化坐标 (107.7, 1.0, 0.0)：
	//   Valid() 对它返回 true，但它离玩家 2485m，锁上去红点直接飞到天边。
	rotHits := make([]scan.Hit, 0, len(allHits))
	for _, h := range allHits {
		if !h.Rot {
			continue
		}
		if !h.P.WorldCoordOK() {
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

	// ★ 锁定即写盘（verified=false）。
	//   玩家站着不动就永远等不到"走动 3 拍"，若只在 verified 时写盘，
	//   「站着不动」这个最常见的场景每次启动都要重扫 3~8 秒。
	//   这条 verified=false 的记录只作参考（known.Pick 不接受它秒锁），
	//   但能让下次启动知道"这个地址上次还在"。
	t.persist(best.P, false)
	return true
}
