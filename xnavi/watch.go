// 定位核心（单一状态机）。
//
// 蓝本：TOTKmap live-go/watch.go（V1.8.7，已真机验证"零自动扫描 + 探针监听 +
// 冻结共识"方案）；行为基线：BotwNavi(live-go) 与 navicemu(live-cemu)。
// 平台差异（进程/端序/内存块/存档）经 Platform 接口隔离；游戏差异（坐标校验/
// 地图换算/layer）在 game.go。
//
// 核心原则（ds 文档第 2 节）：
//   - 单一状态机：同一时刻只有 stateMachine() 写 lock.addr。
//   - 快速路径只给候选：known/共识/探针都不直接 verified，最终锁定必须移动确认。
//   - 全量扫描永久兜底：任何快速路径失败都自动回退扫描。
//   - 游戏重启检测保留：块世代检测，新块 → 只扫新块 + 回 UNLOCKED。
//   - 站桩核对保留（ds 文档永久兜底）：无移动 ≥30s 全量扫描，覆盖探针盲区
//     （玩家站桩 + 锁错槽时没人"在动"可监听）。
// 已删除（迭代中证明弊大于利）：自证回扫、无佐证回扫、teleport 共识仲裁。

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

// procPID 当前附加的模拟器进程（monitor.go status.json 输出用；平台内部自己管理句柄）。
var procPID uint32

// guestRamBase 返回 known 偏移的锚点基址：
// 当前会话块 → known 记录的 block → 平台最大块。
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

// tryOffsets known 快路径：把记住的偏移 rebase 到当前锚块并读取。
// 多副本一致投票：多个偏移都可读但互相矛盾 → 不信任；单偏移有效即可接受。
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
		return 0, [3]float32{}, false // 多偏移矛盾 → 可能是死槽积累，不信任
	}
	return best.addr, best.pos, true
}

// ---- 块世代：模拟器重开游戏后新旧内存块并存（旧块残留上次会话玩家槽）----
// 扫描时发现"从未见过的新块"判定为游戏重启/块重建 → 只扫新块、回 UNLOCKED。
var (
	knownBlocks        []uintptr
	currentSessionBase uintptr
)

// detectNewBlocks：对比本次块与已知块，返回从未见过的新块 base 列表。
// 返回非空 = 游戏重启/块重建（knownBlocks 只保留新块）。
// 首次（knownBlocks 空）：以 known_addrs 记录的 block 为"上次会话块"参照，
// 当前块 ≠ 参照块 → 判定新会话（程序与游戏同时重启也能识别）。
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
				// 参照块还在 → 游戏未重启（同一会话）→ 全部记 known，不触发
				for _, b := range blocks {
					knownBlocks = append(knownBlocks, b.Base)
				}
				return nil
			}
			// 参照块不在 → 游戏重启 → 当前全部块都是新会话
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

// sessionBlocks 返回当前会话的内存块（currentSessionBase 所在块）。
// currentSessionBase 未定时返回全部块（块世代检测尚未建立）。
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

// ---- 单一状态机 ----

const (
	smTick         = 200 * time.Millisecond // 5Hz 循环
	invalidMax     = 10                     // 连续读数无效次数 → 回 UNLOCKED（~2s，扛过读图 Loading）
	teleportDist   = 300.0                  // 相邻采样位移超过 → 判传送
	moveDist       = 0.5                    // 移动确认最小位移（游戏单位/采样）
	moveConfirmN   = 3                      // 连续移动采样次数 → verified
	scanCooldown   = 15 * time.Second       // 扫描失败重试间隔（成功后由解锁事件清零触发立即扫）
	knownRetry     = 1 * time.Second        // known 快路径重试间隔（扫描失败期间）
	probeEvery     = 600 * time.Millisecond // 监听确认采样周期
	probeMoveN     = 3                      // 组内移动采样次数 ≥ 此值 → 判活组
	probeLife      = 60 * time.Second       // 监听确认最长持续时间
	probeAddrCap   = 24                     // 每组最多观察的地址数
	probeNearDist  = 50.0                   // 换锁候选与本锁位置的最大差（防远处微动槽误换）
	frozenTicks    = 150                    // 锁读数停滞 tick 数（30s）→ 启动共识兜底检查
	consensusMin   = 3                      // 共识簇最少成员数
	consensusDist  = 15.0                   // 共识位置与本锁读数的最小差
	consensusEvery = 1 * time.Second        // 共识检查间隔
	verifyEvery    = 30 * time.Second       // 站桩核对周期（无移动时全量扫描兜底）
)

var (
	rescanCh     chan struct{} // /rescan 请求（server.go → requestRescan）
	lastScanAt   time.Time
	lastMoveAt   time.Time // 最近一次"位移 ≥0.5m"时刻（站桩核对判定用）
	lastVerifyAt time.Time // 最近一次站桩核对时刻
	useSaveScan  = true    // --no-save 时关闭存档锚点扫描（known 快路径不受影响）
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
	lastScanAt = time.Time{} // 解锁后下一拍立即扫描；15s 冷却只约束"扫不到"的重试
	fmt.Printf("  [sm] -> UNLOCKED (%s)\n", reason)
}

// requestRescan 供 /rescan HTTP 端点调用（非阻塞，向状态机发信号，不自行开 goroutine）。
func requestRescan() {
	select {
	case rescanCh <- struct{}{}:
	default:
	}
}

// dist3 两点距离（展示序）。
func dist3(a, b [3]float32) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// consensusCopy 在 known 偏移里找最大一致簇（互相 <2m，排除本锁地址）。
// 返回簇的一个代表地址与位置；成员 < consensusMin 视为无共识。
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
	for _, o := range loadOffsets() {
		adr := base + o
		if adr == exclude {
			continue
		}
		if d := decodeTripleAt(h, adr); d != nil {
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

// stateMachine 定位主循环：全程序只有这里写 lock.addr。
func stateMachine() {
	rescanCh = make(chan struct{}, 1)

	inLocked := false
	var prev [3]float32 // 上一次有效读数（展示序 gx, gz, alt）
	hasPrev := false
	moveN := 0    // 连续移动采样计数（≥moveConfirmN → verified）
	invalidN := 0 // 连续读数无效计数
	frozenN := 0  // 锁读数停滞 tick 计数（共识兜底触发用）
	savedKnown := false
	lastKnownAt := time.Now().Add(-knownRetry)
	lastConsensusAt := time.Time{}
	lastVerifyAt = time.Now() // 启动时清零窗口，避免刚锁定就核对
	consensusCand := uintptr(0) // 冻结共识候选（需"动了"才换锁）
	var consensusCandPos [3]float32

	// BOTW 特有：神庙模式 + 回跳撤销（移植自 live-cemu poll）
	shrineMode := false
	var preJump [3]float32 // 最近一次 accept 传送/跳变前的位置（展示序）
	var lastJumpAt time.Time
	// 跳变爆发检测：3s 内 ≥3 次大跳变 = 锁地址失效（传送后槽被游戏重置/复用），重扫
	jumpBurst := 0
	var lastJumpBurstAt time.Time

	// 监听确认（probe）状态：观察 shortlist 各组，谁在动锁谁
	probing := false
	var probeGroups []ShortlistEntry
	probeBase := map[uintptr][3]float32{}
	var probeMoved []int
	var probeStart, probeLast time.Time
	probeLockGi := -1 // 当前锁来自哪个探针组（该组由 moveN 正常确认）
	var probeRefPos [3]float32 // 换锁位置基准（展示序 X, Z, alt）
	lastProbeIgnoreGi := -1    // ignore 限频：同组 30s 内只打一行
	var lastProbeIgnoreAt time.Time

	resetFollow := func() {
		prev, hasPrev = [3]float32{}, false
		moveN, invalidN, frozenN = 0, 0, 0
		savedKnown = false
		consensusCand = 0
	}

	tick := time.NewTicker(smTick)
	defer tick.Stop()
	genN := 0 // 块世代检测计数（15 tick ≈ 3s）

	for range tick.C {
		// 0) 模拟器附着（未运行则等待；平台切换自动处理）
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

		// 1) 游戏重启检测（每 ~3s）：出现从未见过的新块 → 只扫新块 + 回 UNLOCKED
		genN++
		if genN >= 15 {
			genN = 0
			if blks := p.Blocks(minBlockMB); len(blks) > 0 {
				if newOnes := detectNewBlocks(blks); newOnes != nil {
					fmt.Printf("  [sm] %d NEW memory block(s) (game restart) -> rescan on new blocks\n", len(newOnes))
					bestBase, bestSize := uintptr(0), uintptr(0)
					for _, b := range blks {
						for _, nb := range newOnes {
							if b.Base == nb && b.Size > bestSize {
								bestBase, bestSize = b.Base, b.Size
							}
						}
					}
					currentSessionBase = bestBase
					lastScanAt = time.Time{}
					if inLocked {
						inLocked = false
						goUnlocked("game restart (new blocks)")
					}
				}
			}
		}

		// 2) /rescan 请求：回 UNLOCKED 立即重扫
		select {
		case <-rescanCh:
			lastScanAt = time.Time{}
			if inLocked {
				inLocked = false
				goUnlocked("/rescan requested")
			}
		default:
		}

		// ---- UNLOCKED：扫描先行（解锁时冷却已清零），known 作为扫描失败期间的备胎 ----
		if !inLocked {
			if useSaveScan && time.Since(lastScanAt) >= scanCooldown {
				lastScanAt = time.Now()
				res := locate(procPID, 120.0, sessionBlocks(h), func(s string) { fmt.Println("    " + s) })
				if res != nil {
					setLock(res.Addr, false, res.Copies, "scan")
					inLocked = true
					resetFollow()
					// 建立监听确认：观察 shortlist 各组，谁在动锁谁
					if len(res.Shortlist) > 0 {
						probing = true
						probeGroups = res.Shortlist
						probeMoved = make([]int, len(probeGroups))
						probeBase = map[uintptr][3]float32{}
						probeStart, probeLast = time.Now(), time.Time{}
						probeLockGi = 0 // best=ranked[0]
					}
					fmt.Printf("  [sm] scan -> lock 0x%X copies=%d struct=%d mem=(%.1f, %.1f, %.1f) [probing %d groups]\n",
						res.Addr, res.Copies, res.Struct, res.Hud[0], res.Hud[1], res.Hud[2], len(probeGroups))
					// 换锁位置基准（展示序 (X, Z, alt)，与 decodeTripleAt 输出同序）
					probeRefPos = res.Hud
				} else {
					fmt.Println("  [sm] scan found nothing - retrying")
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

		// 监听确认（600ms 一拍）：观察各组是否在动；本锁未 verified 时"谁在动锁谁"
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
				probing = false // 本锁已由移动确认，不再换
			} else {
				bestGi, bestMv := -1, 0
				for gi, mv := range probeMoved {
					if mv >= probeMoveN && gi != probeLockGi && mv > bestMv {
						bestGi, bestMv = gi, mv
					}
				}
				if bestGi >= 0 {
					adr := probeGroups[bestGi].Addrs[0]
					// 位置校验：换锁候选必须在本锁位置附近（玩家槽副本簇），
					// 防止远处"微动槽"（相机/UI/物理波动）在站桩时被误判为玩家。
					// decodeTripleAt 与 probeRefPos 同为展示序 (X, Z, alt)。
					if d := decodeTripleAt(h, adr); d != nil &&
						dist3([3]float32{d[0], d[1], d[2]}, probeRefPos) < probeNearDist {
						setLock(adr, false, probeGroups[bestGi].Copies, "probe")
						saveKnown([]uintptr{adr}, guestRamBase(), [3]float32{}) // 只记偏移，pos 待移动确认
						probing = false
						resetFollow()
						a = adr
						probeRefPos = [3]float32{d[0], d[1], d[2]}
						fmt.Printf("  [sm] probe -> switched to moving group #%d copies=%d addr=0x%X\n",
							bestGi, probeGroups[bestGi].Copies, adr)
					} else {
						// 在动但位置远离玩家 → 不是玩家槽，忽略（限频打印）
						dist := float32(-1)
						if d := decodeTripleAt(h, adr); d != nil {
							dist = dist3([3]float32{d[0], d[1], d[2]}, probeRefPos)
						}
						if bestGi != lastProbeIgnoreGi || time.Since(lastProbeIgnoreAt) > 30*time.Second {
							lastProbeIgnoreGi = bestGi
							lastProbeIgnoreAt = time.Now()
							fmt.Printf("  [sm] probe: group #%d moving but %.0fm away - ignore\n", bestGi, dist)
						}
					}
				}
			}
			if probing && time.Since(probeStart) > probeLife {
				probing = false
			}
		}

		lock.mu.RLock()
		a = lock.addr // probe 可能已换锁
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
				goUnlocked(fmt.Sprintf("readings invalid x%d (scene change?)", invalidMax))
			}
			continue
		}
		invalidN = 0
		cur := [3]float32{v[0], v[1], v[2]}

		// ---- 神庙模式（BOTW 特有）：红点固定神庙入口，忽略内部坐标 ----
		// cur 展示序 = (X, Z, alt)：神庙内部判定看 X/Z 平面
		if shrineMode {
			if abs32(cur[0]) > gameShrineExit || abs32(cur[1]) > gameShrineExit {
				fmt.Printf("  [shrine] left shrine (world %.0f, %.0f) -> normal tracking\n", cur[0], cur[1])
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

		// 传送：verified 锁的大位移就是真传送，直接接受新位置（零扫描）。
		// 未 verified 的锁出现大位移 = 坏槽跳变，回扫。
		if hasPrev && delta > teleportDist {
			lock.mu.RLock()
			ver := lock.verified
			lock.mu.RUnlock()
			if ver {
				incCounter("jumps")
				frozenN = 0
				// 跳变爆发：3s 内 ≥3 次大跳变 → 锁地址不稳定（槽被游戏重置/复用），重扫
				if time.Since(lastJumpBurstAt) < 3*time.Second {
					jumpBurst++
				} else {
					jumpBurst = 1
				}
				lastJumpBurstAt = time.Now()
				if jumpBurst >= 3 {
					fmt.Printf("  [sm] repeated teleport jumps x%d - lock unstable -> rescan\n", jumpBurst)
					inLocked = false
					goUnlocked("repeated jumps (unstable lock)")
					continue
				}
				fmt.Printf("  [sm] teleport jump %.0fm accepted in place (no scan)\n", delta)
				// 回跳撤销窗口：记录跳变前位置，3s 内读数回跳则视为动画假传送
				preJump = prev
				lastJumpAt = time.Now()
				// 传送目标匹配神庙坐标 → 神庙模式（红点固定神庙入口）
				if pnt := matchShrine(cur[0], cur[2], cur[1]); pnt != nil {
					shrineMode = true
					fmt.Printf("  [shrine] entered %s -> pin red dot to shrine\n", pnt.Cn)
				}
			} else {
				inLocked = false
				goUnlocked(fmt.Sprintf("teleport jump %.0fm on unverified lock -> rescan", delta))
				continue
			}
		}

		// 回跳撤销：accept 跳变后 3s 内读数回到跳变前位置附近 → 动画假传送（对话/克洛格等），回滚
		if !lastJumpAt.IsZero() && time.Since(lastJumpAt) < gameShrineReturnWindow &&
			dist3(cur, preJump) < gameShrineReturnDist {
			cur = preJump
			lastJumpAt = time.Time{}
			fmt.Println("  [sm] revert to pre-jump (animation bounce)")
		}

		// 移动确认：连续 3 次采样位移 >0.5m → verified（写 known 记忆 + 坐标先验）
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
					// 内存序 pos = (X, alt, Z) ← 展示序 (gx, gz, alt)
					saveKnown([]uintptr{a}, guestRamBase(), [3]float32{cur[0], cur[2], cur[1]})
				}
			}
		} else {
			moveN = 0
			if hasPrev {
				frozenN++
			}
		}

		// 更新 STATE（/pos 输出口径与旧版一致）
		lock.mu.RLock()
		verified, copies, src := lock.verified, lock.copies, lock.source
		lock.mu.RUnlock()
		state.mu.Lock()
		state.ok = true
		state.gx, state.gy, state.gz = cur[0], cur[2], cur[1] // gx=X东, gy=alt高, gz=Z南
		state.mx, state.my = gameToMap(cur[0], cur[2], cur[1])
		state.layer = gameLayer
		state.age = float64(time.Now().UnixNano()) / 1e9
		state.verified = verified
		state.copies = copies
		state.source = src
		state.mu.Unlock()
		prev = cur
		hasPrev = true

		// ---- 站桩核对（永久兜底）：无移动 ≥30s 时窗口扫描一次（读存档锚点 ±400，
		//      只找"玩家槽"候选，全量 top 会被远处高 copies 的无关槽污染）；
		//      候选与锁位置差 >15m → 回 UNLOCKED 重扫；
		//      无结果或差异不大 → 不杀锁（玩家可能离存档锚点较远）。
		if time.Since(lastVerifyAt) >= verifyEvery && time.Since(lastMoveAt) >= verifyEvery {
			lastVerifyAt = time.Now()
			vstart := time.Now()
			res := locate(procPID, 400.0, sessionBlocks(h), nil)
			if res != nil {
				// cur 与 res.Hud 同为展示序 (X, Z, alt)，直接比较
				vd := dist3(cur, res.Hud)
				if vd > consensusDist {
					fmt.Printf("  [sm] verify: idle scan found player slot %.0fm away -> rescan\n", vd)
					inLocked = false
					goUnlocked("verify: better candidate")
					continue
				}
			}
			fmt.Printf("  [sm] verify: idle %.0fs, window scan clean in %.1fs (keep lock)\n",
				verifyEvery.Seconds(), time.Since(vstart).Seconds())
		}

		// 冻结共识兜底（零扫描）：锁读数停滞 ≥30s 时，找 known 偏移里与本锁
		// 差 >15m 的 ≥3 成员一致簇作候选；候选在下一次检查"动了"（活副本证明）
		// 才换锁——玩家站桩时绝不误换，玩家一动 1~2s 内自愈。
		if frozenN >= frozenTicks && time.Since(lastConsensusAt) >= consensusEvery {
			lastConsensusAt = time.Now()
			a2, v2, ok := consensusCopy(h, a)
			if !ok {
				consensusCand = 0
			} else if a2 != consensusCand {
				consensusCand, consensusCandPos = a2, v2
				fmt.Printf("  [sm] lock frozen %.0fs, consensus candidate 0x%X (%.0fm away) - watching\n",
					frozenTicks*smTick.Seconds(), a2, dist3(v2, cur))
			} else if dist3(v2, consensusCandPos) > moveDist {
				// 候选位置变了 = 活副本在跟随玩家 → 换锁
				fmt.Printf("  [sm] frozen lock 0x%X -> relock to live consensus copy 0x%X\n", a, a2)
				setLock(a2, false, 0, "consensus")
				resetFollow()
				probing = false
				continue
			}
		}
	}
}

// ---- 导航目标（server.go 使用，原样保留）----

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
	target = nil
	targetMu.Unlock()
}
