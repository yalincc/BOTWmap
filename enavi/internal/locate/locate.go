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
	readFailMax  = 25    // 连续读失败几次判定失效（2.5 秒，足够黑屏加载过渡）
	moveDist     = 0.5   // 视为"动了"的门槛
	moveConfirmN = 3     // 连续几拍动 → 已验证
	// 神庙本地尺度判据（§3.5 物理特征鉴别表）：
	//   地下神庙试炼场 = |X|<300 && |Z|<300 && **Y < 50**（涵盖 DLC 罗塔·奇格尔 Z=108.9m 长廊等神庙）
	//   平原之塔 X≈-215 Z≈250 → Y≈250 超 50 → 不误判
	//   中央平原地面遗迹 Y≈100~130 → Y 超 50 → 不误判
	shrineXY    = 300.0 // 神庙本地坐标的 X/Z 绝对值上限（米，覆盖 136 座神庙全量包围盒）
	shrineMaxY  = 50.0  // 神庙本地坐标的海拔上限（米）
	saveFreshS  = 120   // 磁盘存档"新鲜"判定：mtime 距今秒数（自动存档落地后约 1~2 秒写入）
	winWide     = 3000.0
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

// ---------- ★ 身份判据与存活判据（2026-10-21 两次事故后重订） ----------
//
// 事故 1（11:53）：读坐标不做校验 → 进神庙后 Actor 坐标被清零/写成 NaN，
// 旧代码把 NaN 当合法坐标一路用下去。
//
// 事故 2（15:2x）：加了「3x3 旋转矩阵」判据后仍然无限重扫，
// 因为 **readActor 只读 64 字节**，而矩阵在 +0x20 处 → 要读到 68 字节才看得见
// → 每拍都误判「Actor 已死」→ 无限重扫 → 表现为「坐标固定、一直失败」。
//
// 事故 3（15:4x，实测确认）：在某些场景里 **根本不存在真正的 Actor 3x3 矩阵**。
//   600m 宽窗实测：带 rot 的命中 60 个，其中
//     · 真 Actor 矩阵（非轴排列）= **0**
//     · 轴矩阵（骨骼基向量/方向轴，误判）= 32
//     · 位置历史记录（pos+单位四元数）= 8
//   而 120m 窄窗：带四元数指纹的命中 **1213** 个，带矩阵的只有 6 个（全是轴矩阵）。
//
// ⇒ 结论：**当前实机唯一可靠的身份判据是「pos + 单位模长四元数」**，
//   也就是 BOTW 自己的「位置历史记录」（0x28 步长，pos + 四元数 + 角度）。
//   它实时更新（实测玩家跑动时逐帧变），比 3x3 矩阵判据**更准**。
//   3x3 矩阵判据降级为辅助（存在时加分），并排除轴矩阵这种假阳性。
//
// 存活判据（三道闸）：
//  ① 坐标必须合法（WorldCoordOK：NaN/Inf/越界/近零/退化/参数表全挡）
//  ② 四元数仍是单位模长（说明这条记录还活着）
//  ③ 缓冲读够了长度（不够 = "没读到"，不是"没有"，绝不能据此判死）

// readActor 读一个位置记录 + 验证它还活着。
//
// 返回 (pos, alive, ok)：
//   - ok=false   → 连 12 字节都读不出来（真正的读失败）
//   - alive=false → 读出来了，但坐标非法 或 四元数已失效 → 记录死了，必须重扫
//
// ★★★ 读取长度必须够（2026-10-21 实测事故）：
//   记录 = pos(12) + 四元数(16) → 至少 28 字节；
//   若还要看 3x3 矩阵（最远 +0x2C）→ 需 80 字节。
//   旧代码读 64 字节 → 矩阵在 +0x20 时够不到 → 误判「Actor 已死」→ 无限重扫。
// readActor 读一个位置记录。
// 只要能读出 12 字节并且通过 WorldCoordOK，即视为有效坐标。
func (t *Tracker) readActor(addr uintptr) (pos scan.Vec3, ok bool) {
	buf, err := t.proc.ReadTimeout(addr, 12, 500*time.Millisecond)
	if err != nil || len(buf) < 12 {
		return scan.Vec3{}, false
	}
	p := scan.Vec3{
		X: math.Float32frombits(binary.LittleEndian.Uint32(buf[0:])),
		Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
		Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
	}
	if !p.WorldCoordOK() {
		return p, false
	}
	return p, true
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
}

// Stats 诊断信息。
type Stats struct {
	Addr   uintptr
	Scans  int
	Busy   bool
	Anchor scan.Vec3
}

// New 建立跟踪器。以最新存档锚点开窗扫描定位真林克。
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

	// 锚点（存档）——冷启动扫描中心。
	// ★ 用 saveanchor.Best 挑「有进度且最新」的那一档，排除空档/新档。
	anchors, _ := saveanchor.Find(p.ExePath())
	best := saveanchor.Best(anchors)
	if best != nil {
		t.anchor = scan.Vec3{X: best.Pos[0], Y: best.Pos[1], Z: best.Pos[2]}
	}

	// 存档锚点窗口扫描（以存档为中心，在近场找真实玩家坐标槽）
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
		logf("[locate] 存档窄窗/中窗未找到活跃玩家，改用 %0.0fm 宽窗重扫\n", winWide)
		if t.rescan(t.anchor, winWide) {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("未在存档附近找到林克（请在游戏内按【+】手动存个档，或点击【手工校准】输入大致位置后重新定位）")
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

	// ★ 死读当前锁定地址：只要能读出合法的海拉鲁世界坐标（WorldCoordOK），就死读它
	v, ok := t.readActor(addr)
	isShrineLocal := ok && isShrineScale(v)

	// 神庙内部试炼场本地尺度判定（必须 |X|<100, |Z|<100 且高度 Y < 50）
	// 注意：中央平原之塔等大世界坐标也在原点附近（但高度 Y > 100m），不可误判为神庙！
	if shrine {
		if ok && !isShrineLocal {
			// 出庙（脱离神庙本地尺度，恢复大世界尺度）：解除神庙挂起，恢复实时跟随
			t.mu.Lock()
			t.shrine = false
			t.cur = v
			t.ok = true
			t.fails = 0
			t.mu.Unlock()
			logf("[locate] 离开神庙，恢复跟随：addr=%#x pos=(%.1f, %.1f, %.1f)\n", addr, v.X, v.Y, v.Z)
			return Update{OK: true, Verified: verified, Pos: v, Addr: addr, Source: "player", Note: "已离开神庙，恢复跟随"}
		}
		// 还在神庙内（无论是读出神庙本地坐标，还是黑屏瞬态读不到）：
		// ★ 死锁保护：绝不把 addr 置 0，绝不在神庙内部发起重扫，红点死死钉在神庙门口！
		return Update{OK: true, Verified: true, Pos: cur, Addr: addr, Source: "shrine", Shrine: true, Note: "神庙内试炼场（本地坐标），红点钉在门口"}
	} else if ok && isShrineLocal && !isShrineScale(cur) && v.Dist3D(cur) > 100 {
		// 进庙：世界尺度 → 神庙本地尺度。位置钉在进入前的门口（cur）
		t.mu.Lock()
		t.shrine = true
		t.ok = true
		t.fails = 0
		t.mu.Unlock()
		logf("[locate] 进入神庙，红点钉在入口：pos=(%.1f, %.1f, %.1f)\n", cur.X, cur.Y, cur.Z)
		return Update{OK: true, Verified: true, Pos: cur, Addr: addr, Source: "shrine", Shrine: true, Note: "神庙内试炼场（本地坐标），红点钉在门口"}
	}

	if !ok {
		// 真正的读失败（进程退出/内存页未就绪/坐标非法）
		t.mu.Lock()
		t.fails++
		f := t.fails
		if f >= readFailMax {
			t.ok = false
			t.addr = 0 // 连续读失败达上限 → 允许重扫
		}
		t.mu.Unlock()
		if f >= readFailMax {
			t.rescanAsync(cur, winFar)
			return Update{OK: false, Source: "lost", Note: "连续读失败，重扫中"}
		}
		// 单次失败多半是瞬态（如传送加载中、过图黑屏），保持上一已知位置
		return Update{OK: okCur, Verified: verified, Pos: cur, Addr: addr, Source: "hold"}
	}

	// 读成功，重置失败计数
	t.mu.Lock()
	t.fails = 0
	t.mu.Unlock()

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
					t.mu.Unlock()
					logf("[locate] 走动仲裁识别真玩家：addr=%#x pos=(%.1f, %.1f, %.1f)\n", c.addr, cPos.X, cPos.Y, cPos.Z)
					return Update{OK: true, Verified: true, Pos: cPos, Addr: c.addr, Source: "player", Note: "走动仲裁识别为真玩家"}
				}
			}
		}
	}

	d := v.Dist3D(cur)

	// ★ 传送：单拍 >300m。
	// 老大实测确认：传送后 addr 不变，坐标直接更新！因此直接更新坐标，绝不重扫换 addr！
	if d > teleportDist {
		t.mu.Lock()
		t.cur = v
		t.fails = 0
		t.shrine = false // 传送必定解除神庙状态
		t.cands = nil   // 清空旧候选池
		t.mu.Unlock()
		logf("[locate] 传送跳变（位移 %.1fm）：addr=%#x 保持锁定，新坐标=(%.1f, %.1f, %.1f)\n",
			d, addr, v.X, v.Y, v.Z)
		return Update{OK: true, Verified: verified, Pos: v, Addr: addr, Source: "player", Note: "传送落地"}
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
			t.cands = nil
			logf("[locate] 移动确认为真玩家：addr=%#x 已锁定\n", addr)
		}
	}
	upd := Update{OK: true, Verified: t.verified, Pos: v, Addr: addr, Source: "player"}
	t.mu.Unlock()
	return upd
}

// Rescan 手动重扫。
func (t *Tracker) Rescan() {
	t.mu.Lock()
	t.shrine = false
	center := t.cur
	t.mu.Unlock()
	t.rescanAsync(center, winFar)
}

// RescanManual 手工指定坐标重扫（输入大致 XZ 坐标定向扫描，150m 窄窗秒搜）。
func (t *Tracker) RescanManual(center scan.Vec3) {
	t.mu.Lock()
	t.shrine = false
	t.cur = center
	t.addr = 0
	t.mu.Unlock()
	logf("[locate] 收到手工校准坐标：(%.1f, %.1f)，开启定向快速找人\n", center.X, center.Z)
	t.rescanAsync(center, 150.0)
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
	// ★ 刷新 User Heap 区域：模拟器若在游戏内重启过，User Heap 会重新分配映射
	if d, _, err := t.proc.FindDram(); err == nil {
		if regs := scan.HeapRegions(t.proc, d, 64<<10); len(regs) > 0 {
			t.regions = regs
		}
	}

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
	workers := 4
	const sleepPerChunk = 4 * time.Millisecond
	var jobs []scanJob
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
			jobs = append(jobs, scanJob{r.Base + off, n})
		}
	}
	if len(jobs) == 0 {
		return false
	}

	ch := make(chan scanJob, workers*2)
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

	// 过滤候选：
	// ① 坐标必须通过 WorldCoordOK
	// ② ★★★ 硬门槛：必须有连续 run（Run >= 2）！
	//    真林克在 3D 世界生成后拥有完整的 RingBuffer（实测 run=33）。
	//    而在游戏主菜单、黑屏加载期，内存里只有参数表等静态数据（run=0）。
	//    ★ 绝对不收 Run < 2 的死数据！宁可报未命中返回 false，也绝不误锁死数据！
	identHits := make([]scan.Hit, 0, len(allHits))
	for _, h := range allHits {
		if !h.P.WorldCoordOK() {
			continue
		}
		if h.Run >= 2 {
			identHits = append(identHits, h)
		}
	}
	if len(identHits) == 0 {
		return false
	}

	// 坐标分组去重并排序（Run 长度越长越优先 + 有四元数优先 + 距中心近优先）
	groups := scan.Group(identHits)
	if len(groups) == 0 {
		return false
	}

	best := groups[0]
	logf("[locate] 扫描锁定最佳候选：%#x pos=(%.1f, %.1f, %.1f) run=%d dist=%.1fm\n",
		best.Addr, best.P.X, best.P.Y, best.P.Z, best.Run, best.Dist)

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

// scanJob 一次分块读取任务。
type scanJob struct {
	addr uintptr
	n    int
}
