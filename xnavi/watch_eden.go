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
	addr uintptr
	init [3]float32
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
	buf := readMem(h, addr, 12)
	if len(buf) < 12 {
		return [3]float32{}, false
	}
	x := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(buf[4:]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(buf[8:]))
	if !WorldCoordOK(x, y, z) {
		return [3]float32{}, false
	}
	return [3]float32{x, y, z}, true
}

func (t *EdenTracker) Start(ctx context.Context) {
	fmt.Println("  [pipeline] Eden pipeline started (enavi minimal core: Run>=2 gate, warp zero-rescan, shrine hold)")

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
	lockSince := time.Time{}
	lastUnverifiedHint := time.Time{}

	reset := func(reason string) {
		lockedAddr = 0
		hasPrev = false
		verified = false
		moveN = 0
		shrineMode = false
		cands = nil
		holdLogged = false
		lockSince = time.Time{}
		lastUnverifiedHint = time.Time{}
		goUnlocked(reason)
	}

	scanNear := func(center [3]float32, radius float32) bool {
		h := t.p.Handle()
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

		// 门禁①：坐标通过 WorldCoordOK
		// 门禁②：硬门槛 Run >= 2（真林克专属 RingBuffer 特征，彻底排除静态数据）
		var identHits []EdenHit
		for _, hit := range allHits {
			if !WorldCoordOK(hit.P[0], hit.P[1], hit.P[2]) {
				continue
			}
			if hit.Run >= 2 {
				identHits = append(identHits, hit)
			}
		}
		if len(identHits) == 0 {
			return false
		}

		groups := GroupEdenHits(identHits)
		if len(groups) == 0 {
			return false
		}

		best := groups[0]
		lockedAddr = best.Addr
		cur = best.P
		prev = cur
		hasPrev = true
		verified = false
		moveN = 0
		shrineMode = false
		holdLogged = false
		lockSince = time.Now()
		lastUnverifiedHint = time.Time{}

		var slots []candSlot
		for i := 0; i < len(groups) && i < 5; i++ {
			slots = append(slots, candSlot{addr: groups[i].Addr, init: groups[i].P})
		}
		cands = slots

		setLock(lockedAddr, false, len(groups), "eden-scan")
		publishState(true, cur[0], cur[1], cur[2], false, len(groups), "eden-scan")
		fmt.Printf("  [lock] Eden locked at 0x%X (%.1f, %.1f, %.1f) run=%d dist=%.1fm copies=%d\n",
			lockedAddr, cur[0], cur[1], cur[2], best.Run, best.Dist, len(groups))
		return true
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("  [pipeline] Eden pipeline stopped (context cancelled)")
			return
		case <-t.stopCh:
			fmt.Println("  [pipeline] Eden pipeline stopped (stop requested)")
			return
		case req := <-t.rescanCh:
			reset("Eden /rescan requested")
			if req != nil && req.HasTarget {
				hasManual = true
				manualTarget = [3]float32{req.TargetX, 0, req.TargetZ}
				fmt.Printf("  [track] Eden manual target set: (%.1f, %.1f)\n", req.TargetX, req.TargetZ)
			}
		case <-tick.C:
			// 1. 确保模拟器附加
			if !t.p.Attach() {
				if lockedAddr != 0 {
					fmt.Println("  [lost] Eden emulator process exited or detached")
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
					fmt.Printf("  [track] Eden scanning manual window 150m near (%.1f, %.1f)...\n", scanCenter[0], scanCenter[2])
				case latestSaveAnchor != nil:
					// 始终以最新存档锚点为中心扫描（绝不拿切场景掉锁前的旧废弃坐标当中心）
					scanCenter = latestSaveAnchor.Pos
					radius = 120.0
					if saveIsFresh {
						fmt.Printf("  [track] Eden fresh save detected (<=120s), scanning window 120m near (%.1f, %.1f, %.1f)...\n",
							scanCenter[0], scanCenter[1], scanCenter[2])
					} else {
						fmt.Printf("  [track] Eden scanning save anchor window 120m near (%.1f, %.1f, %.1f)...\n",
							scanCenter[0], scanCenter[1], scanCenter[2])
					}
				default:
					state.mu.Lock()
					state.ok = false
					state.source = "waiting for save anchor (press [+] in game)"
					state.mu.Unlock()
					continue
				}

				found := scanNear(scanCenter, radius)
				if !found && !hasManual {
					found = scanNear(scanCenter, 400.0)
				}
				if !found && !hasManual {
					found = scanNear(scanCenter, 3000.0)
				}
				if found {
					hasManual = false
				} else {
					fmt.Println("  [track] Eden scan found no live player (waiting for 3D world)...")
				}
				continue
			}

			// 3. LOCKED：死守 lockedAddr，过图黑屏平滑保持，落地直接接管，绝不主动掉锁
			d, ok := readEdenActor(h, lockedAddr)
			if !ok {
				for i := 0; i < 3; i++ {
					time.Sleep(30 * time.Millisecond)
					if d, ok = readEdenActor(h, lockedAddr); ok {
						break
					}
				}
			}

			// 读不到标准三元组（过图黑屏 / 场景切换过渡期 / 瞬态读空）
			if !ok {
				if shrineMode {
					// 神庙内黑屏或瞬态：死死钉在神庙入口
					publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
					continue
				}

				if !holdLogged {
					fmt.Println("  [hold] Eden reading transient/loading -> holding position")
					holdLogged = true
				}
				if hasPrev {
					publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
				}
				continue
			}

			// 读出合法世界坐标
			holdLogged = false
			cur := d
			isShrine := isShrineScale(cur)

			// 神庙内部试炼场本地尺度挂起
			if shrineMode {
				if !isShrine {
					// 出庙：脱离神庙本地尺度，恢复大世界跟踪
					shrineMode = false
					destName := formatDestName(cur)
					fmt.Printf("  [shrine] Eden left shrine -> normal tracking (%.1f, %.1f, %.1f) [%s]\n",
						cur[0], cur[1], cur[2], destName)
					publishState(true, cur[0], cur[1], cur[2], verified, len(cands), "player")
					prev = cur
					hasPrev = true
					continue
				}
				// 还在神庙内：死锁保护，红点死死钉在神庙入口！
				publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
				continue
			} else if isShrine && (!hasPrev || !isShrineScale(prev)) && dist3(cur, prev) > 100.0 {
				// 进庙：世界尺度 → 神庙本地尺度，红点钉在进入前的门口 prev
				shrineMode = true
				shrineEntrance = prev
				destName := formatDestName(prev)
				fmt.Printf("  [shrine] Eden entered %s -> holding entrance (%.1f, %.1f, %.1f)\n",
					destName, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2])
				publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
				continue
			}

			// 多候选走动仲裁（防驿站等多 NPC 场景误锁静止 NPC）
			if !verified && len(cands) > 1 {
				for _, c := range cands {
					if c.addr == lockedAddr {
						continue
					}
					if cd, okCd := readEdenActor(h, c.addr); okCd {
						dCand := dist3(cd, c.init)
						if dCand >= 0.5 && dCand < 150.0 {
							lockedAddr = c.addr
							cur = cd
							verified = true
							cands = nil
							setLock(lockedAddr, true, 1, "eden-move-confirm")
							fmt.Printf("  [track] candidate arbitration -> locked live player at 0x%X (%.1f, %.1f, %.1f)\n",
								lockedAddr, cur[0], cur[1], cur[2])
							break
						}
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
				fmt.Printf("  [warp] Eden warped (dist=%.1fm) -> %s (%.1f, %.1f, %.1f)\n",
					delta, destName, cur[0], cur[1], cur[2])
				publishState(true, cur[0], cur[1], cur[2], verified, 1, "player")
				prev = cur
				continue
			}

			// 单帧位移异常过大（>150m 非传送跳变）
			if hasPrev && delta > 150.0 {
				publishState(true, prev[0], prev[1], prev[2], verified, len(cands), "hold")
				continue
			}

			// 正常跟随与移动确认
			if hasPrev && delta >= 0.5 {
				moveN++
				if moveN >= 3 && !verified {
					verified = true
					cands = nil
					setLock(lockedAddr, true, 1, "eden-live")
					edenSetRefCenter(cur)
					fmt.Printf("  [track] lock 0x%X confirmed live (moving)\n", lockedAddr)
				}
			} else {
				moveN = 0
			}

			// 提示机制：若锁定后持续未确认走动，友好提醒用户
			if !verified && time.Since(lockSince) > 6*time.Second && time.Since(lastUnverifiedHint) > 15*time.Second {
				lastUnverifiedHint = time.Now()
				fmt.Println("  [hint] 主角未确认走动（verified=false）。请走动几步；若未跟随，请按 [+] 手工存档并点击【重新扫描】。")
			}

			publishState(true, cur[0], cur[1], cur[2], verified, len(cands), "player")
			prev = cur
			hasPrev = true
		}
	}
}
