// 定位核心（单一状态机）。
//
// 蓝本：TOTKmap live-go/watch.go（V1.8.7，已真机验证"零自动扫描 + 探针监听 +
// 冻结共识"方案）；行为基线：BotwNavi(live-go) 与 navicemu(live-cemu)。
// 平台差异（进程/端序/内存块/存档）经 Platform 接口隔离；游戏差异（坐标校验/
// 地图换算/layer）在 game.go。
//
// 核心原则（ds 文档第 2 节）：
//   - 单一状态机：同一时刻只有 stateMachine() 写 lock.addr。
//   - 固定偏移优先：平台已知偏移直接秒锁，失败再扫描。
//   - 全量扫描永久兜底：任何快速路径失败都自动回退扫描。
//   - 游戏重启检测保留：块世代检测，新块 → 只扫新块 + 回 UNLOCKED。

package main

import (
	"fmt"
	"math"
	"sync"
	"time"
)

type lockT struct {
	mu       sync.RWMutex
	addr     uintptr
	verified bool
	copies   int
	source   string
}

var lock = lockT{}

// TOTK layer 日志（验证天空/地底位面）：layer 变化或超 30s 打一次。
var (
	lastLayerLog    int
	lastLayerLogAt  time.Time
)

type stateT struct {
	mu         sync.RWMutex
	ok         bool
	gx, gy, gz float32 // gx=X东, gy=alt高, gz=Z南
	mx, my     float32 // 地图像素
	layer      int
	age        float64
	verified   bool
	copies     int
	source     string
}

var state = stateT{}

var procPID uint32

func guestRamBase() uintptr {
	if currentSessionBase != 0 {
		return currentSessionBase
	}
	if b := loadKnownBlock(); b != 0 {
		return b
	}
	p := currentPlatform()
	if p == nil || p.Handle() == 0 {
		return 0
	}
	base, _ := p.LargestBlock(1024.0)
	return base
}

func tryOffsets() (uintptr, [3]float32, bool) {
	if !ensureAttach() {
		return 0, [3]float32{}, false
	}
	p := currentPlatform()
	h := p.Handle()
	base := guestRamBase()
	if base == 0 {
		return 0, [3]float32{}, false
	}
	offs := loadOffsets()
	type val struct {
		addr uintptr
		pos  [3]float32
	}
	var vals []val
	for _, o := range offs {
		if d := currentGame.DecodeAt(h, base+o); d != nil {
			vals = append(vals, val{base + o, [3]float32{d[0], d[1], d[2]}})
		}
	}
	if len(vals) == 0 {
		return 0, [3]float32{}, false
	}
	best, bestN := val{}, -1
	for _, a := range vals {
		n := 0
		for _, b := range vals {
			if abs32(a.pos[0]-b.pos[0]) < 2 && abs32(a.pos[1]-b.pos[1]) < 2 && abs32(a.pos[2]-b.pos[2]) < 2 {
				n++
			}
		}
		if n > bestN {
			best, bestN = a, n
		}
	}
	if len(vals) >= 2 && bestN < 2 {
		return 0, [3]float32{}, false
	}
	return best.addr, best.pos, true
}

// consensusCopy 冻结共识兜底（V2.2.0 Q7 自 live-go 1.8.7 移植）：
// 在锚块上的 known 偏移里找与当前锁（exclude）不同、≥consensusMin 成员一致
// （两两欧氏距离 <2m）的簇作候选。调用方要求候选"动了"（活副本证明）才换锁，
// 玩家站桩绝不误换。仅锚块（live-go currentSessionBase 同口径）。
func consensusCopy(h uintptr, exclude uintptr) (uintptr, [3]float32, bool) {
	base := guestRamBase()
	if base == 0 {
		return 0, [3]float32{}, false
	}
	type val struct {
		addr uintptr
		pos  [3]float32
	}
	var vals []val
	for _, o := range currentGame.KnownOffsets(currentPlatform()) {
		adr := base + o
		if adr == exclude {
			continue
		}
		if d := currentGame.DecodeAt(h, adr); d != nil {
			vals = append(vals, val{adr, [3]float32{d[0], d[1], d[2]}})
		}
	}
	best, bestN := val{}, 0
	for _, x := range vals {
		n := 0
		for _, y := range vals {
			if dist3(x.pos, y.pos) < 2 {
				n++
			}
		}
		if n > bestN {
			best, bestN = x, n
		}
	}
	if bestN < consensusMin {
		return 0, [3]float32{}, false
	}
	return best.addr, best.pos, true
}

var (
	knownBlocks        []uintptr
	currentSessionBase uintptr
)

func detectNewBlocks(blocks []MemBlock) []uintptr {
	var newOnes []uintptr
	if len(knownBlocks) == 0 {
		if ref := loadKnownBlock(); ref != 0 {
			still := false
			for _, b := range blocks {
				if b.Base == ref {
					still = true
					break
				}
			}
			if still {
				for _, b := range blocks {
					knownBlocks = append(knownBlocks, b.Base)
				}
				return nil
			}
			for _, b := range blocks {
				knownBlocks = append(knownBlocks, b.Base)
				newOnes = append(newOnes, b.Base)
			}
			return newOnes
		}
		for _, b := range blocks {
			knownBlocks = append(knownBlocks, b.Base)
		}
		return nil
	}
	for _, b := range blocks {
		found := false
		for _, k := range knownBlocks {
			if k == b.Base {
				found = true
				break
			}
		}
		if !found {
			newOnes = append(newOnes, b.Base)
		}
	}
	if len(newOnes) > 0 {
		knownBlocks = newOnes
		return newOnes
	}
	return nil
}

func sessionBlocks(h uintptr) []MemBlock {
	p := currentPlatform()
	if p == nil {
		return nil
	}
	blks := p.Blocks(minBlockMB)
	if currentSessionBase == 0 {
		return blks
	}
	var out []MemBlock
	for _, b := range blks {
		if b.Base == currentSessionBase {
			out = append(out, b)
		}
	}
	if len(out) > 0 {
		return out
	}
	return blks
}

const (
	smTick         = 200 * time.Millisecond
	invalidMax     = 10
	teleportDist   = 300.0
	moveDist       = 0.5
	moveConfirmN   = 3
	scanCooldown   = 15 * time.Second
	knownRetry     = 1 * time.Second
	probeEvery     = 600 * time.Millisecond
	probeMoveN     = 3
	probeLife      = 60 * time.Second
	probeAddrCap   = 24
	probeNearDist  = 50.0
	frozenTicks    = 150
	consensusMin   = 3
	consensusDist  = 15.0
	consensusEvery = 1 * time.Second
	verifyEvery    = 30 * time.Second
)

var (
	rescanCh     chan struct{}
	lastScanAt   time.Time
	lastFixedTry time.Time
	lastMoveAt   time.Time
	lastVerifyAt time.Time
	useSaveScan  = true
)

func setLock(addr uintptr, verified bool, copies int, source string) {
	lock.mu.Lock()
	lock.addr = addr
	lock.verified = verified
	lock.copies = copies
	lock.source = source
	lock.mu.Unlock()
}

func goUnlocked(reason string) {
	setLock(0, false, 0, "locating...")
	state.mu.Lock()
	state.ok = false
	state.source = "locating..."
	state.mu.Unlock()
	lastScanAt = time.Time{}
	fmt.Printf("  [sm] -> UNLOCKED (%s)\n", reason)
}

func requestRescan() {
	select {
	case rescanCh <- struct{}{}:
	default:
	}
}

func dist3(a, b [3]float32) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

func stateMachine() {
	rescanCh = make(chan struct{}, 1)

	inLocked := false
	var prev [3]float32
	hasPrev := false
	moveN := 0
	invalidN := 0
	frozenN := 0
	savedKnown := false
	lastKnownAt := time.Now().Add(-knownRetry)
	lastVerifyAt = time.Now()

	shrineMode := false
	var preJump [3]float32
	var lastJumpAt time.Time

	probing := false
	var probeGroups []ShortlistEntry
	probeBase := map[uintptr][3]float32{}
	var probeMoved []int
	var probeStart, probeLast time.Time
	probeLockGi := -1
	lockSince := time.Now()
	unverifiedN := 0
	lastDataHintAt := time.Now().Add(-time.Minute)

	// 冻结共识兜底状态（V2.2.0 Q7 自 live-go 1.8.7 移植）
	lastConsensusAt := time.Time{}
	consensusCand := uintptr(0) // 冻结共识候选（需"动了"才换锁）
	var consensusCandPos [3]float32
	// Q14：FrozenRescanAfter<=0 = 彻底禁用冻结兜底（BOTW 固定偏移是活副本永不失效，
	// 站立零扫描）。此前阈值 0 时 frozenN>=0 恒真，导致 BOTW/Cemu 锁定后每 60s 强制重扫
	frozenEnabled := currentGame.FrozenRescanAfter() > 0
	frozenLimit := int(currentGame.FrozenRescanAfter() / smTick)
	// Q10 根治：冻结 → 后台重扫（不解锁、不清显示）。站立不动时副本无写入属正常
	// （TOTK 只在移动时写位置），解锁重扫会把红点甩到副本数最多的静态结构上。
	// 后台扫描+探针：玩家真移动时探针自动切活副本（mv≥3），站立时什么都不变。
	bgScan := false
	var lastBgScanAt time.Time

	// 探针逐地址移动计数（V2.2.0 Q7）：位置历史是环形缓冲，同一组里 Addrs[0]
	// 可能是冻结的旧条目，实时条目在其他下标——换锁必须锁"组内动得最多的地址"，
	// 否则锁到旧槽位（每 ~30s 环回写一次才更新，表现为红点 10~30s 才跳一次）
	probeAddrMoves := map[uintptr]int{}

	resetFollow := func() {
		prev, hasPrev = [3]float32{}, false
		moveN, invalidN, frozenN = 0, 0, 0
		savedKnown = false
		consensusCand = 0
	}

	tick := time.NewTicker(smTick)
	defer tick.Stop()
	genN := 0
	tickN := 0
	zeroBlockN := 0
	lastZeroProbe := time.Now()

	for range tick.C {
		if !ensureAttach() {
			if inLocked {
				inLocked = false
				goUnlocked("emulator lost")
			} else {
				state.mu.Lock()
				state.ok = false
				state.source = "waiting for emulator"
				state.mu.Unlock()
			}
			continue
		}
		p := currentPlatform()
		h := p.Handle()
		procPID = p.PID()

		genN++
		// Ryujinx guest 块动态重映射，新块检测不可靠，禁用
		_ = genN

		// —— 0 块检测：游戏未加载/切换模拟器 → 自动重探测平台（仅 --emu=auto）——
		// 每 5 tick（≈1s）检查当前平台是否还有游戏内存（≥256MB 块）；连续 5 次
		// （≈5s）0 块且另一平台已加载游戏时自动切换。修两个真机 bug：
		// ① 传送/重映射瞬间 0 块卡死约 30s（配枚举容错后此处为兜底切换）；
		// ② Ryujinx 进程活着但游戏已关时永远切不到 Cemu。
		tickN++
		if autoPlatform && !inLocked && tickN%5 == 0 {
			if len(p.Blocks(256.0)) == 0 {
				if zeroBlockN < 5 {
					zeroBlockN++
				}
				if zeroBlockN >= 5 && time.Since(lastZeroProbe) >= 15*time.Second {
					lastZeroProbe = time.Now()
					if alt := platformWithGame(p); alt != nil {
						fmt.Printf("  [platform] %s 0 blocks for ~%ds, auto-switching to %s (game detected)\n",
							p.Name(), zeroBlockN, alt.Name())
						setPlatform(alt)
						currentSessionBase = 0
						knownBlocks = nil
						zeroBlockN = 0
						goUnlocked("platform auto-switched")
						p = alt
						h = alt.Handle()
						procPID = alt.PID()
					}
				}
			} else {
				zeroBlockN = 0
			}
		}

		select {
		case <-rescanCh:
			lastScanAt = time.Time{}
			if inLocked {
				inLocked = false
				goUnlocked("/rescan requested")
			}
		default:
		}

		// ---- UNLOCKED：解锁策略因游戏而异 ----
		if !inLocked {
			// 1) 固定偏移秒锁（仅 BOTW：偏移跨重启验证过。TOTK 走扫描优先——
			//    偏移是会话快照无一持续跟随，秒锁只会锁到僵尸，见 PreferScanOnUnlock）
			if !currentGame.PreferScanOnUnlock() && len(currentGame.KnownOffsets(p)) > 0 && time.Since(lastFixedTry) > 2*time.Second {
				offsets := currentGame.KnownOffsets(p)
				lastFixedTry = time.Now()
				blks := p.Blocks(256.0)
				fmt.Printf("  [sm] trying fixed offsets on %d blocks:\n", len(blks))
				var cands []OffsetCand
				for _, blk := range blks {
					for _, off := range offsets {
						addr := blk.Base + off
						d := currentGame.DecodeAt(h, addr)
						if d == nil {
							// 区分"读内存失败"与"读到但坐标非法"，原始字节直接进日志，
							// 便于判断游戏是否处于传送/加载的数据异常期。
							if raw := readMem(h, addr, 12); len(raw) < 12 {
								fmt.Printf("    try base=0x%X+0x%X -> RPM failed\n", blk.Base, off)
							} else {
								fmt.Printf("    try base=0x%X+0x%X -> invalid coords hex=% X\n", blk.Base, off, raw)
							}
							continue
						}
						ok := currentGame.ValidRaw(d, p.LittleEndian())
						fmt.Printf("    try base=0x%X+0x%X -> (%.1f, %.1f, %.1f) valid=%v\n", blk.Base, off, d[0], d[1], d[2], ok)
						if ok {
							cands = append(cands, OffsetCand{addr, [3]float32{d[0], d[1], d[2]}})
						}
					}
				}
				if len(cands) > 0 {
					best, detail, ok := currentGame.PickOffset(cands, p)
					if ok {
						needConfirm := currentGame.FixedNeedsMoveConfirm()
						fmt.Printf("  [sm] fixed offset -> lock 0x%X mem=(%.1f, %.1f, %.1f) %s\n",
							best.Addr, best.Pos[0], best.Pos[1], best.Pos[2], detail)
						setLock(best.Addr, !needConfirm, 0, "fixed-offset")
						inLocked = true
						resetFollow()
						lockSince = time.Now() // 新锁重新计时，否则 15s unverified 保险丝用旧时间戳秒杀新锁
						if needConfirm {
							fmt.Printf("  [sm] fixed offset locked OK [pending move confirm]\n")
						} else {
							fmt.Printf("  [sm] fixed offset locked OK\n")
						}
						continue
					}
					fmt.Printf("  [sm] fixed offset rejected: %s\n", detail)
				}
				fmt.Printf("  [sm] fixed offset not valid on %d guest blocks, falling back to scan\n", len(blks))
				if time.Since(lastDataHintAt) > 15*time.Second {
					lastDataHintAt = time.Now()
					fmt.Printf("  [sm]   hint: 坐标数据异常（游戏传送/加载中?），游戏恢复后固定偏移会自动锁定；如长时间不恢复请检查游戏是否卡死\n")
				}
			}
			// 2) 固定偏移失败，走全量结构扫描（不依赖存档锚点）
			if useSaveScan && time.Since(lastScanAt) >= scanCooldown {
				lastScanAt = time.Now()
				blocks := sessionBlocks(h)
				logf := func(s string) { fmt.Println("    " + s) }
				fmt.Println("  [sm] structural scan (no anchor window)...")
				hits := structuralScan(h, blocks, logf)
				if len(hits) > 0 {
					ranked, shortlist := groupAndRank(h, hits)
					for i := 0; i < len(ranked) && i < 10; i++ {
						g := ranked[i]
						logf(fmt.Sprintf("    rank %d: x%-5d struct %2d  mem=(%.1f, %.1f, %.1f)",
							i+1, g.Copies, g.Struct, g.X, g.Y, g.Z))
					}
					best := ranked[0]
					addr := best.Addrs[0]
					setLock(addr, false, best.Copies, "scan")
					inLocked = true
					resetFollow()
					lockSince = time.Now()
					probing = true
					probeGroups = shortlist
					probeMoved = make([]int, len(shortlist))
					probeBase = map[uintptr][3]float32{}
					probeAddrMoves = map[uintptr]int{}
					probeStart = time.Now()
					probeLast = time.Now()
					probeLockGi = 0
					fmt.Printf("  [sm] scan -> lock 0x%X copies=%d [probing top %d, waiting move confirm]\n",
						addr, best.Copies, len(shortlist))
				} else {
					fmt.Println("  [sm] structural scan found nothing - retrying")
				}
				continue
			}
			if time.Since(lastKnownAt) >= knownRetry {
				lastKnownAt = time.Now()
				if a, v, ok := tryOffsets(); ok {
					setLock(a, false, 0, "known")
					inLocked = true
					resetFollow()
					lockSince = time.Now() // 同固定偏移路径：新锁重新计时
					probing = false
					probeGroups = nil
					fmt.Printf("  [sm] known offset -> lock 0x%X mem=(%.1f, %.1f, %.1f) [pending move confirm]\n",
						a, v[0], v[1], v[2])
				}
			}
			continue
		}

		// ---- LOCKED ----
		lock.mu.RLock()
		a := lock.addr
		lock.mu.RUnlock()
		if a == 0 {
			inLocked = false
			continue
		}

		if probing && time.Since(probeLast) >= probeEvery {
			probeLast = time.Now()
			for gi, g := range probeGroups {
				n := len(g.Addrs)
				if n > probeAddrCap {
					n = probeAddrCap
				}
				for i := 0; i < n; i++ {
					adr := g.Addrs[i]
					d := currentGame.DecodeAt(h, adr)
					if d == nil {
						continue
					}
					v := [3]float32{d[0], d[1], d[2]}
					b, ok := probeBase[adr]
					probeBase[adr] = v
					if ok && dist3(v, b) > moveDist {
						probeMoved[gi]++
						probeAddrMoves[adr]++
					}
				}
			}
			lock.mu.RLock()
			ver := lock.verified
			lock.mu.RUnlock()
			_ = ver
			// Q10：verified 锁也允许探针切换（后台重扫后的自愈通道）——
			// 切换后走 pending move confirm 重新验证，误切会被移动确认淘汰
			bestGi, bestMv := -1, 0
				for gi, mv := range probeMoved {
					if mv >= probeMoveN && gi != probeLockGi && mv > bestMv {
						bestGi, bestMv = gi, mv
					}
				}
				if bestGi >= 0 {
					// Q7：锁"组内移动最多的地址"而非 Addrs[0]——位置历史环形缓冲里
					// Addrs[0] 可能是冻结旧条目（每 ~30s 环回才更新），实时条目在其他下标
					adr := probeGroups[bestGi].Addrs[0]
					bestAdrMv := probeAddrMoves[adr]
					nCap := len(probeGroups[bestGi].Addrs)
					if nCap > probeAddrCap {
						nCap = probeAddrCap
					}
					for i := 0; i < nCap; i++ {
						ad := probeGroups[bestGi].Addrs[i]
						if m := probeAddrMoves[ad]; m > bestAdrMv {
							bestAdrMv, adr = m, ad
						}
					}
					if d := currentGame.DecodeAt(h, adr); d != nil {
						// 哪个在动就切到哪个（不限制距离，因为锁的位置可能本身就是错的静态候选）
						// 切到后不直接 verified，等移动确认
						setLock(adr, false, probeGroups[bestGi].Copies, "probe")
						probing = false
						resetFollow()
						probeAddrMoves = map[uintptr]int{}
						a = adr
						lockSince = time.Now()
						fmt.Printf("  [sm] probe -> switched to moving group #%d copies=%d addr=0x%X (addrMv=%d) mem=(%.1f,%.1f,%.1f) [pending move confirm]\n",
							bestGi, probeGroups[bestGi].Copies, adr, bestAdrMv, d[0], d[1], d[2])
					}
				}
			if probing && time.Since(probeStart) > probeLife {
				probing = false
				// Q13：一轮完整观察周期结束 → 冻结计数清零。站立不动时 500 组
				// 全无移动属正常（副本只在移动时写入），清零避免后台重扫反复触发；
				// 玩家动起来后下一个 90s 冻结周期内的探针会抓到移动组
				frozenN = 0
				// Q7 诊断：探针到期仍没切组——打印有移动计数的组，判断活组是否在观察名单
				moved := 0
				for gi, mv := range probeMoved {
					if mv > 0 && gi < len(probeGroups) {
						moved++
						if d := currentGame.DecodeAt(h, probeGroups[gi].Addrs[0]); d != nil {
							fmt.Printf("  [sm] probe expired: group #%d mv=%d copies=%d mem=(%.1f,%.1f,%.1f)\n",
								gi, mv, probeGroups[gi].Copies, d[0], d[1], d[2])
						}
					}
				}
				if moved == 0 {
					fmt.Printf("  [sm] probe expired: %d groups watched, none moved\n", len(probeGroups))
				}
			}
		}

		lock.mu.RLock()
		a = lock.addr
		lock.mu.RUnlock()
		if a == 0 {
			inLocked = false
			continue
		}

		// Q10：冻结后台重扫——保持当前锁和位置显示，只跑扫描+探针。
		// 玩家真移动时活副本组 mv≥3 自动切换；站立不动时扫描无果、什么都不变。
		if bgScan && time.Since(lastBgScanAt) >= 60*time.Second {
			bgScan = false
			lastBgScanAt = time.Now()
			logf := func(s string) { fmt.Println("    " + s) }
			fmt.Println("  [sm] frozen -> background rescan (lock kept)...")
			hits := structuralScan(h, sessionBlocks(h), logf)
			if len(hits) > 0 {
				_, shortlist := groupAndRank(h, hits)
				probing = true
				probeGroups = shortlist
				probeMoved = make([]int, len(shortlist))
				probeBase = map[uintptr][3]float32{}
				probeAddrMoves = map[uintptr]int{}
				probeStart = time.Now()
				probeLast = time.Now()
				probeLockGi = -1
				for gi := range shortlist {
					if len(shortlist[gi].Addrs) > 0 && shortlist[gi].Addrs[0] == a {
						probeLockGi = gi // 当前锁自己的组：探针不切到自己
						break
					}
				}
				fmt.Printf("  [sm] background rescan done, probing top %d (lock kept)\n", len(shortlist))
			}
		}

		v := currentGame.DecodeAt(h, a)
		if v == nil {
			invalidN++
			state.mu.Lock()
			state.ok = false
			state.source = "address lost (waiting for restore)"
			state.mu.Unlock()
			// 进神庙/骑乘时地址暂时读不到，永远保持最后位置不飘，等游戏恢复
			// 不重扫，不飘红点
			_ = invalidMax
			continue
		}
		invalidN = 0
		cur := [3]float32{v[0], v[1], v[2]}

		// 锁了 15 秒还没 verified，说明锁错地址了（坐标不随动），自动重扫
		lock.mu.RLock()
		ver := lock.verified
		lock.mu.RUnlock()
		if ua := currentGame.UnverifiedRescanAfter(); ua > 0 && !ver && time.Since(lockSince) > ua {
			unverifiedN++
			fmt.Printf("  [sm] lock 0x%X unverified after 15s -> rescan\n", a)
			if unverifiedN >= 3 {
				unverifiedN = 0
				fmt.Printf("  [sm]   hint: 连续多轮锁定后坐标未移动——玩家可能站在原地或游戏冻结；\n")
				fmt.Printf("  [sm]         在游戏内走动几步可触发移动确认；仍无效请检查游戏是否卡死\n")
			}
			inLocked = false
			goUnlocked("unverified timeout")
			continue
		}

		if shrineMode {
			// 统一序：cur 为展示序 (X, alt, Z)，出神庙判定用水平轴 X/Z
			if abs32(cur[0]) > currentGame.ShrineExit() || abs32(cur[2]) > currentGame.ShrineExit() {
				fmt.Printf("  [shrine] left shrine -> normal tracking\n")
				shrineMode = false
			} else {
				state.mu.Lock()
				state.ok = true
				state.age = float64(time.Now().UnixNano()) / 1e9
				state.source = "shrine"
				state.mu.Unlock()
				continue
			}
		}

		delta := dist3(cur, prev)

		if hasPrev && delta > teleportDist {
			lock.mu.RLock()
			ver := lock.verified
			lock.mu.RUnlock()
			if ver {
				incCounter("jumps")
				frozenN = 0
				preJump = prev
				lastJumpAt = time.Now()
				if pnt := currentGame.MatchShrine(cur[0], cur[1], cur[2]); pnt != nil {
					shrineMode = true
					fmt.Printf("  [shrine] entered %s\n", pnt.Cn)
				}
			} else {
				inLocked = false
				goUnlocked(fmt.Sprintf("teleport jump %.0fm on unverified -> rescan", delta))
				continue
			}
		}

		shrineWin, shrineDist := currentGame.ShrineReturn()
		if !lastJumpAt.IsZero() && time.Since(lastJumpAt) < shrineWin &&
			dist3(cur, preJump) < shrineDist {
			cur = preJump
			lastJumpAt = time.Time{}
		}

		if hasPrev && delta > moveDist {
			moveN++
			frozenN = 0
			lastMoveAt = time.Now()
			if moveN >= moveConfirmN {
				lock.mu.Lock()
				if !lock.verified {
					fmt.Printf("  [sm] lock 0x%X confirmed live (moving)\n", a)
					lock.verified = true
				}
				lock.mu.Unlock()
				probing = false
				if !savedKnown {
					savedKnown = true
					saveKnown([]uintptr{a}, guestRamBase(), [3]float32{cur[0], cur[1], cur[2]})
					dumpDiagnostic(a, cur)
				}
			}
		} else {
			moveN = 0
			if hasPrev {
				frozenN++
			}
		}

		lock.mu.RLock()
		verified, copies, src := lock.verified, lock.copies, lock.source
		lock.mu.RUnlock()
		gx, gy, gz := currentGame.Display(cur)
		state.mu.Lock()
		state.ok = true
		state.gx, state.gy, state.gz = gx, gy, gz
		state.mx, state.my = currentGame.ToMap(gx, gy, gz)
		state.layer = currentGame.LayerOf(gx, gy, gz)
		state.age = float64(time.Now().UnixNano()) / 1e9
		state.verified = verified
		state.copies = copies
		state.source = src
		state.mu.Unlock()
		prev = cur
		hasPrev = true

		// TOTK layer 日志（天空20/地面18/地底19）：变化或超 30s 打一次
		if currentGame.Name() == "totk" {
			lay := currentGame.LayerOf(gx, gy, gz)
			if lay != lastLayerLog || time.Since(lastLayerLogAt) > 30*time.Second {
				lastLayerLog = lay
				lastLayerLogAt = time.Now()
				fmt.Printf("  [layer] %d (%s) @ (%.0f, %.0f, %.0f)\n", lay, totkLayerName(lay), gx, gy, gz)
			}
		}

		// 冻结共识兜底（V2.2.0 Q7 自 live-go 1.8.7 移植，零扫描）：锁读数停滞 ≥30s
		// （frozenN≥frozenTicks）时，在锚块 known 偏移里找与本锁差 >15m 的
		// ≥consensusMin 成员一致簇作候选；候选在下一次检查"动了"（活副本证明）
		// 才换锁——玩家站桩绝不误换，玩家一动 1~2s 内自愈。
		if frozenEnabled && frozenN >= frozenLimit && time.Since(lastConsensusAt) >= consensusEvery {
			lastConsensusAt = time.Now()
			a2, v2, ok := consensusCopy(h, a)
			if !ok {
				consensusCand = 0
				// Q10：冻结且无共识候选 → 后台重扫（不解锁）。
				// 站立不动时副本无写入属正常；玩家真移动时探针会找到活副本并切换。
				// 节流：与上次后台重扫间隔 ≥60s 才再次置位（防日志每秒刷屏）
				if time.Since(lastBgScanAt) >= 60*time.Second {
					fmt.Printf("  [sm] lock frozen %.0fs -> background rescan (lock kept)\n",
						float64(frozenN)*smTick.Seconds())
					bgScan = true
				}
				continue
			} else if a2 != consensusCand {
				consensusCand, consensusCandPos = a2, v2
				fmt.Printf("  [sm] lock frozen %.0fs, consensus candidate 0x%X (%.0fm away) - watching\n",
					float64(frozenLimit)*smTick.Seconds(), a2, dist3(v2, cur))
			} else if dist3(v2, consensusCandPos) > moveDist {
				// 候选位置变了 = 活副本在跟随玩家 → 换锁
				fmt.Printf("  [sm] frozen lock 0x%X -> relock to live consensus copy 0x%X\n", a, a2)
				setLock(a2, false, 0, "consensus")
				resetFollow()
				probing = false
				probeGroups = nil
				continue
			}
		}

		// verified 后不做站桩核对：玩家站着不动时全量扫描会找到其他 Actor，误杀正确锁
		// 只有读数失效才重扫（上面 invalidN 逻辑已处理）
		_ = lastVerifyAt
	}
}

func setTarget(t *targetT) {
	targetMu.Lock()
	target = t
	targetMu.Unlock()
}

func getTarget() *targetT {
	targetMu.Lock()
	defer targetMu.Unlock()
	return target
}

func clearTarget() {
	targetMu.Lock()
	defer targetMu.Unlock()
	target = nil
}
