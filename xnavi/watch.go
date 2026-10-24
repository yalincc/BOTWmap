// watch.go — xnavi 统一分发调度器（Dispatcher）。
//
// 架构遵循 PLAN-XNAVI-PIPELINE-REBUILD：
// - 识别运行中的模拟器 (Cemu / Ryujinx / Eden)；
// - 管理专属管道 (CemuTracker / RyujinxTracker / EdenTracker) 生命周期；
// - 统一分发 /rescan 请求与全局状态发布。

package main

import (
	"context"
	"fmt"
	"time"
)

var (
	procPID            uint32
	currentSessionBase uintptr
	knownBlocks        []uintptr
)

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

func verifyLock(a uintptr, why string) {
	lock.mu.Lock()
	changed := !lock.verified
	lock.verified = true
	lock.mu.Unlock()
	if changed {
		fmt.Printf("  [sm] lock 0x%X verified by %s\n", a, why)
	}
}

func mountSearchBlocks(p Platform, h uintptr) []MemBlock {
	if ep, ok := p.(*edenPlatform); ok && ep != nil {
		if blks := edenGuestBlocksWithOptions(h, 0.0625, true, ep.pid); len(blks) > 0 {
			return blks
		}
	}
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

func mountFallbackPos(h uintptr, a uintptr, prev [3]float32, hasPrev bool, groups []ShortlistEntry) ([]float32, string) {
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(15 * time.Millisecond)
		}
		if d := currentGame.DecodeAt(h, a); d != nil {
			return d, "player slot"
		}
	}
	if !hasPrev {
		return nil, ""
	}
	var best []float32
	bestD := float32(50.0)
	for gi := range groups {
		g := &groups[gi]
		n := len(g.Addrs)
		if n > 8 {
			n = 8
		}
		for i := 0; i < n; i++ {
			d := currentGame.DecodeAt(h, g.Addrs[i])
			if d == nil {
				continue
			}
			if dd := dist3([3]float32{d[0], d[1], d[2]}, prev); dd < bestD {
				bestD, best = dd, d
			}
		}
	}
	if best != nil {
		return best, "mirror copy"
	}
	return nil, ""
}

// createTrackerForPlatform 为给定平台创建专属管道。
func createTrackerForPlatform(p Platform) Tracker {
	if p == nil {
		return nil
	}
	switch p.Name() {
	case "Cemu":
		if cp, ok := p.(*cemuPlatform); ok {
			return NewCemuTracker(cp)
		}
	case "Ryujinx":
		if rp, ok := p.(*ryujinxPlatform); ok {
			return NewRyujinxTracker(rp)
		}
	case "Eden":
		if ep, ok := p.(*edenPlatform); ok {
			return NewEdenTracker(ep)
		}
	}
	return nil
}

// stateMachine 统一调度分发器。
func stateMachine() {
	fmt.Println("  [sm] dispatcher started, routing platforms to dedicated pipelines")

	var activeTracker Tracker
	var activeCancel context.CancelFunc
	var currentPlatName string

	stopActive := func() {
		if activeCancel != nil {
			activeCancel()
			activeCancel = nil
		}
		if activeTracker != nil {
			activeTracker.Stop()
			activeTracker = nil
		}
	}

	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()

	tickN := 0
	zeroBlockN := 0
	lastZeroProbe := time.Now()
	lastWaitLog := time.Time{}

	for {
		select {
		case req := <-rescanReqCh:
			if activeTracker != nil {
				activeTracker.Rescan(req)
			}
		case <-tick.C:
			tickN++

			// 1. 检查当前平台附加
			if !ensureAttach() {
				if activeTracker != nil {
					stopActive()
					currentPlatName = ""
					goUnlocked("emulator lost")
				}
				state.mu.Lock()
				state.ok = false
				state.source = "waiting for emulator"
				state.mu.Unlock()
				// v3.2.2 诊断：模拟器未启动时用户在日志里看不到任何提示 → 10s 一次指路
				if time.Since(lastWaitLog) >= 10*time.Second {
					lastWaitLog = time.Now()
					fmt.Println("  [wait] 未检测到模拟器 —— 请先启动 Cemu/Ryujinx/Eden 并进入游戏（10 秒后重试）/ No emulator detected — start Cemu/Ryujinx/Eden and enter the game")
				}
				continue
			}

			p := currentPlatform()
			procPID = p.PID()

			// 2. 自动平台切换逻辑（--emu=auto）
			if autoPlatform && tickN%5 == 0 {
				if len(p.Blocks(256.0)) == 0 {
					if zeroBlockN < 5 {
						zeroBlockN++
					}
					if zeroBlockN >= 5 && time.Since(lastZeroProbe) >= 15*time.Second {
						lastZeroProbe = time.Now()
						if alt := platformWithGame(p); alt != nil {
							fmt.Printf("  [platform] %s 0 blocks for ~%ds, auto-switching to %s\n",
								p.Name(), zeroBlockN, alt.Name())
							setPlatform(alt)
							p = alt
							currentSessionBase = 0
							knownBlocks = nil
							zeroBlockN = 0
							EdenClearRefCenter()
							stopActive()
							currentPlatName = ""
							goUnlocked("platform auto-switched")
						}
					}
				} else {
					zeroBlockN = 0
				}
			}

			// 3. 管道实例化与生命周期管理
			platName := p.Name()
			if platName != currentPlatName || activeTracker == nil {
				stopActive()
				currentPlatName = platName
				tracker := createTrackerForPlatform(p)
				if tracker != nil {
					activeTracker = tracker
					var ctx context.Context
					ctx, activeCancel = context.WithCancel(context.Background())
					fmt.Printf("  [dispatcher] launching tracker pipeline: %s\n", tracker.Name())
					go tracker.Start(ctx)
				} else {
					fmt.Printf("  [dispatcher] no tracker found for platform: %s\n", platName)
				}
			}
		}
	}
}
