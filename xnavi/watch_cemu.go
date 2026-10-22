// watch_cemu.go — Cemu 专属追踪管道（Wii U 大端直读，固定偏移死锁）。
//
// 架构设计（做减法，死守锁定 addr，杜绝打补丁与误掉锁）：
// - 固定偏移秒锁：blk.Base + 0x1055300C 经过社区长期验证，游戏运行期恒定；
// - 锁定即死守：除非模拟器进程退出，绝对不重置 lockedAddr；
// - 骑马 / 盾滑 / 摩托保护：原地保持上一有效坐标，绝不丢锁；
// - 出入神庙无缝接管：试炼场内死钉神庙入口，出庙读到大世界坐标秒级恢复追踪；
// - 传送跳变（>300m）：识别地标并打印 [warp] 日志，无缝接管；
// - 全量标准化日志：[attach], [lock], [warp], [shrine], [hold], [lost]。

package main

import (
	"context"
	"fmt"
	"math"
	"time"
)

type CemuTracker struct {
	p        *cemuPlatform
	rescanCh chan *RescanRequest
	stopCh   chan struct{}
}

func NewCemuTracker(p *cemuPlatform) *CemuTracker {
	return &CemuTracker{
		p:        p,
		rescanCh: make(chan *RescanRequest, 4),
		stopCh:   make(chan struct{}),
	}
}

func (t *CemuTracker) Name() string {
	return "botw-cemu"
}

func (t *CemuTracker) Stop() {
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
}

func (t *CemuTracker) Rescan(req *RescanRequest) {
	select {
	case t.rescanCh <- req:
	default:
	}
}

// readCemuActor 直接按大端读取 12 字节并经 WorldCoordOK 校验。
func readCemuActor(h uintptr, addr uintptr) ([3]float32, bool) {
	buf := readMem(h, addr, 12)
	if len(buf) < 12 {
		return [3]float32{}, false
	}
	x := math.Float32frombits(uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]))
	y := math.Float32frombits(uint32(buf[4])<<24 | uint32(buf[5])<<16 | uint32(buf[6])<<8 | uint32(buf[7]))
	z := math.Float32frombits(uint32(buf[8])<<24 | uint32(buf[9])<<16 | uint32(buf[10])<<8 | uint32(buf[11]))
	if !WorldCoordOK(x, y, z) {
		return [3]float32{}, false
	}
	return [3]float32{x, y, z}, true
}

func (t *CemuTracker) Start(ctx context.Context) {
	fmt.Println("  [pipeline] Cemu 追踪管道已启动 (固定偏移 / Wii U 大端直读) / Cemu pipeline started (fixed-offset, Wii U big-endian)")

	tick := time.NewTicker(100 * time.Millisecond) // 10Hz
	defer tick.Stop()

	var lockedAddr uintptr
	var prev [3]float32
	hasPrev := false
	shrineMode := false
	var shrineEntrance [3]float32
	holdLogged := false
	lastScanLogAt := time.Time{}

	reset := func(reason string) {
		lockedAddr = 0
		hasPrev = false
		shrineMode = false
		holdLogged = false
		lastScanLogAt = time.Time{}
		goUnlocked(reason)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("  [pipeline] Cemu 追踪管道已停止 (上下文取消) / Cemu pipeline stopped (context cancelled)")
			return
		case <-t.stopCh:
			fmt.Println("  [pipeline] Cemu 追踪管道已停止 (请求停止) / Cemu pipeline stopped (stop requested)")
			return
		case <-t.rescanCh:
			reset("Cemu /rescan requested")
		case <-tick.C:
			// 1. 确保模拟器附加
			if !t.p.Attach() {
				if lockedAddr != 0 {
					fmt.Println("  [lost] Cemu 模拟器进程断开或退出 / Cemu emulator process exited or detached")
					reset("Cemu lost")
				} else {
					state.mu.Lock()
					state.ok = false
					state.source = "waiting for Cemu"
					state.mu.Unlock()
				}
				continue
			}

			h := t.p.Handle()
			offsets := currentGame.KnownOffsets(t.p)

			// 2. UNLOCKED：固定偏移秒锁（仅在首次启动或进程重启时执行一次）
			if lockedAddr == 0 {
				if time.Since(lastScanLogAt) > 5*time.Second {
					lastScanLogAt = time.Now()
					fmt.Println("  [scan] Cemu 尝试读取动力学根地址 0x1055300C (等待 3D 场景加载) / Reading fixed root 0x1055300C (waiting for 3D world)...")
				}
				blks := t.p.Blocks(256.0)
				for _, blk := range blks {
					for _, off := range offsets {
						addr := blk.Base + off
						d, ok := readCemuActor(h, addr)
						if !ok {
							for i := 0; i < 3; i++ {
								time.Sleep(30 * time.Millisecond)
								if d, ok = readCemuActor(h, addr); ok {
									break
								}
							}
						}
						if ok {
							lockedAddr = addr
							prev = d
							hasPrev = true
							shrineMode = false
							holdLogged = false
							setLock(lockedAddr, true, 1, "cemu-fixed")
							fmt.Printf("  [lock] Cemu 固定偏移锁定成功！Addr: 0x%X (%.1f, %.1f, %.1f) [无需移动验证，已完全锁定] / Cemu fixed root locked! Addr: 0x%X [Verified, fully locked]\n",
								lockedAddr, d[0], d[1], d[2], lockedAddr)
							publishState(true, d[0], d[1], d[2], true, 1, "cemu-fixed")
							break
						}
					}
					if lockedAddr != 0 {
						break
					}
				}
				continue
			}

			// 3. LOCKED：死守 lockedAddr
			d, ok := readCemuActor(h, lockedAddr)
			if !ok {
				for i := 0; i < 3; i++ {
					time.Sleep(30 * time.Millisecond)
					if d, ok = readCemuActor(h, lockedAddr); ok {
						break
					}
				}
			}

			// 读不到标准三元组（过图黑屏 / 场景切换过渡期 / 瞬态读空）
			if !ok {
				// 场景加载过渡期或神庙内黑屏
				if shrineMode {
					publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 0, "shrine")
					continue
				}

				if !holdLogged {
					fmt.Println("  [hold] Cemu 场景加载或瞬态过渡中 -> 保持上一位置 / Loading or transient -> holding position")
					holdLogged = true
				}
				if hasPrev {
					publishState(true, prev[0], prev[1], prev[2], true, 1, "hold")
				}
				continue
			}

			// 读出合法三元组
			holdLogged = false
			cur := d
			isShrineLocal := isShrineScale(cur)

			// 神庙内部试炼场本地尺度挂起
			if shrineMode {
				if !isShrineLocal {
					// 出庙：脱离神庙本地尺度，恢复大世界跟踪
					shrineMode = false
					destName := formatDestName(cur)
					fmt.Printf("  [shrine] Cemu 角色离开试炼场 -> 恢复大世界追踪 (%.1f, %.1f, %.1f) [%s] / Left shrine -> Resumed world tracking\n",
						cur[0], cur[1], cur[2], destName)
					publishState(true, cur[0], cur[1], cur[2], true, 1, "cemu-fixed")
					prev = cur
					hasPrev = true
					continue
				}
				// 还在神庙内：死死钉在神庙入口
				publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 1, "shrine")
				continue
			} else if isShrineLocal && (!hasPrev || !isShrineScale(prev)) && dist3(cur, prev) > 100.0 {
				// 进庙：世界尺度 → 神庙本地尺度，红点钉在进入前的门口 prev
				shrineMode = true
				shrineEntrance = prev
				destName := formatDestName(prev)
				fmt.Printf("  [shrine] Cemu 角色进入试炼场 [%s] -> 锚定神庙入口 (%.1f, %.1f, %.1f) / Entered shrine [%s] -> Holding entrance\n",
					destName, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], destName)
				publishState(true, shrineEntrance[0], shrineEntrance[1], shrineEntrance[2], true, 1, "shrine")
				continue
			}

			delta := dist3(cur, prev)

			// 传送判定（>300m）
			if hasPrev && delta > 300.0 {
				shrineMode = false
				destName := formatDestName(cur)
				fmt.Printf("  [warp] Cemu 角色大传送 (距离=%.1fm) -> %s (%.1f, %.1f, %.1f) [零重扫接管] / Fast travel (dist=%.1fm) -> %s [Direct takeover]\n",
					delta, destName, cur[0], cur[1], cur[2], delta, destName)
				publishState(true, cur[0], cur[1], cur[2], true, 1, "cemu-fixed")
				prev = cur
				continue
			}

			// 单帧位移异常过大（>150m 非传送跳变）
			if hasPrev && delta > 150.0 {
				publishState(true, prev[0], prev[1], prev[2], true, 1, "hold")
				continue
			}

			// 正常跟随
			publishState(true, cur[0], cur[1], cur[2], true, 1, "cemu-fixed")
			prev = cur
			hasPrev = true
		}
	}
}
