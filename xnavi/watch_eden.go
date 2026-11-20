// watch_eden.go — Eden 专属追踪管道（完整并入 enavi 极简成熟架构）。
//
// 架构设计：
// - 100% 对齐 enavi 极简成熟架构：启动扫描锁定后死读 addr，不搞过度复杂状态；
// - readEdenActor：独立小端 float32 直读 + WorldCoordOK 原语，不被 gameValidTriple 误杀神庙坐标；
// - Run >= 2 真身门禁，游戏加载期绝不抢跑假点；
// - 走动仲裁：站桩不误换，走动秒确认；
// - 传送零重扫：落地直接采纳新落点，addr 恒定不变，绝不重扫换 addr；
// - 神庙本地尺度挂起：试炼场内红点死钉门口，不重扫、不掉线，出庙自动接管；
// - 全量标准化日志：[attach], [lock], [track], [warp], [shrine], [hold], [lost]。

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

type candSlot struct {
	addr  uintptr
	init  [3]float32
	last  [3]float32 // 最近一次采样位置（微动仲裁用）
	micro int        // 连续微动（0.05~10m 单步）计数
	addrs []uintptr  // 组内全部副本地址（真身环形缓冲逐帧轮换槽位，仲裁需全读）
}

type EdenTracker struct {
	p        *edenPlatform
	rescanCh chan *RescanRequest
	stopCh   chan struct{}
}

func NewEdenTracker(p *edenPlatform) *EdenTracker {
	return &EdenTracker{
		p:        p,
		rescanCh: make(chan *RescanRequest, 4),
		stopCh:   make(chan struct{}),
	}
}

func (t *EdenTracker) Name() string {
	return "botw-eden"
}

func (t *EdenTracker) Stop() {
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
}

func (t *EdenTracker) Rescan(req *RescanRequest) {
	select {
	case t.rescanCh <- req:
	default:
	}
}

// readEdenActor 直接按小端读取 12 字节并经 WorldCoordOK 校验（移植自 enavi readActor）。
// 关键：不走 gameValidTriple，原生支持地下神庙试炼场坐标。
func readEdenActor(h uintptr, addr uintptr) ([3]float32, bool) {
	return readEdenActorMode(h, addr, false)
}

// readEdenActorMode shrine=true 时用神庙本地尺度校验（ShrineCoordOK）：
// 试炼场内玩家可能位于本地原点/轴上，大世界校验的近零剔除会误杀。
func readEdenActorMode(h uintptr, addr uintptr, shrine bool) ([3]float32, bool) {
	buf := readMem(h, addr, 12)
	if len(buf) < 12 {
		return [3]float32{}, false
	}
	x := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(buf[4:]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(buf[8:]))
	if shrine {
		if !ShrineCoordOK(x, y, z) {
			return [3]float32{}, false
		}
	} else if !WorldCoordOK(x, y, z) {
		return [3]float32{}, false
	}
	return [3]float32{x, y, z}, true
}

func (t *EdenTracker) Start(ctx context.Context) {
	fmt.Println("  [pipeline] Eden 追踪管道已启动 (Run>=2 动力学门禁 / 零重扫传送) / Eden pipeline started (Run>=2 gate, warp zero-rescan)")

	tick := time.NewTicker(100 * time.Millisecond) // 10Hz
	defer tick.Stop()

	var lockedAddr uintptr
	var cur, prev [3]float32
	hasPrev := false
	verified := false
	moveN := 0
	shrineMode := false
	var shrineEntrance [3]float32
	var cands []candSlot

	var manualTarget [3]float32
	hasManual := false
	lastScanAt := time.Time{}
	holdLogged := false
	holdSince := time.Time{}
	jumpSince := time.Time{}
	lockSince := time.Time{}
	lastUnverifiedHint := time.Time{}
	var trackAddrs []uintptr // 已验证真身的环形缓冲副本地址集（活槽跟踪用）
	lastWorldPos := [3]float32{}
	hasLastWorld := false
	// shrineProbeActive 神庙探测试探态：读空/冻结只说明"锁定失效"，
	// 不能证明进庙（站桩活玩家读值同样冻结——2026-11-20 误判事故）。
	// 探测期间红点保持最后位置（source=hold），只有读到/扫到**正在运动**
	// 的神庙本地坐标（正向证据）才确认进庙，否则按常规掉锁重扫。
	shrineProbeActive := false
	var probeEntrance [3]float32
	probeSince := time.Time{}
	lastProbeScan := time.Time{}
	lastMoveAt := time.Now() // 最近一次观测到真身移动（冻结检测用）

	reset := func(reason string) {
		lockedAddr = 0
		hasPrev = false
		verified = false
		moveN = 0
		shrineMode = false
		cands = nil
		holdLogged = false
		holdSince = time.Time{}
		jumpSince = time.Time{}
		lockSince = time.Time{}
		lastUnverifiedHint = time.Time{}
		trackAddrs = nil
		shrineProbeActive = false
		probeSince = time.Time{}
		lastProbeScan = time.Time{}
		lastMoveAt = time.Now()
		goUnlocked(reason)
	}

	// enterShrineProbe 进入神庙探测态：红点保持最后世界坐标，开始本地尺度
	// 探测。进庙确认必须拿到正向运动证据（原有"读到神庙本地坐标"判定的
	// 强化版），探测本身绝不改变状态语义。
	enterShrineProbe := func(reason string) {
		shrineProbeActive = true
		probeEntrance = lastWorldPos
		probeSince = time.Now()
		lastProbeScan = time.Time{}
		fmt.Printf("  [probe] Eden %s -> 开始神庙本地尺度探测（红点暂保持最后位置，需正向证据确认进庙） / Lock stale (%s), probing for shrine with positive-evidence requirement\n",
			reason, reason)
	}

	// shrineModeScan=true：神庙本地尺度扫描——半径钳制到 300m（试炼场边界），
	// 读值/采样用 ShrineCoordOK（放宽近零剔除），锁定后直接进入 shrineMode。
	scanNear := func(center [3]float32, radius float32, shrineModeScan bool) bool {
		h := t.p.Handle()
		if shrineModeScan && radius > 300 {
			radius = 300
		}
		blks := edenHeapBlocks(h, 0.0625)
		if len(blks) == 0 {
			return false
		}

		var allHits []EdenHit
		var mu sync.Mutex

		const chunk = 8 << 20
		const overlap = 256
		const sleepPerChunk = 4 * time.Millisecond
		workers := 4

		type scanJob struct {
			addr uintptr
			n    int
		}
		var jobs []scanJob
		for _, b := range blks {
			if b.Size == 0 {
				continue
			}
			for off := uintptr(0); off < b.Size; off += uintptr(chunk) {
				n := chunk
				if remain := int(b.Size - off); remain < n {
					n = remain
				}
				if uintptr(n) < b.Size-off {
					n += overlap
				}
				jobs = append(jobs, scanJob{b.Base + off, n})
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
					buf := readMem(h, j.addr, j.n)
					time.Sleep(sleepPerChunk)
					if len(buf) < 12 {
						continue
					}
					hits := FindEdenTriples(buf, j.addr, &center, radius)
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

		// 门禁①：坐标通过 WorldCoordOK（神庙扫描用 ShrineCoordOK 放宽近零剔除）
		// 门禁②：硬门槛 Run >= 2（真林克专属 RingBuffer 特征，彻底排除静态数据）
		var identHits []EdenHit
		for _, hit := range allHits {
			if shrineModeScan {
				if !ShrineCoordOK(hit.P[0], hit.P[1], hit.P[2]) {
					continue
				}
			} else if !WorldCoordOK(hit.P[0], hit.P[1], hit.P[2]) {
				continue
			}
			if hit.Run >= 2 {
				identHits = append(identHits, hit)
			}
		}
		if len(identHits) == 0 {
			return false
		}

		groups := GroupEdenHits(identHits, nil)
		if len(groups) == 0 {
			return false
		}

		// 收集每个 0.1m 网格组的全部副本地址（上限 64）。
		// 真身是环形缓冲：每帧写入不同槽位，只盯单一代表地址会读到冻结旧帧。
		addrsByCell := make(map[string][]uintptr)
		for _, hit := range identHits {
			k := edenMotionKey(hit.P)
			if len(addrsByCell[k]) < 64 {
				addrsByCell[k] = append(addrsByCell[k], hit.Addr)
			}
		}

		// 锁定前运动学甄别：对前 64 组的全部副本做 3 轮采样（~0.5s），
		// 持续平移的组（真身）优先于长 Run 静态组（动画/参数表）。
		var motion map[string]edenMotion
		if len(groups) > 1 {
			top := groups
			if len(top) > 64 {
				top = top[:64]
			}
			motion = edenSampleMotionAddrsMode(h, top, addrsByCell, 3, shrineModeScan)
			groups = GroupEdenHits(identHits, motion)
		}

		// 首选候选：运动学证据最强者（有持续平移直接锁定，跳过等待走动）；
		// 无运动证据时退回排序首位（站立玩家场景，走两步由仲裁确认）。
		best := groups[0]
		bestLive := false
		if motion != nil {
			var bestM edenMotion
			for _, g := range groups {
				m := motion[edenMotionKey(g.P)]
				if m.sustained() && m.maxD > bestM.maxD {
					best, bestM, bestLive = g, m, true
				}
			}
		}

		if shrineModeScan {
			// 探测态门槛：本地尺度静态表/残帧遍地都是，必须有"正在运动"
			// 的正向证据才承认是庙内真身；否则视为未发现，继续探测/常规自愈。
			if !bestLive {
				return false
			}
			lockedAddr = best.Addr
			cur = best.P
			prev = cur
			hasPrev = true
			verified = true
			moveN = 0
			shrineMode = true
			shrineEntrance = probeEntrance
			shrineProbeActive = false
			holdLogged = false
			lockSince = time.Now()
			lastMoveAt = time.Now()
			lastUnverifiedHint = time.Time{}
			trackAddrs = addrsByCell[edenMotionKey(best.P)]
			cands = nil
			setLock(lockedAddr, true, 1, "eden-shrine-lock")
			publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
			fmt.Printf("  [shrine] Eden 神庙内探测锁定成功（本地坐标持续运动）Addr: 0x%X 本地 (%.1f, %.1f, %.1f) -> 红点锚定入口 (%.1f, %.1f, %.1f) / Shrine confirmed by live local motion, holding entrance\n",
				lockedAddr, cur[0], cur[1], cur[2], shrineEntrance[0], shrineEntrance[1], shrineEntrance[2])
			return true
		}

		lockedAddr = best.Addr
		cur = best.P
		prev = cur
		hasPrev = true
		verified = false
		moveN = 0
		shrineMode = false
		holdLogged = false
		lockSince = time.Now()
		lastMoveAt = time.Now()
		lastUnverifiedHint = time.Time{}
		trackAddrs = addrsByCell[edenMotionKey(best.P)]

		var slots []candSlot
		for i := 0; i < len(groups) && i < 12; i++ {
			slots = append(slots, candSlot{addr: groups[i].Addr, init: groups[i].P, last: groups[i].P, addrs: addrsByCell[edenMotionKey(groups[i].P)]})
		}
		cands = slots

		if bestLive {
			// 扫描期间已观测到持续平移 = 走动验证的等价证据，直接确认，
			// 不再要求用户"走两步"（真身环形缓冲轮换槽位，死等单地址位移会误报）。
			verified = true
			cands = nil
			setLock(lockedAddr, true, 1, "eden-motion-lock")
			edenSetRefCenter(cur)
			publishState(true, cur[0], cur[1], cur[2], true, 1, "player")
			fmt.Printf("  [lock] Eden 运动学锁定成功！扫描期间持续平移，真身锁定 Addr: 0x%X (%.1f, %.1f, %.1f) -> 【已完全锁定】 / Motion-verified lock at 0x%X -> [Fully locked]\n",
				lockedAddr, cur[0], cur[1], cur[2], lockedAddr)
			return true
		}

		setLock(lockedAddr, false, len(groups), "eden-scan")
		publishState(true, cur[0], cur[1], cur[2], false, len(groups), "eden-scan")
		fmt.Printf("  [verify] Eden 捕获动力学候选地址 Addr: 0x%X (%.1f, %.1f, %.1f) -> 【请在游戏内移动角色以完成验证】 / Candidate locked: 0x%X -> [Please move Link in-game to verify]\n",
			lockedAddr, cur[0], cur[1], cur[2], lockedAddr)
		return true
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("  [pipeline] Eden pipeline stopped (context cancelled)")
			return
		case <-t.stopCh:
			fmt.Println("  [pipeline] Eden 追踪管道已停止 (请求停止) / Eden pipeline stopped (stop requested)")
			return
		case req := <-t.rescanCh:
			reset("Eden /rescan requested")
			if req != nil && req.HasTarget {
				hasManual = true
				manualTarget = [3]float32{req.TargetX, 0, req.TargetZ}
				fmt.Printf("  [scan] Eden 手工校准目标: (%.1f, %.1f) / Eden manual target set: (%.1f, %.1f)\n", req.TargetX, req.TargetZ, req.TargetX, req.TargetZ)
			}
		case <-tick.C:
			// 1. 确保模拟器附加
			if !t.p.Attach() {
				if lockedAddr != 0 {
					fmt.Println("  [lost] Eden 模拟器进程断开或退出 / Eden emulator process exited or detached")
					reset("Eden lost")
				} else {
					state.mu.Lock()
					state.ok = false
					state.source = "waiting for Eden"
					state.mu.Unlock()
				}
				EdenClearRefCenter()
				continue
			}

			h := t.p.Handle()

			// 模组注入定位：仅在显式选择该模式时启用（默认 auto 走下方内存扫描）。
			// 信标缺失或停摆时回退扫描器。
			if locateMode == "mod-inject" && tryBeaconTiers(h, t.p) {
				continue
			}

			// 2. UNLOCKED：User Heap 动态扫描（带 Run>=2 门禁）
			if lockedAddr == 0 {
				if time.Since(lastScanAt) < 1*time.Second {
					continue
				}
				lastScanAt = time.Now()

				var scanCenter [3]float32
				var radius float32 = 120.0

				// 检查磁盘新鲜存档（对齐 enavi：传送落地必定触发自动存档）
				anchors := currentGame.SaveAnchors(t.p)
				var latestSaveAnchor *SaveAnchor
				saveIsFresh := false
				if len(anchors) > 0 {
					latestSaveAnchor = anchors[0]
					if fi, err := os.Stat(latestSaveAnchor.Path); err == nil {
						if time.Since(fi.ModTime()) <= 120*time.Second {
							saveIsFresh = true
						}
					}
				}

				switch {
				case hasManual:
					scanCenter = manualTarget
					radius = 150.0
					fmt.Printf("  [scan] Eden 扫描手工定向窗口 150m (%.1f, %.1f)... / Scanning manual window 150m near (%.1f, %.1f)...\n", scanCenter[0], scanCenter[2], scanCenter[0], scanCenter[2])
				case latestSaveAnchor != nil:
					// 始终以最新存档锚点为中心扫描（绝不拿切场景掉锁前的旧废弃坐标当中心）
					scanCenter = latestSaveAnchor.Pos
					radius = 120.0
					if saveIsFresh {
						fmt.Printf("  [scan] Eden 发现新鲜存档 (<=120s)，扫描 120m 锚点窗口 (%.1f, %.1f, %.1f)... / Fresh save detected, scanning window 120m...\n",
							scanCenter[0], scanCenter[1], scanCenter[2])
					} else {
						fmt.Printf("  [scan] Eden 扫描存档锚点 120m 窗口 (%.1f, %.1f, %.1f)... / Scanning save anchor window 120m...\n",
							scanCenter[0], scanCenter[1], scanCenter[2])
					}
				default:
					state.mu.Lock()
					state.ok = false
					state.source = "waiting for save anchor (press [+] in game)"
					state.mu.Unlock()
					continue
				}

				found := scanNear(scanCenter, radius, false)
				if !found && !hasManual {
					found = scanNear(scanCenter, 400.0, false)
				}
				if !found && !hasManual {
					found = scanNear(scanCenter, 3000.0, false)
				}
				if found {
					hasManual = false
				} else {
					fmt.Println("  [scan] Eden 扫描未发现活动玩家 (等待进入 3D 游戏世界)... / No live player found (waiting for 3D world)...")
				}
				continue
			}

			// 3. LOCKED：死守 lockedAddr，过图黑屏平滑保持，落地直接接管，绝不主动掉锁
			// 神庙模式/探测态用本地尺度校验，避免近零坐标被误杀。
			shrineRead := shrineMode || shrineProbeActive
			d, ok := readEdenActorMode(h, lockedAddr, shrineRead)
			if !ok {
				for i := 0; i < 3; i++ {
					time.Sleep(30 * time.Millisecond)
					if d, ok = readEdenActorMode(h, lockedAddr, shrineRead); ok {
						break
					}
				}
			}

			// 环形缓冲活槽跟踪：真身每帧写入不同槽位，锁定槽读到的是冻结旧帧。
			// 在组内副本地址里找“离上一位置最近且确有变化”的活槽并切换，
			// 否则红点会钉死不动、走动验证永远失败（2026-11-19 真机实测）。
			if verified && len(trackAddrs) > 1 && hasPrev {
				bestP := prev
				bestAddr := lockedAddr
				bestStep := float32(-1)
				for _, a := range trackAddrs {
					if a == lockedAddr {
						continue
					}
					p, okP := readEdenActorMode(h, a, shrineRead)
					if !okP {
						continue
					}
					step := dist3(p, prev)
					if step < 0.05 || step >= 30.0 {
						continue // 冻结旧帧 / 异常跳变不是正常走动
					}
					if bestStep < 0 || step < bestStep {
						bestStep, bestP, bestAddr = step, p, a
					}
				}
				if bestStep >= 0 {
					lockedAddr = bestAddr
					d, ok = bestP, true
				}
			}

			// 读不到标准三元组（过图黑屏 / 进庙场景销毁 / 瞬态读空）
			if !ok {
				if shrineMode {
					// 神庙内读空：红点钉入口；每 15s 本地尺度重扫（需运动证据）
					// 刷新内部锁定，确保出庙瞬间能读到世界坐标恢复追踪。
					if time.Since(lastProbeScan) > 15*time.Second {
						lastProbeScan = time.Now()
						if scanNear([3]float32{}, 300, true) {
							continue
						}
					}
					publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
					continue
				}

				if !holdLogged {
					fmt.Println("  [hold] Eden reading transient/loading -> holding position")
					holdLogged = true
					holdSince = time.Now()
					// 已验证真身突然持续读空：可能是进庙，也可能是槽位释放。
					// 只启动探测（红点保持最后位置），进庙与否由正向证据决定。
					if verified && hasLastWorld {
						enterShrineProbe("锁定地址持续读空")
					}
				}
				if shrineProbeActive {
					// 每 5s 神庙本地尺度扫描（需持续运动证据才确认进庙）。
					if time.Since(lastProbeScan) > 5*time.Second {
						lastProbeScan = time.Now()
						if scanNear([3]float32{}, 300, true) {
							continue
						}
					}
					// 探测 30s 无正向证据 → 不是进庙，常规掉锁重扫（恢复原行为）。
					if time.Since(probeSince) > 30*time.Second {
						fmt.Println("  [lost] Eden 读空探测 30s 未发现庙内运动目标，按槽位释放重扫 / Probe found no live shrine target, rescanning")
						reset("Eden probe timeout (slot freed)")
						continue
					}
					if hasPrev {
						publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
					}
					continue
				}
				// 保持态超时自愈：正常过图/传送加载远短于 20s。持续读空说明
				// 演员槽位已被释放/复用（实测传送后槽位被清零），旧实现会永远
				// 钉死在废弃坐标上 → 解锁重扫，以最新存档锚点重新定位。
				if time.Since(holdSince) > 20*time.Second {
					fmt.Println("  [lost] Eden 锁定地址持续读空 >20s，判定演员槽位已释放，自动重扫 / Locked slot freed, auto-rescanning")
					reset("Eden hold timeout (slot freed)")
					continue
				}
				if hasPrev {
					publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
				}
				continue
			}

			// 读出合法坐标
			holdLogged = false
			holdSince = time.Time{}
			cur := d
			isShrine := isShrineScale(cur)

			// 探测态期间读到值：
			// 世界尺度的新位置（非冻结回声）= 锁定仍活着，退出探测继续跟随；
			// 本地尺度读值必须再核实"副本槽位仍在更新"（正向运动证据）才确认
			// 进庙——静态本地表/残帧读值合法但冻结，绝不能据此判进庙。
			if shrineProbeActive {
				frozenEcho := hasPrev && dist3(cur, prev) < 0.05
				if !isShrine && !frozenEcho {
					shrineProbeActive = false
					lastMoveAt = time.Now()
					fmt.Printf("  [probe] Eden 探测期间读到大世界新位置 -> 锁定仍有效，恢复跟随 (%.1f, %.1f, %.1f) / Live world read during probe, resuming\n",
						cur[0], cur[1], cur[2])
					prev = cur
					hasPrev = true
					lastWorldPos = cur
					hasLastWorld = true
					publishState(true, cur[0], cur[1], cur[2], verified, len(cands), "player")
					continue
				}
				if isShrine && !frozenEcho {
					liveLocal := false
					for _, a := range trackAddrs {
						if p, okP := readEdenActorMode(h, a, true); okP && isShrineScale(p) && dist3(p, cur) >= 0.05 && dist3(p, cur) < 30.0 {
							liveLocal = true
							break
						}
					}
					if liveLocal {
						shrineMode = true
						shrineEntrance = probeEntrance
						shrineProbeActive = false
						fmt.Printf("  [shrine] Eden 确认进入试炼场（本地坐标持续运动）-> 锚定入口 (%.1f, %.1f, %.1f) / Shrine entry confirmed by live local motion\n",
							shrineEntrance[0], shrineEntrance[1], shrineEntrance[2])
						publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
						prev = cur
						hasPrev = true
						continue
					}
				}
				// 冻结回声/静态本地读值：红点保持最后位置，继续探测
				if time.Since(lastProbeScan) > 5*time.Second {
					lastProbeScan = time.Now()
					if scanNear([3]float32{}, 300, true) {
						continue
					}
				}
				if time.Since(probeSince) > 30*time.Second {
					fmt.Println("  [lost] Eden 冻结探测 30s 无庙内运动证据，按锁定失效重扫 / Probe found no live shrine target, rescanning")
					reset("Eden probe timeout (stale lock)")
					continue
				}
				if hasPrev {
					publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
				}
				continue
			}

			// 冻结检测：读值合法但真身长时间无任何位移，且全部副本槽位也读不出
			// 变化 → 锁定可能失效（进庙残留帧/槽位复用）。只启动探测，不直接
			// 判进庙：站桩活玩家同样全冻结，进庙必须另有正向运动证据。
			if verified && !isShrine && hasLastWorld && time.Since(lastMoveAt) > 15*time.Second {
				anyAlive := false
				for _, a := range trackAddrs {
					if p, okP := readEdenActor(h, a); okP && dist3(p, cur) >= 0.05 && dist3(p, cur) < 30.0 {
						anyAlive = true
						break
					}
				}
				if !anyAlive {
					enterShrineProbe("读值冻结 15s 且全副本静默")
					if hasPrev {
						publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
					}
					continue
				}
				lastMoveAt = time.Now() // 有活槽证据，重置计时继续观察
			}

			// 神庙内部试炼场本地尺度挂起
			if shrineMode {
				if !isShrine {
					// 出庙：脱离神庙本地尺度，恢复大世界跟踪
					shrineMode = false
					destName := formatDestName(cur)
					fmt.Printf("  [shrine] Eden 角色离开试炼场 -> 恢复大世界追踪 (%.1f, %.1f, %.1f) [%s] / Left shrine -> Resumed world tracking\n",
						cur[0], cur[1], cur[2], destName)
					publishState(true, cur[0], cur[1], cur[2], verified, len(cands), "player")
					prev = cur
					hasPrev = true
					lastWorldPos = cur
					hasLastWorld = true
					continue
				}
				// 还在神庙内：红点钉入口；每 15s 本地尺度重扫（需运动证据）
				// 刷新内部锁定，防止庙内演员重建后旧地址失效导致出庙时读空。
				if time.Since(lastProbeScan) > 15*time.Second {
					lastProbeScan = time.Now()
					if scanNear([3]float32{}, 300, true) {
						continue
					}
				}
				publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
				continue
			} else if isShrine && (!hasPrev || !isShrineScale(prev)) && dist3(cur, prev) > 100.0 {
				// 进庙：世界尺度 → 神庙本地尺度，红点钉在进入前的门口 prev
				shrineMode = true
				shrineEntrance = prev
				destName := formatDestName(prev)
				fmt.Printf("  [shrine] Eden 角色进入试炼场 [%s] -> 锚定神庙入口 (%.1f, %.1f, %.1f) / Entered shrine [%s] -> Holding entrance\n",
					destName, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], destName)
				publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
				continue
			}

			// 多候选走动仲裁（防驿站等多 NPC 场景误锁静止 NPC）
			// 真身是环形缓冲：每帧写入不同槽位，旧帧冻结。因此对组内全部副本
			// 地址读取，取“离上次位置最近且确有变化”的活槽；累计位移 >=0.5m
			// 或连续 2 次微动即确认。锁定候选本身也参与（不再跳过）。
			if !verified && len(cands) > 0 {
				for ci := range cands {
					c := &cands[ci]
					scanAddrs := c.addrs
					if len(scanAddrs) == 0 {
						scanAddrs = []uintptr{c.addr}
					}
					var bestP [3]float32
					var bestAddr uintptr
					bestStep := float32(-1)
					for _, a := range scanAddrs {
						p, okP := readEdenActor(h, a)
						if !okP {
							continue
						}
						step := dist3(p, c.last)
						if step < 0.05 || step >= 10.0 {
							continue // 冻结旧帧或大跳变都不是走动
						}
						if bestStep < 0 || step < bestStep {
							bestStep, bestP, bestAddr = step, p, a
						}
					}
					if bestStep < 0 {
						continue
					}
					c.micro++
					c.last = bestP
					dCand := dist3(bestP, c.init)
					if (dCand >= 0.5 && dCand < 150.0) || (c.micro >= 2 && dCand < 150.0) {
						lockedAddr = bestAddr
						cur = bestP
						verified = true
						trackAddrs = c.addrs
						cands = nil
						setLock(lockedAddr, true, 1, "eden-move-confirm")
						fmt.Printf("  [lock] Eden 走动仲裁成功！真身锁定 Addr: 0x%X (%.1f, %.1f, %.1f) -> 【已完全锁定】 / Movement arbitration confirmed! Locked at 0x%X -> [Fully locked]\n",
							lockedAddr, cur[0], cur[1], cur[2], lockedAddr)
						break
					}
				}
			}

			delta := dist3(cur, prev)

			// 传送判定（>300m）
			// ★ enavi 实测真理：传送后 addr 不变，坐标直接更新！绝不重扫换 addr！
			if hasPrev && delta > 300.0 {
				shrineMode = false
				cands = nil
				edenSetRefCenter(cur)
				destName := formatDestName(cur)
				fmt.Printf("  [warp] Eden 角色大传送 (距离=%.1fm) -> %s (%.1f, %.1f, %.1f) [零重扫接管] / Fast travel (dist=%.1fm) -> %s [Direct takeover]\n",
					delta, destName, cur[0], cur[1], cur[2], delta, destName)
				publishState(true, cur[0], cur[1], cur[2], verified, 1, "player")
				prev = cur
				lastMoveAt = time.Now()
				if !isShrine {
					lastWorldPos = cur
					hasLastWorld = true
				}
				continue
			}

			// 单帧位移异常过大（>150m 非传送跳变）
			if hasPrev && delta > 150.0 {
				if jumpSince.IsZero() {
					jumpSince = time.Now()
				}
				// 持续大跳变 >10s：槽位很可能已被别的演员复用（坐标持续漂移），
				// 旧实现会永远 hold 在旧坐标 → 解锁重扫。
				if time.Since(jumpSince) > 10*time.Second {
					fmt.Println("  [lost] Eden 锁定地址持续大跳变 >10s，判定槽位被复用，自动重扫 / Slot reused, auto-rescanning")
					reset("Eden persistent jump (slot reused)")
					continue
				}
				publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
				continue
			}
			jumpSince = time.Time{}

			// 正常跟随与移动确认
			if hasPrev && delta >= 0.5 {
				moveN++
				lastMoveAt = time.Now()
				if moveN >= 3 && !verified {
					verified = true
					cands = nil
					setLock(lockedAddr, true, 1, "eden-live")
					edenSetRefCenter(cur)
					fmt.Printf("  [lock] Eden 角色移动确认成功！真身锁定 Addr: 0x%X -> 【已完全锁定】 / Movement confirmed! Live actor locked at 0x%X -> [Fully locked]\n", lockedAddr, lockedAddr)
				}
			} else {
				moveN = 0
			}
			if hasPrev && delta >= 0.05 {
				lastMoveAt = time.Now() // 微动也算活体证据（冻结检测用）
			}

			// 提示机制：若锁定后持续未确认走动，友好提醒用户
			if !verified && time.Since(lockSince) > 6*time.Second && time.Since(lastUnverifiedHint) > 15*time.Second {
				lastUnverifiedHint = time.Now()
				fmt.Println("  [hint] 主角未确认走动 (verified=false)。请在游戏内走动几步 / Link movement not confirmed. Please move a few steps in-game")
			}

			// 超时自愈：90s 仍未确认且候选全体冻结 → 锁的很可能是静态数据，
			// 自动解锁重扫（旧实现只会无限提示，用户走多久都没用）。
			// 有任何候选在动则不重扫——真身可能只是没被选为首候选，仲裁会继续。
			if !verified && time.Since(lockSince) > 90*time.Second {
				allFrozen := true
				for _, c := range cands {
					if cd, okC := readEdenActor(h, c.addr); okC && dist3(cd, c.init) >= 0.05 {
						allFrozen = false
						break
					}
				}
				if allFrozen {
					fmt.Println("  [rescan] Eden 锁定 90s 未确认走动且候选全体冻结，判定静态误锁，自动重扫 / Static lock suspected, auto-rescanning")
					reset("Eden static-lock timeout")
					continue
				}
			}

			publishState(true, cur[0], cur[1], cur[2], verified, len(cands), "player")
			prev = cur
			hasPrev = true
			if !isShrine {
				lastWorldPos = cur
				hasLastWorld = true
			}
		}
	}
}
