// locate.go — Savage Wind（BOTW PC 重编译版）定位主循环
//
// ★ 架构（2026-11-06 换血，替代原来的「写死偏移」方案）：
//
//	L0  缓存槽直读    读「静态槽 → 对象 → 坐标」，微秒级，常态走这条
//	L1  静态区重找    扫映射起始 512MB（实测 49MB / 50ms），跑判别漏斗，约 220ms
//	L2  全窗口兜底    用真值锚点（游戏日志 [place] / F10）扫全部 guest 区，1.5~3s
//
// 每拍（200ms）只做 L0；每 2 秒做一次结构深度校验：
//
//	obj+0x00 必须是指向静态区的指针
//	obj+0x120 必须自指（值 == obj+0x120）
//
// 深度校验连续失败 → 降 L1；L1 失败 → 降 L2；都失败才标记离线。
//
// ★ 纪律（这是 TOTKmap 踩过的坑，别再犯）：
//   - 只有本文件有权改「当前锁」。守护逻辑只能"要求重锁"，不能自己换地址。
//   - 坐标长时间不动 ≠ 失效（玩家就是站着不动），必须先做结构校验再决定。
//   - 位移突增 = 玩家传送（合法），不要当异常；只有"来回交替打乒乓"才是坏锁。
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type locatorState struct {
	OK       bool
	Verified bool
	GX       float32 // X 东
	GY       float32 // alt 高
	GZ       float32 // Z 南+
	MX       float32 // 地图像素
	MY       float32
	Age      float64
	Source   string
	PID      uint32
	Offset   uintptr // 当前静态槽 guest VA 偏移（诊断/展示用）
	Rescans  int
	Note     string
}

type locator struct {
	mu sync.RWMutex
	st locatorState

	h          syscall.Handle
	pid        uint32
	procName   string
	lastPidChk time.Time

	// 当前锁
	lock    chainLock
	obj     uintptr
	hasLock bool
	cache   *chainCache
	fp      string // 游戏构建指纹

	// 失败与重扫节奏
	fail      int
	lastScan  time.Time
	deepFails int

	// 深度校验
	lastDeep time.Time

	// 冻结观察
	lastPos     [3]float32
	freezeSince time.Time

	// 乒乓观察（坏锁：在两处之间来回跳）
	prevPos  [3]float32
	prevPrev [3]float32
	pingPong int

	// 神庙驻留
	shrineMode     bool
	shrineEntrance [3]float32
	hasPrev        bool

	rescans int
}

var (
	gamePathMu  sync.RWMutex
	gameExePath string
)

func setGameExePath(p string) {
	gamePathMu.Lock()
	gameExePath = p
	gamePathMu.Unlock()
}

func getGameExePath() string {
	gamePathMu.RLock()
	defer gamePathMu.RUnlock()
	return gameExePath
}

// rescanReq 由 /rescan 置位，由 tick 串行消费（避免与定位循环并发读句柄）。
var rescanReq atomic.Bool

func newLocator(name string) *locator {
	l := &locator{procName: name, lock: chainLock{Field: posFieldDefault}}
	l.cache = loadChainCache()
	return l
}

// resolvePID 优先用手动指定的 PID（--pid），否则自动发现游戏进程。
func resolvePID(want string) uint32 {
	if manualPID != 0 {
		return manualPID
	}
	return findProcessPID(want)
}

// snapshot 供 /pos 与 status.json 读取。
func (l *locator) snapshot() locatorState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.st
}

// isShrineScale 判定是否落在神庙内部局部坐标系（|X|<300, |Z|<300, Y<50）。
// 实测：Savage Wind 神庙内坐标为 -75.50/15.70/75.50 这类小值，判据成立。
func isShrineScale(pos [3]float32) bool {
	return float64(pos[1]) < 50.0 &&
		math.Abs(float64(pos[0])) < 300.0 &&
		math.Abs(float64(pos[2])) < 300.0
}

func dist3(a, b [3]float32) float32 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// validTriple 世界坐标合法性（与 xnavi 模拟器版 gameValidTriple 同口径）。
func validTriple(x, alt, z float32) bool {
	if x != x || alt != alt || z != z {
		return false
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	ax, az := math.Abs(float64(x)), math.Abs(float64(z))
	if ax < 5 && az < 5 {
		return false
	}
	if (ax < 5 && alt < 5) || (az < 5 && alt < 5) {
		return false
	}
	if ax < 1e-20 || az < 1e-20 {
		return false
	}
	if x <= -7000 || x >= 7000 || z <= -7000 || z >= 7000 || alt <= -600 || alt >= 5000 {
		return false
	}
	if ax < 100 && az < 100 && alt < 100 {
		return false
	}
	// 拒绝 denormal / 极小值：指针字节被当 float 读时常落在 1e-30 量级，
	// 会混过纯值域校验（2026-11-06 踩过：+0x8 的指针被当成坐标）。
	for _, v := range []float32{x, alt, z} {
		a := math.Abs(float64(v))
		if a != 0 && a < 1e-6 {
			return false
		}
	}
	return true
}

func (l *locator) markOffline(note string) {
	l.mu.Lock()
	changed := l.st.Note != note
	l.st.OK = false
	l.st.Verified = false
	l.st.Note = note
	l.st.Source = ""
	l.mu.Unlock()
	// GUI 日志面板/控制台只在状态变化时提示一次，避免刷屏。
	if changed {
		fmt.Printf("  [status] %s\n", note)
	}
}

func (l *locator) updateState(pos [3]float32, source string, verified bool) {
	l.mu.Lock()
	l.st.OK = true
	l.st.Verified = verified
	l.st.GX, l.st.GY, l.st.GZ = pos[0], pos[1], pos[2]
	l.st.MX, l.st.MY = 2*pos[0]+12000, 2*pos[2]+10000
	l.st.Age = float64(time.Now().UnixNano()) / 1e9
	l.st.Source = source
	l.st.Offset = l.lock.SlotOff
	l.st.Note = ""
	l.mu.Unlock()
}

// ---- 加锁：L0 缓存 → L1 静态区 → L2 真值锚点 ----

// tryCachedLock 用缓存槽读一次并做结构校验。
func (l *locator) tryCachedLock() bool {
	if l.cache == nil || l.cache.SlotOff == 0 {
		return false
	}
	// 坐标字段只认 posFieldDefault。历史上"自动学习"学错过一次（field=0x8），
	// 那种缓存必须作废，否则会一直读垃圾。
	if l.cache.Field != posFieldDefault {
		fmt.Printf("  [lock] 缓存里的坐标字段可疑（0x%X，期望 0x%X），忽略缓存\n", l.cache.Field, posFieldDefault)
		l.cache = nil
		return false
	}
	obj, ok := l.ptrAt(chainGuestBase + l.cache.SlotOff)
	if !ok || obj < chainGuestBase || obj >= chainGuestLimit {
		return false
	}
	if _, ok := l.tripleAt(obj + posFieldDefault); !ok {
		return false
	}
	if !l.selfRefOK(obj) {
		return false
	}
	l.lock = chainLock{SlotOff: l.cache.SlotOff, Slot2Off: l.cache.Slot2Off, Field: posFieldDefault}
	l.obj = obj
	l.hasLock = true
	return true
}

// acquireLock 逐级降级找锁。返回是否成功。
func (l *locator) acquireLock() bool {
	if l.fp == "" {
		l.fp = gameFingerprint(getGameExePath())
	}
	// L0：缓存（指纹一致才有意义）
	if l.cache != nil && l.cache.Fingerprint == l.fp {
		if l.tryCachedLock() {
			l.mu.Lock()
			l.st.Rescans++
			l.mu.Unlock()
			fmt.Printf("  [lock] 缓存槽命中（槽 0x%X，字段 0x%X）对象 0x%X\n",
				l.cache.SlotOff, l.cache.Field, l.obj)
			return true
		}
		fmt.Println("  [lock] 缓存槽失效，降级静态区重找")
	}

	// L1：静态区重找
	if lk, obj, diag, ok := l.resolveByStaticSlots(); ok {
		l.lock, l.obj, l.hasLock = lk, obj, true
		l.rememberLock("静态区漏斗")
		fmt.Printf("  [lock] 静态区命中：%s\n", diag)
		return true
	} else {
		fmt.Printf("  [lock] 静态区未命中：%s\n", diag)
	}

	// L2：真值锚点全窗口兜底
	anchor, okAnchor := latestPlaceAnchor(getGameExePath())
	if !okAnchor && l.hasPrev {
		anchor, okAnchor = l.prevPos, true // 用最后已知位置做锚（玩家可能就在附近）
	}
	if okAnchor {
		fmt.Printf("  [lock] 启用全窗口兜底，锚点 %.2f/%.2f/%.2f…\n", anchor[0], anchor[1], anchor[2])
		if lk, obj, ok := l.fullScanWithAnchor(anchor); ok {
			l.lock, l.obj, l.hasLock = lk, obj, true
			l.rememberLock("全窗口锚点")
			fmt.Printf("  [lock] 全窗口命中：对象 0x%X 槽 0x%X 字段 0x%X\n", obj, lk.SlotOff, lk.Field)
			return true
		}
		fmt.Println("  [lock] 全窗口兜底未命中")
	}

	l.markOffline("未找到玩家对象（游戏可能还在标题/加载界面）")
	return false
}

// rememberLock 锁定成功后写缓存。
func (l *locator) rememberLock(how string) {
	l.mu.Lock()
	l.st.Rescans++
	l.mu.Unlock()
	if l.fp == "" {
		l.fp = gameFingerprint(getGameExePath())
	}
	c := &chainCache{
		Fingerprint: l.fp,
		SlotOff:     l.lock.SlotOff,
		Slot2Off:    l.lock.Slot2Off,
		Field:       l.lock.Field,
		ObjHint:     l.obj,
		SavedAt:     time.Now().Format("2006-01-02 15:04:05"),
		Note:        how,
	}
	l.cache = c
	saveChainCache(c)
}

// dropLock 丢弃当前锁，等下一拍重新 acquire。
func (l *locator) dropLock(why string) {
	if l.hasLock {
		fmt.Printf("  [lost] 丢弃当前锁：%s\n", why)
	}
	l.hasLock = false
	l.obj = 0
	l.fail = 0
	l.deepFails = 0
	l.freezeSince = time.Time{}
}

// tick 每拍调用一次（200ms）。
func (l *locator) tick() {
	if rescanReq.Swap(false) {
		l.lastScan = time.Now()
		l.dropLock("用户请求重新定位")
		l.cache = nil
	}

	l.checkCalibration()

	// 句柄存活时快速校验进程是否已退出（GetExitCodeProcess）。
	if l.h != 0 {
		var exitCode uint32
		if err := syscall.GetExitCodeProcess(l.h, &exitCode); err == nil && exitCode != 259 /* STILL_ACTIVE */ {
			syscall.CloseHandle(l.h)
			l.h = 0
			l.pid = 0
			l.dropLock("游戏进程已退出")
			l.shrineMode = false
			l.hasPrev = false
			l.cache = nil
			l.fp = ""
			l.mu.Lock()
			l.st.PID = 0
			l.mu.Unlock()
			l.markOffline("等待 Savage Wind 启动")
			return
		}
	}

	// 每 2 秒核对进程是否还是同一个（游戏重启后 PID 会变）。
	if time.Since(l.lastPidChk) > 2*time.Second {
		l.lastPidChk = time.Now()
		if pid := resolvePID(l.procName); pid == 0 || (l.pid != 0 && pid != l.pid) {
			if l.h != 0 {
				syscall.CloseHandle(l.h)
				l.h = 0
			}
			l.pid = 0
			l.dropLock("游戏进程变化")
			l.shrineMode = false
			l.hasPrev = false
			l.cache = nil
			l.fp = ""
			l.mu.Lock()
			l.st.PID = 0
			l.mu.Unlock()
		}
	}

	if l.h == 0 {
		pid := resolvePID(l.procName)
		if pid == 0 {
			l.markOffline("等待 Savage Wind 启动")
			return
		}
		h := openProcessRO(pid)
		if h == 0 {
			l.markOffline("无法只读打开游戏进程")
			return
		}
		// ★ 进程刚退出时，Windows 还没把它从进程表摘掉，OpenProcess 仍会成功。
		// 必须试读一次 guest 映射，确认真的可用，否则会把"正在死的进程"当成新进程
		// 反复附加 + 空扫一次静态区（2026-11-06 实测到的假附加）。
		if _, ok := readProcess(h, chainGuestBase, 16); !ok {
			syscall.CloseHandle(h)
			l.markOffline("游戏进程未就绪（正在启动或退出）")
			return
		}
		l.h = h
		l.pid = pid
		setGameExePath(processExePath(h))
		l.cache = loadChainCache()
		l.fp = ""
		l.mu.Lock()
		l.st.PID = pid
		l.mu.Unlock()
		fmt.Printf("  [game] 已附加 savagewind.exe，PID %d\n", pid)
	}

	if !l.hasLock {
		// 限频：两次重找至少间隔 3 秒，避免拖累游戏
		if !l.lastScan.IsZero() && time.Since(l.lastScan) < 3*time.Second {
			return
		}
		l.lastScan = time.Now()
		if !l.acquireLock() {
			return
		}
	}

	cur, ok := l.tripleAt(l.obj + l.lock.Field)
	if !ok {
		l.fail++
		if l.fail >= 5 {
			l.dropLock(fmt.Sprintf("连续 %d 拍读不到合法坐标", l.fail))
		}
		l.markOffline("坐标读取异常，正在重试")
		return
	}
	l.fail = 0

	// 每 2 秒做一次结构深度校验（判断"锁还是不是玩家"）。
	if time.Since(l.lastDeep) > 2*time.Second {
		l.lastDeep = time.Now()
		if !l.selfRefOK(l.obj) || !l.staticPtrAt0(l.obj) {
			l.deepFails++
			if l.deepFails >= 2 {
				l.dropLock("结构校验连续失败（对象已被回收或搬走）")
				return
			}
		} else {
			l.deepFails = 0
		}
	}

	// ---- 神庙驻留（判据与模拟器版一致，实测神庙内是小坐标） ----
	isShrine := isShrineScale(cur)
	if l.shrineMode {
		if !isShrine && validTriple(cur[0], cur[1], cur[2]) {
			l.shrineMode = false
			fmt.Printf("  [shrine] 离开神庙 -> 恢复世界追踪 (%.1f, %.1f, %.1f)\n", cur[0], cur[1], cur[2])
			l.hasPrev = true
			l.prevPos = cur
			l.updateState(cur, "chain", true)
			return
		}
		l.updateState(l.shrineEntrance, "shrine", true)
		return
	}
	if isShrine && l.hasPrev && !isShrineScale(l.prevPos) && dist3(cur, l.prevPos) > 100.0 {
		l.shrineMode = true
		l.shrineEntrance = l.prevPos
		fmt.Printf("  [shrine] 进入神庙 -> 锚定入口 (%.1f, %.1f, %.1f)\n",
			l.shrineEntrance[0], l.shrineEntrance[1], l.shrineEntrance[2])
		l.updateState(l.shrineEntrance, "shrine", true)
		return
	}

	// ---- 正常大世界 ----
	hadPrev := l.hasPrev
	prev := l.prevPos
	prevPrev := l.prevPrev
	l.hasPrev = true
	l.prevPrev = prev
	l.prevPos = cur

	// 乒乓检测：交替复用槽会导致两点之间来回跳（TOTKmap 415m 乱跳的根因）。
	// 注意：单次大位移 = 玩家传送，是合法行为，不能当异常处理。
	if hadPrev && dist3(cur, prev) > 50 && dist3(cur, prevPrev) < 10 {
		l.pingPong++
		if l.pingPong >= 3 {
			l.pingPong = 0
			l.dropLock("位置在两处之间反复跳变（疑似坏锁）")
			return
		}
	} else {
		l.pingPong = 0
	}

	l.updateState(cur, "chain", true)
}

// checkCalibration 消费 GUI 提交的 calibrate.json（F10 屏幕坐标手动校准）。
// 新架构下它有两个用途：确认当前读数是准的 / 作为 L2 的真值锚点。
func (l *locator) checkCalibration() {
	buf, err := os.ReadFile("calibrate.json")
	if err != nil {
		return
	}
	_ = os.Remove("calibrate.json") // 消费后立刻清理

	var req struct {
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
		Z  float64 `json:"z"`
		TS int64   `json:"ts"`
	}
	if err := json.Unmarshal(buf, &req); err != nil {
		return
	}
	tx, ty, tz := float32(req.X), float32(req.Y), float32(req.Z)
	fmt.Printf("  [calib] 收到手动校准: %.2f / %.2f / %.2f\n", tx, ty, tz)
	if l.h == 0 {
		fmt.Println("  [calib] 游戏未附加，无法执行校准")
		return
	}
	// 1) 当前锁就对：直接确认
	if l.hasLock {
		if cur, ok := l.tripleAt(l.obj + l.lock.Field); ok {
			xMatch := math.Abs(float64(cur[0]-tx)) < 1.0
			yMatch := (ty == 0) || (math.Abs(float64(cur[1]-ty)) < 1.0)
			zMatch := math.Abs(float64(cur[2]-tz)) < 1.0
			if xMatch && yMatch && zMatch {
				fmt.Printf("  [calib] ★ 当前链式读数与校准值一致（%.2f/%.2f/%.2f），已确认\n", cur[0], cur[1], cur[2])
				return
			}
		}
	}
	// 2) 不一致：拿校准值当锚点做全窗口搜索并重建锁
	if lk, obj, ok := l.fullScanWithAnchor([3]float32{tx, ty, tz}); ok {
		l.lock, l.obj, l.hasLock = lk, obj, true
		l.rememberLock("F10 校准")
		fmt.Printf("  [calib] ★ 校准命中：对象 0x%X 槽 0x%X 字段 0x%X\n", obj, lk.SlotOff, lk.Field)
	} else {
		fmt.Println("  [calib] 未匹配到该坐标，请确认角色正站在该位置且未暂停")
	}
}

// exactCoordHit3D 保留给单测用（历史校准算法的定值匹配）。
func exactCoordHit3D(d []byte, tx, ty, tz float32) (uintptr, bool) {
	for i := 0; i+12 <= len(d); i += 4 {
		x := math.Float32frombits(binary.LittleEndian.Uint32(d[i:]))
		if math.Abs(float64(x-tx)) >= 0.5 {
			continue
		}
		alt := math.Float32frombits(binary.LittleEndian.Uint32(d[i+4:]))
		if math.Abs(float64(alt-ty)) >= 0.5 {
			continue
		}
		z := math.Float32frombits(binary.LittleEndian.Uint32(d[i+8:]))
		if math.Abs(float64(z-tz)) >= 0.5 {
			continue
		}
		return uintptr(i), true
	}
	return 0, false
}
