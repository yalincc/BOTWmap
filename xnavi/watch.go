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
		if d := decodeTripleAt(h, base+o); d != nil {
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

	resetFollow := func() {
		prev, hasPrev = [3]float32{}, false
		moveN, invalidN, frozenN = 0, 0, 0
		savedKnown = false
	}

	tick := time.NewTicker(smTick)
	defer tick.Stop()
	genN := 0

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

		select {
		case <-rescanCh:
			lastScanAt = time.Time{}
			if inLocked {
				inLocked = false
				goUnlocked("/rescan requested")
			}
		default:
		}

		// ---- UNLOCKED：固定偏移优先，失败再扫描 ----
		if !inLocked {
			// 1) 先试平台已知固定偏移（秒锁，不扫描）
			if offsets := p.KnownOffsets(); len(offsets) > 0 {
				blks := p.Blocks(512.0) // 和扫描用同一组 guest 块
				fmt.Printf("  [sm] trying fixed offsets on %d blocks:\n", len(blks))
				for i, blk := range blks {
					fmt.Printf("    block[%d] base=0x%X size=%d MB\n", i, blk.Base, blk.Size/1048576)
				}
				fixedOK := false
				for _, blk := range blks {
					for _, off := range offsets {
						addr := blk.Base + off
						d := decodeTripleAt(h, addr)
						fmt.Printf("    try base=0x%X+0x%X=0x%X -> ", blk.Base, off, addr)
						if d != nil {
							fmt.Printf("(%.1f, %.1f, %.1f)\n", d[0], d[1], d[2])
						} else {
							fmt.Println("read failed")
						}
					}
				}
				// 选离存档锚点最近的那个
				anchors := readSaveAnchors(p)
				if len(anchors) > 0 {
					savePt := anchors[0].Pos // (X, alt, Z)
					bestDist := float32(1e9)
					bestAddr := uintptr(0)
					var bestD [3]float32
					for _, blk := range blks {
						for _, off := range offsets {
							addr := blk.Base + off
							d := decodeTripleAt(h, addr)
							if d == nil {
								continue
							}
							// 对 Ryujinx 小端，d=(X, Z, alt)；对 Cemu 大端，d=(X, alt, Z)
							var ok bool
							if p.LittleEndian() {
								ok = gameValidTriple(d[0], d[2], d[1])
							} else {
								ok = gameValidTriple(d[0], d[1], d[2])
							}
							if !ok {
								continue
							}
							// 对 Ryujinx：d=(X, Z, alt)；对 Cemu：d=(X, alt, Z)
							var dd float32
							if p.LittleEndian() {
								ddx := d[0] - savePt[0]
								ddalt := d[2] - savePt[1]
								ddz := d[1] - savePt[2]
								dd = float32(math.Sqrt(float64(ddx*ddx + ddalt*ddalt + ddz*ddz)))
							} else {
								ddx := d[0] - savePt[0]
								ddalt := d[1] - savePt[1]
								ddz := d[2] - savePt[2]
								dd = float32(math.Sqrt(float64(ddx*ddx + ddalt*ddalt + ddz*ddz)))
							}
							fmt.Printf("    score base=0x%X dist=%.0fm\n", blk.Base, dd)
							if dd < bestDist {
								bestDist = dd
								bestAddr = addr
								bestD = [3]float32{d[0], d[1], d[2]}
							}
						}
					}
					if bestAddr != 0 {
						fmt.Printf("  [sm] fixed offset -> lock 0x%X mem=(%.1f, %.1f, %.1f) dist=%.0fm [confirmed]\n",
							bestAddr, bestD[0], bestD[1], bestD[2], bestDist)
						setLock(bestAddr, true, 0, "fixed-offset")
						fixedOK = true
					}
				}
				if fixedOK {
					continue
				}
				fmt.Printf("  [sm] fixed offset not valid on %d guest blocks, falling back to scan\n", len(blks))
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
					d := decodeTripleAt(h, adr)
					if d == nil {
						continue
					}
					v := [3]float32{d[0], d[1], d[2]}
					b, ok := probeBase[adr]
					probeBase[adr] = v
					if ok && dist3(v, b) > moveDist {
						probeMoved[gi]++
					}
				}
			}
			lock.mu.RLock()
			ver := lock.verified
			lock.mu.RUnlock()
			if ver {
				probing = false
			} else {
				bestGi, bestMv := -1, 0
				for gi, mv := range probeMoved {
					if mv >= probeMoveN && gi != probeLockGi && mv > bestMv {
						bestGi, bestMv = gi, mv
					}
				}
				if bestGi >= 0 {
					adr := probeGroups[bestGi].Addrs[0]
					if d := decodeTripleAt(h, adr); d != nil {
						// 哪个在动就切到哪个（不限制距离，因为锁的位置可能本身就是错的静态候选）
						// 切到后不直接 verified，等移动确认
						setLock(adr, false, probeGroups[bestGi].Copies, "probe")
						probing = false
						resetFollow()
						a = adr
						lockSince = time.Now()
						fmt.Printf("  [sm] probe -> switched to moving group #%d copies=%d addr=0x%X mem=(%.1f,%.1f,%.1f) [pending move confirm]\n",
							bestGi, probeGroups[bestGi].Copies, adr, d[0], d[1], d[2])
					}
				}
			}
			if probing && time.Since(probeStart) > probeLife {
				probing = false
			}
		}

		lock.mu.RLock()
		a = lock.addr
		lock.mu.RUnlock()
		if a == 0 {
			inLocked = false
			continue
		}
		v := decodeTripleAt(h, a)
		if v == nil {
			invalidN++
			state.mu.Lock()
			state.ok = false
			state.source = "address lost"
			state.mu.Unlock()
			if invalidN >= invalidMax {
				inLocked = false
				goUnlocked(fmt.Sprintf("readings invalid x%d", invalidMax))
			}
			continue
		}
		invalidN = 0
		cur := [3]float32{v[0], v[1], v[2]}

		// 锁了 15 秒还没 verified，说明锁错地址了（坐标不随动），自动重扫
		lock.mu.RLock()
		ver := lock.verified
		lock.mu.RUnlock()
		if !ver && time.Since(lockSince) > 15*time.Second {
			fmt.Printf("  [sm] lock 0x%X unverified after 15s -> rescan\n", a)
			inLocked = false
			goUnlocked("unverified timeout")
			continue
		}

		if shrineMode {
			if abs32(cur[0]) > gameShrineExit || abs32(cur[1]) > gameShrineExit {
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
				if pnt := matchShrine(cur[0], cur[2], cur[1]); pnt != nil {
					shrineMode = true
					fmt.Printf("  [shrine] entered %s\n", pnt.Cn)
				}
			} else {
				inLocked = false
				goUnlocked(fmt.Sprintf("teleport jump %.0fm on unverified -> rescan", delta))
				continue
			}
		}

		if !lastJumpAt.IsZero() && time.Since(lastJumpAt) < gameShrineReturnWindow &&
			dist3(cur, preJump) < gameShrineReturnDist {
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
					saveKnown([]uintptr{a}, guestRamBase(), [3]float32{cur[0], cur[2], cur[1]})
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
		state.mu.Lock()
		state.ok = true
		state.gx, state.gy, state.gz = cur[0], cur[2], cur[1]
		state.mx, state.my = gameToMap(cur[0], cur[2], cur[1])
		state.layer = gameLayer
		state.age = float64(time.Now().UnixNano()) / 1e9
		state.verified = verified
		state.copies = copies
		state.source = src
		state.mu.Unlock()
		prev = cur
		hasPrev = true

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
