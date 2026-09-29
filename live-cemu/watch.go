// 运行状态机：坐标轮询、看门狗、校验、短名单监听、结构扫描兜底。
// 与 BotwNavi(live-go) watch.go 同构；差异：进程=Cemu、memory_base 锚定、大端读取、
// 无存档锚点（定位路径：known 偏移 → 社区链 → 结构扫描+走动确认）。

package main

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

var (
	procMu     sync.Mutex
	procHandle uintptr
	procPID    uint32
	guestBase  atomic.Uintptr // memory_base
)

const processName = "cemu" // Cemu.exe

type lockT struct {
	mu       sync.RWMutex
	addr     uintptr
	verified bool
	copies   int
	source   string
}

var lock = lockT{}

type stateT struct {
	mu       sync.RWMutex
	ok       bool
	gx, gy, gz float32 // gx=X, gy=alt, gz=Z
	mx, my   float32   // 地图像素
	layer    int
	age      float64
	lastSeen float64
	verified bool
	copies   int
	source   string
}

var state = stateT{}

// findLiveCemu 找到"正在运行游戏"的 Cemu 实例：
// 多个 cemu.exe 并存时（重启残留），优先选有 ≥minBlockMB 大块（游戏世界已加载）的实例，
// 否则退回 pid 最大的最新实例（游戏可能还在加载，等 watchdog 追上来）。
func findLiveCemu() (uint32, uintptr) {
	pids := findPids(processName)
	if len(pids) == 0 {
		return 0, 0
	}
	live, maxPid := uint32(0), uint32(0)
	for _, pid := range pids {
		if pid > maxPid {
			maxPid = pid
		}
		h, err := openProcess(pid)
		if err != nil || h == 0 {
			continue
		}
		if len(rwBlocks(h, minBlockMB)) > 0 && pid > live {
			live = pid
		}
		closeHandle(h)
	}
	choose := live
	if choose == 0 {
		choose = maxPid
	}
	if choose == 0 {
		return 0, 0
	}
	h, err := openProcess(choose)
	if err != nil || h == 0 {
		return 0, 0
	}
	return choose, h
}

func reopenProcess() bool {
	procMu.Lock()
	defer procMu.Unlock()
	pid, h := findLiveCemu()
	if pid == 0 || h == 0 {
		return false
	}
	if pid == procPID && procHandle != 0 {
		closeHandle(h)
		return true
	}
	if procHandle != 0 {
		closeHandle(procHandle)
	}
	procHandle, procPID = h, pid
	fmt.Printf("  [proc] re-attached to Cemu pid=%d\n", pid)
	return true
}

// guestRamBase 返回 known 偏移的锚点基址 = 最大已提交 RW 块的 **AllocationBase**（每次必可找到）。
// ⚠️ 必须用 AllocationBase 而非 BaseAddress：本机两者差 0x2000000（_情报-社区固定针位.md）。
// 社区链需要精确 memory_base（见 findCemuBase，同样锚 AllocationBase）；known 快路径锚与之一致，
// 固定针位偏移（0x1055300C）也落在同一锚下，三者自洽。
func guestRamBase() uintptr {
	procMu.Lock()
	h := procHandle
	procMu.Unlock()
	if h == 0 {
		return 0
	}
	_, alloc, _ := largestRWAlloc(h)
	return alloc
}

// decodePosF 校验并转换内存序 (X, alt, Z) → 展示序 (gx, gz, alt)。无效返回 nil。
func decodePosF(v [3]float32) []float32 {
	gx, alt, gz := v[0], v[1], v[2]
	if !posRangeOK(gx, alt, gz) {
		return nil
	}
	return []float32{gx, gz, alt}
}

// 社区固定针位（确定性、零扫描、版本内稳定）：
// 来源 koko-yl/BotWRamWatch（ram_dll/dllmain.cpp），BotW JP v208 在本机跨实例实测有效。
// base = Cemu 进程最大 RW 块的 AllocationBase（不是 BaseAddress！）；
// X=base+0x1055300C  Y(高度)=base+0x10553010  Z=base+0x10553014（PPC 大端 f32）。
const (
	fixedPosXOff = 0x1055300C
	fixedPosYOff = 0x10553010
	fixedPosZOff = 0x10553014
)

// tryFixedPos 确定性快路径：直接读 AllocationBase + 固定偏移，无需任何扫描/走动。
func tryFixedPos() (uintptr, [3]float32, bool) {
	if !reopenProcess() {
		return 0, [3]float32{}, false
	}
	procMu.Lock()
	h := procHandle
	procMu.Unlock()
	_, alloc, _ := largestRWAlloc(h)
	if alloc == 0 {
		return 0, [3]float32{}, false
	}
	guestBase.Store(alloc) // known 偏移锚 = AllocationBase（固定偏移 0x1055300C 同锚，自洽）
	a := alloc + fixedPosXOff
	if p, ok := readPosBE(h, a); ok {
		if d := decodePosF([3]float32{p[0], p[1], p[2]}); d != nil {
			return a, [3]float32{d[0], d[1], d[2]}, true
		}
	}
	return 0, [3]float32{}, false
}

// tryOffsets known fast path：把记住的偏移 rebase 到当前 memory_base 并读取。
func tryOffsets() (uintptr, [3]float32, bool) {
	if !reopenProcess() {
		return 0, [3]float32{}, false
	}
	procMu.Lock()
	h := procHandle
	procMu.Unlock()
	base := guestRamBase()
	if base == 0 {
		return 0, [3]float32{}, false
	}
	guestBase.Store(base)
	offs := loadOffsets()
	type val struct {
		addr uintptr
		pos  [3]float32
	}
	var vals []val
	for _, o := range offs {
		if v, ok := readPosBE(h, base+o); ok {
			if d := decodePosF([3]float32{v[0], v[1], v[2]}); d != nil {
				vals = append(vals, val{base + o, [3]float32{d[0], d[1], d[2]}})
			}
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
	// 多个偏移都可读但互相矛盾 → 不信任（可能锁到别的 actor 或陈旧副本）。
	// 单个偏移有效即可接受（其余偏移读不出=死偏移，不应拖垮整个快路径）。
	if len(vals) >= 2 && bestN < 2 {
		return 0, [3]float32{}, false
	}
	return best.addr, best.pos, true
}

// poll 10Hz 把锁定的地址镜像到 STATE。跳变过滤同 BotwNavi。
var (
	lastPollGx, lastPollGy, lastPollGz float32
	bigJumpN                           int
)

// poll 每 100ms 读一次锁定地址，更新 state。
// 大跳变处理（v1.1.0 改造）：
//   - 正常移动（<pollJumpMax）直接更新
//   - 大跳变进入 pending 确认：稳定 2 次 → 传送（accept）；回到原位附近 → 回弹扰动
//     （拿克洛格动画等）→ 忽略，不画轨迹虚线
//   - 持续乱跳（driftN>=6）→ 判定漂移 → 触发 relocalize（结构扫描+坐标先验找回）
//   - 传送目标匹配神庙坐标 → 进入神庙模式（红点固定神庙入口，不追踪内部坐标）
var (
	pendingPos   [3]float32
	pendingN     int
	pendingDelta float32
	driftN       int
	shrineMode   bool
	shrinePin    [3]float32 // 进入神庙前最后一个世界坐标（红点固定点）
	preJump      [3]float32 // 最近一次 accept 传送/跳变前的位置
	lastJumpAt   time.Time  // 最近一次 accept 传送的时间（回跳撤销窗口）
)

func poll() {
	for {
		lock.mu.RLock()
		a := lock.addr
		verified := lock.verified
		copies := lock.copies
		src := lock.source
		lock.mu.RUnlock()

		var v []float32
		if a != 0 {
			procMu.Lock()
			h := procHandle
			procMu.Unlock()
			if h != 0 {
				if p, ok := readPosBE(h, a); ok {
					v = decodePosF([3]float32{p[0], p[1], p[2]})
				}
			}
		}
		state.mu.Lock()
		if v != nil {
			gx, gz, alt := v[0], v[1], v[2]

			// ---- 进入神庙识别（坐标尺度法）：世界尺度(|x|,|z|>300) → 内部尺度(<300) 单 poll 大跳变 ----
			// 确定性判定：不依赖"目标是否匹配神庙入口坐标"，任何此类跳变都判进入
			// （覆盖直接走门进入、传送进内部等；旧 matchShrine 入口保留作传送补充）
			if !shrineMode && lastPollGx != 0 &&
				abs32(gx) < pollShrineExit && abs32(gz) < pollShrineExit &&
				(abs32(lastPollGx) > pollShrineExit || abs32(lastPollGz) > pollShrineExit) &&
				dist3(gx, alt, gz, lastPollGx, lastPollGy, lastPollGz) > 50 {
				shrineMode = true
				shrinePin = [3]float32{lastPollGx, lastPollGy, lastPollGz}
				fmt.Printf("  [shrine] entered shrine (interior %.0f, %.0f) -> pin dot to entrance\n", gx, gz)
			}

			// ---- 神庙模式：红点固定神庙入口（shrinePin），忽略内部坐标 ----
			if shrineMode {
				if abs32(gx) > pollShrineExit || abs32(gz) > pollShrineExit {
					fmt.Printf("  [shrine] left shrine (world %.0f, %.0f) -> normal tracking\n", gx, gz)
					shrineMode = false
				} else {
					gx, alt, gz = shrinePin[0], shrinePin[1], shrinePin[2]
				}
			}

			// 回跳撤销：accept 跳变后 3s 内读数回到跳变前位置附近 → 动画假传送（对话/克洛格等），回滚
			if !lastJumpAt.IsZero() && time.Since(lastJumpAt) < 3*time.Second &&
				dist3(gx, alt, gz, preJump[0], preJump[1], preJump[2]) < 50 {
				gx, alt, gz = preJump[0], preJump[1], preJump[2]
				lastPollGx, lastPollGy, lastPollGz = gx, alt, gz
				lastJumpAt = time.Time{}
				fmt.Println("  [poll] revert to pre-jump (animation bounce)")
			}

			accept := true
			if state.ok && lastPollGx != 0 {
				dx, dy, dz := gx-lastPollGx, alt-lastPollGy, gz-lastPollGz
				delta := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
				if delta > pollJumpMax {
					if pendingN > 0 && dist3(gx, alt, gz, pendingPos[0], pendingPos[1], pendingPos[2]) < 5 {
						// 稳定在 pending 附近 → 传送确认
						pendingN++
						if pendingN >= pollJumpConfirmN {
							preJump = [3]float32{lastPollGx, lastPollGy, lastPollGz}
							lastJumpAt = time.Now()
							gx, alt, gz = pendingPos[0], pendingPos[1], pendingPos[2]
							fmt.Printf("  [poll] teleport %.1fm x%d -> accept\n", pendingDelta, pendingN)
							if p := matchShrine(gx, alt, gz); p != nil {
								shrineMode = true
								shrinePin = [3]float32{gx, alt, gz}
								fmt.Printf("  [shrine] entered %s -> pin red dot to shrine\n", p.Cn)
							}
							pendingN = 0
							driftN = 0
						} else {
							accept = false
						}
					} else if dist3(gx, alt, gz, lastPollGx, lastPollGy, lastPollGz) < 5 {
						// 回到原位置附近 → 回弹扰动（克洛格动画等）→ 忽略，不画虚线
						pendingN = 0
						driftN = 0
						accept = false
					} else {
						// 新的大跳变目标
						pendingPos = [3]float32{gx, alt, gz}
						pendingDelta = delta
						pendingN = 1
						driftN++
						accept = false
						if driftN >= pollDriftN {
							fmt.Printf("  [poll] sustained jumps x%d -> drift detected, relocalizing\n", driftN)
							driftN = 0
							pendingN = 0
							go relocalize("poll drift")
						}
					}
				} else {
					// 正常移动
					pendingN = 0
					driftN = 0
				}
			}
			if accept {
				lastPollGx, lastPollGy, lastPollGz = gx, alt, gz
				state.ok = true
				state.gx, state.gy, state.gz = gx, alt, gz
				state.mx, state.my = 2*gx+12000, 2*gz+10000
				state.layer = 0
				state.verified = verified
				state.copies = copies
				state.source = src
				if shrineMode {
					state.source = "shrine"
				}
			}
			state.age = 0
			state.lastSeen = float64(time.Now().UnixNano()) / 1e9
		} else {
			state.age = float64(time.Now().UnixNano())/1e9 - state.lastSeen
			state.ok = false
			if a != 0 {
				state.source = "address lost"
			} else {
				state.age = float64(time.Now().UnixNano())/1e9 - state.lastSeen
				state.source = "locating..."
			}
		}
		state.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

const (
	pollJumpMax      = 10.0 // 单 poll 正常移动上限（m）
	pollJumpConfirmN = 6    // 大跳变稳定确认次数（600ms，动画短暂停留不误判传送）
	pollDriftN       = 4    // 持续乱跳判定漂移的计数（更快触发重扫）
	pollShrineMatch  = 80.0 // 跳变目标与神庙坐标的像素匹配半径（px）
	pollShrineExit   = 300.0 // 神庙内部坐标上限（|x|,|z| 超此判定已离开）
)

// dist3 三维欧氏距离。
func dist3(a1, a2, a3, b1, b2, b3 float32) float32 {
	dx, dy, dz := a1-b1, a2-b2, a3-b3
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// matchShrine 跳变目标（内存序 gx, alt, gz）是否落在某神庙入口附近（像素坐标）。
func matchShrine(gx, alt, gz float32) *ProgressPoint {
	if progress == nil {
		return nil
	}
	px, py := 2*gx+12000, 2*gz+10000
	progress.mu.RLock()
	defer progress.mu.RUnlock()
	for i := range progress.refs {
		p := &progress.refs[i]
		if p.T != "shrine" {
			continue
		}
		dx, dy := px-float32(p.X), py-float32(p.Y)
		if dx*dx+dy*dy < pollShrineMatch*pollShrineMatch {
			return p
		}
	}
	return nil
}

// watchShortlist 锁定真正在动的候选组（大端读取）。
func watchShortlist(sl []ShortlistEntry) {
	procMu.Lock()
	h := procHandle
	procMu.Unlock()
	if h == 0 {
		return
	}
	base := map[uintptr][3]float32{}
	for _, g := range sl {
		for _, a := range g.Addrs {
			if p, ok := readPosBE(h, a); ok {
				base[a] = [3]float32{p[0], p[1], p[2]}
			}
		}
	}
	moved := make([]int, len(sl))
	confirmed := -1
	for {
		time.Sleep(600 * time.Millisecond)
		procMu.Lock()
		h = procHandle
		procMu.Unlock()
		if h == 0 {
			continue
		}
		for gi, g := range sl {
			for _, a := range g.Addrs {
				p, ok := readPosBE(h, a)
				if !ok {
					continue
				}
				v := [3]float32{p[0], p[1], p[2]}
				b, ok := base[a]
				base[a] = v
				if !ok {
					continue
				}
				if abs32(v[0]-b[0]) > 0.5 || abs32(v[1]-b[1]) > 0.5 || abs32(v[2]-b[2]) > 0.5 {
					moved[gi]++
				}
			}
		}
		// confirmed 后锁定组不再重算，防止副本组横跳
		top := confirmed
		if top < 0 {
			top = 0
			for i := 1; i < len(sl); i++ {
				if sl[i].Dist < 15.0 && sl[i].Dist <= sl[top].Dist+15.0 && moved[i] > moved[top] {
					top = i
				}
			}
		}
		if moved[top] >= 3 && top != confirmed {
			confirmed = top
			a := sl[top].Addrs[0]
			lock.mu.Lock()
			lock.addr = a
			lock.verified = true
			lock.copies = sl[top].Copies
			lock.source = "scan"
			lock.mu.Unlock()
			addrs := loadKnownAddrs()
			addrs = append(addrs, a)
			if p, ok := readPosBE(h, a); ok { _ = p; saveKnown([]uintptr{a}, guestBase.Load(), [3]float32{}) } // 锁定时只记偏移，pos 待平滑确认
			fmt.Printf("  [watch] locked onto group %d: copies=%d struct=%d mem=%v\n",
				top, sl[top].Copies, sl[top].Struct, sl[top].Hud)
		}
	}
}

func loadKnownAddrs() []uintptr {
	base := guestBase.Load()
	if base == 0 {
		return nil
	}
	var out []uintptr
	for _, o := range loadOffsets() {
		out = append(out, base+o)
	}
	return out
}

// verifyKnown 后台校验锁定的地址：平滑移动 → verified；死槽/坏槽 → 重定位。
// verifyTarget：同一时刻最多一个 verify goroutine，避免重复启动泄漏。
var verifyTarget atomic.Uintptr

func verifyKnown(addr uintptr) {
	if !verifyTarget.CompareAndSwap(0, addr) {
		return // 已有 verify 在跑
	}
	defer verifyTarget.Store(0)
	procMu.Lock()
	h := procHandle
	procMu.Unlock()
	if h == 0 {
		return
	}
	prev := readPos(h, addr)

	// 读数合法即标记 verified：known/motion 锁定的槽本就是结构候选，
	// 站着不动不应触发"无移动重扫"；坏槽由 bad 计数与 relocalize 兜底。
	if prev != nil {
		lock.mu.Lock()
		wasV := lock.verified
		if !wasV {
			lock.verified = true
			fmt.Printf("  [verify] 0x%X reads sane mem=(%.1f, %.1f, %.1f) -> verified now\n", addr, prev[0], prev[2], prev[1])
		}
		lock.mu.Unlock()
		if !wasV {
			saveKnown([]uintptr{addr}, guestBase.Load(), [3]float32{}) // 初始 verified 只记偏移，pos 待平滑确认
		}
	}
	bad := 0
	smoothN := 0
	staleSince := time.Now()
	staleGap := 8 * time.Second
	for {
		time.Sleep(300 * time.Millisecond)
		lock.mu.RLock()
		curAddr := lock.addr
		lock.mu.RUnlock()
		if curAddr != addr {
			return
		}
		procMu.Lock()
		h = procHandle
		procMu.Unlock()
		if h == 0 {
			return
		}
		cur := readPos(h, addr)
		if cur == nil {
			bad++
			if bad == 2 {
				reopenProcess()
			}
			if bad >= 4 {
				if a, v, ok := tryOffsets(); ok {
					fmt.Printf("  [verify] re-based offset onto new block -> 0x%X mem=(%.1f, %.1f, %.1f)\n", a, v[0], v[1], v[2])
					lock.mu.Lock()
					lock.addr = a
					lock.verified = false
					lock.copies = 0
					lock.source = "known"
					lock.mu.Unlock()
					go verifyKnown(a)
					return
				}
			}
			if bad >= 10 {
				fmt.Println("  [verify] address went bad - re-locating")
				lock.mu.Lock()
				lock.addr = 0
				lock.verified = false
				lock.mu.Unlock()
				relocalize("remembered address became unreadable")
				return
			}
			continue
		}
		bad = 0
		delta := float32(math.Inf(1))
		if prev != nil {
			dx, dy, dz := cur[0]-prev[0], cur[1]-prev[1], cur[2]-prev[2]
			delta = float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
		}
		if prev != nil && delta > verifyMoveMin && delta < verifyJumpMax {
			smoothN++
		} else {
			smoothN = 0
		}
		if smoothN >= verifySmoothN {
			lock.mu.Lock()
			wasVerified := lock.verified
			if !wasVerified {
				fmt.Printf("  [verify] address 0x%X is live (moving smoothly) -> verified\n", addr)
				lock.verified = true
			}
			lock.mu.Unlock()
			if !wasVerified {
				saveKnown([]uintptr{addr}, guestBase.Load(), [3]float32{cur[0], cur[2], cur[1]}) // 平滑确认才写 pos（cur=(gx,gz,alt)→(X,alt,Z)）
			}
		}
		prev = cur
		if time.Since(staleSince) >= staleGap {
			// 死槽/坏槽保护：**未验证**的锁长时间无任何移动 → 重扫换真槽。
			// （已验证的真锁站定不算死槽：玩家站着不动是合法状态，不应触发重扫；
			//   真锁变坏由 bad 计数与 poll 漂移兜底。）
			lock.mu.RLock()
			stillVerified := lock.verified
			lock.mu.RUnlock()
			if !stillVerified {
				fmt.Printf("  [verify] no movement for %.0fs at 0x%X -> re-locating\n", staleGap.Seconds(), addr)
				relocalize("no movement: stale or bad slot")
				lock.mu.RLock()
				still := lock.addr
				lock.mu.RUnlock()
				if still != addr {
					return // 已换槽，新 verify 在跑
				}
				// 未换槽：先验锁定的可能还是冻结槽 → 强制差分监听找真正在动的槽
				fmt.Println("  [verify] prior lock unchanged -> forcing motion diff scan")
				if a := locateByMotion(h, guestRamBase(), false); a != 0 && a != addr {
					lock.mu.Lock()
					lock.addr = a
					lock.verified = false
					lock.copies = 0
					lock.source = "motion"
					lock.mu.Unlock()
					fmt.Printf("  [verify] diff scan locked moving slot -> 0x%X\n", a)
					go verifyKnown(a)
					return
				}
			}
			staleSince = time.Now()
			if staleGap < 60*time.Second {
				staleGap *= 2 // 挂机退避：避免频繁无谓重扫
			}
		}
	}
}

func readPos(h uintptr, addr uintptr) []float32 {
	if p, ok := readPosBE(h, addr); ok {
		return decodePosF([3]float32{p[0], p[1], p[2]})
	}
	return nil
}

const (
	verifyMoveMin  = 0.2
	verifyJumpMax  = 50.0
	verifySmoothN  = 3
	verifyKeepDist = 15.0
)

// relocalize：known 失效时的重定位。路径：社区链（确定性，需 memory_base）→ 结构扫描+走动确认。
func relocalize(reason string) bool {
	if !reopenProcess() {
		fmt.Printf("  [relocate] %s -> Cemu not running\n", reason)
		return false
	}
	procMu.Lock()
	h := procHandle
	procMu.Unlock()

	// 0) 社区固定针位（确定性、零扫描）：本机已验证跟随玩家，首选
	if a, v, ok := tryFixedPos(); ok {
		lock.mu.Lock()
		lock.addr = a
		lock.verified = false
		lock.copies = 0
		lock.source = "fixed"
		lock.mu.Unlock()
		go verifyKnown(a)
		fmt.Printf("  [relocate] %s -> fixed offset 0x%X mem=(%.1f, %.1f, %.1f)\n",
			reason, a, v[0], v[1], v[2])
		return true
	}

	// 1) 社区链：仅当 memory_base 可锚定时尝试
	if base, _, ok := findCemuBase(h); ok {
		guestBase.Store(base)
		fmt.Printf("  [relocate] %s -> memory_base=0x%012X\n", reason, base)
		if res := chainLocate(h, base); res != nil {
			lock.mu.Lock()
			lock.addr = res.Addr
			lock.verified = false
			lock.copies = res.Copies
			lock.source = "chain"
			lock.mu.Unlock()
			go verifyKnown(res.Addr)
			saveKnown([]uintptr{res.Addr}, guestRamBase(), [3]float32{}) // chain lock: 只记偏移
			fmt.Printf("  [relocate] chain -> 0x%X mem=(%.1f, %.1f, %.1f) [verify on move]\n",
				res.Addr, res.Hud[0], res.Hud[1], res.Hud[2])
			return true
		}
		fmt.Println("  [relocate] chain not resolvable for this build - falling back to motion scan")
	} else {
		fmt.Printf("  [relocate] %s -> memory_base not anchored; motion scan\n", reason)
	}

	// 2) 结构扫描 + 走动确认
	if a := locateByMotion(h, guestRamBase(), true); a != 0 {
		lock.mu.Lock()
		lock.addr = a
		lock.verified = false // 不立即 verified：交 verifyKnown（首读 sane 才验证），防垃圾槽对外服务/写库
		lock.copies = 0
		lock.source = "motion"
		lock.mu.Unlock()
		go verifyKnown(a)
		return true
	}
	fmt.Println("  [relocate] no luck yet - will retry; walk a few steps in-game to help lock")
	return false
}

var lastScanAt = time.Now().Add(-time.Hour)

// watchdog 无需用户操作持续保持锁定。
func watchdog() {
	for {
		time.Sleep(3 * time.Second)
		lock.mu.RLock()
		hasAddr := lock.addr != 0
		src := lock.source
		lock.mu.RUnlock()
		state.mu.RLock()
		ok := state.ok
		state.mu.RUnlock()
		if hasAddr && (ok || src == "scan") {
			continue
		}
		if a, v, ok := tryOffsets(); ok {
			_ = !hasAddr
			lock.mu.Lock()
			lock.addr = a
			lock.verified = false
			lock.copies = 0
			lock.source = "known"
			lock.mu.Unlock()
			fmt.Printf("  [watchdog] recovered 0x%X mem=(%.1f, %.1f, %.1f)\n", a, v[0], v[1], v[2])
			go verifyKnown(a) // CAS 去重，始终盯最新锁
			continue
		}
		if time.Since(lastScanAt) > 20*time.Second {
			lastScanAt = time.Now()
			if !relocalize("watchdog: no remembered offset works") {
				// 游戏世界未加载（无大块）时压到 3s 重试，游戏加载完自动锁定
				procMu.Lock()
				hh := procHandle
				procMu.Unlock()
				if hh != 0 && len(rwBlocks(hh, minBlockMB)) == 0 {
					fmt.Println("  [watchdog] waiting for game world to load...")
					lastScanAt = time.Now().Add(-17 * time.Second)
				}
			}
		}
	}
}

// ---- 结构扫描 + 走动确认（兜底）----

const (
	motionChunk     = 32 << 20
	motionWorkers   = 8
	motionCountdown = 5
)

// locateByMotion 结构候选 + 走几步差分。无存档锚点时的手动兜底。
func locateByMotion(h uintptr, base uintptr, priorOK bool) uintptr {
	blocks := rwBlocks(h, minBlockMB)
	if len(blocks) == 0 {
		fmt.Println("  [motion] game world not loaded yet (no big RW block) - watchdog will retry")
		return 0
	}
	hits := structuralScan(h, blocks, func(s string) { fmt.Println("  " + s) })
	if len(hits) == 0 {
		fmt.Println("  [motion] no structural candidates")
		return 0
	}
	// 坐标先验：BotW 自动存档，读档位置 ≈ 上次退出位置。
	// 结构候选里离上次已知坐标最近的槽 = 玩家；静态 NPC/物件在固定坐标（通常远超 1000m），天然排除。
	// priorOK=false（死槽重扫）：跳过先验捷径，强制差分监听找"正在动的槽"，
	// 否则冻结槽（坐标=玩家最后位置）会被先验选中，卡死无法恢复。
	if priorOK {
		if p := loadKnownPos(); len(p) == 3 {
		bestAddr, bestD := uintptr(0), float32(1e9)
		for _, hit := range hits {
			dx, dy, dz := hit.X-p[0], hit.Y-p[1], hit.Z-p[2]
			d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
			if d < bestD {
				bestAddr, bestD = hit.Addr, d
			}
		}
			if bestAddr != 0 && bestD < 1000 {
				fmt.Printf("  [motion] nearest to last-known pos (%.0fm) -> 0x%X  [prior lock]\n", bestD, bestAddr)
				return bestAddr
			}
		}
	}
	baseline := map[uintptr][3]float32{}
	for _, hit := range hits {
		baseline[hit.Addr] = [3]float32{hit.X, hit.Y, hit.Z}
	}

	fmt.Println("")
		fmt.Println("")
		fmt.Println("  >>> WALK A FEW STEPS IN THE GAME (any time within 30s) <<<")
		fmt.Println("      (do NOT open the in-game map - it pauses the world)")
		beep(3)
		movedCount := map[uintptr]int{}
		deadline := time.Now().Add(30 * time.Second)
		bestAddr := uintptr(0)
		for time.Now().Before(deadline) {
			time.Sleep(600 * time.Millisecond)
			procMu.Lock()
			h = procHandle
			procMu.Unlock()
			if h == 0 {
				continue
			}
			for a, b := range baseline {
				p, ok := readPosBE(h, a)
				if !ok {
					continue
				}
				x, y, z := p[0], p[1], p[2]
				if abs32(x-b[0]) < 0.5 && abs32(y-b[1]) < 0.5 && abs32(z-b[2]) < 0.5 {
					continue
				}
				if !posRangeOK(x, y, z) {
					continue
				}
				movedCount[a]++
				if movedCount[a] >= 3 {
					bestAddr = a
					break
				}
			}
			if bestAddr != 0 {
				break
			}
		}
		if bestAddr == 0 {
			fmt.Printf("  [motion] no movement observed in 30s (%d candidates watched) - will retry later\n", len(baseline))
			return 0
		}
		p, _ := readPosBE(h, bestAddr)
		fmt.Printf("  [motion] player locked at 0x%X mem=(%.1f, %.1f, %.1f)\n", bestAddr, p[0], p[1], p[2])
		return bestAddr
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
	target = nil
	targetMu.Unlock()
}

// beep 在终端发出 n 次响铃，提示玩家走动。
func beep(n int) {
	for i := 0; i < n; i++ {
		fmt.Print("\a")
		time.Sleep(200 * time.Millisecond)
	}
}
