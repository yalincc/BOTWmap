package main

// calibrate.go — TOTK 基准校准（指针扫描定位方案 S3，2026-10-01）
//
// 用户在 TOTK 游戏地图上读出当前坐标，输入 GUI → GUI 写 calibrate.json（文件通信，
// 遵守双入口原则：GUI 不依赖 8766 HTTP）→ core 轮询发现 → 在最近一次扫描候选里
// 找匹配地址（连续验证）→ 指针扫描建链 → 经 calibSetCh 通知 watch 循环切换 CHAIN 模式。
//
// 隔离红线：本模块不修改自动定位的任何状态（lock/state 只读），校准产物只经
// calibSetCh 交给 watch 循环；BOTW（ChainCapable=false）直接拒绝。

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

const (
	calibTol          = 3.0  // 基准坐标匹配容差（米）
	calibFreshWindow  = 15 * time.Minute
	calibVerifyN      = 5  // 连续验证读数次数
	calibVerifyGap    = 300 * time.Millisecond
	calibFileName     = "calibrate.json"
)

// ---- 跨文件共享状态 ----

var (
	calibMu    sync.Mutex
	calibRead  []*Group  // 最近一次扫描的候选组（watch 循环写入）
	calibAt    time.Time
	calibSetCh = make(chan *totkChainRT, 1) // 校准成功 → watch 循环切 CHAIN

	locMu     sync.RWMutex
	locMode   string // "chain" | "probe" | "fixed" | ""
	chainStat struct {
		OK, Fail int
		Ptr      uintptr
	}
)

// calibStashGroups watch 循环扫描后调用（缓存候选给校准用）。
func calibStashGroups(ranked []*Group) {
	calibMu.Lock()
	calibRead = ranked
	calibAt = time.Now()
	calibMu.Unlock()
}

// SetLocMode watch 循环设置当前定位模式（status.json 展示）。
func SetLocMode(m string) {
	locMu.Lock()
	locMode = m
	locMu.Unlock()
}

// GetLocStatus status.json 读取。
func GetLocStatus() (string, int, int) {
	locMu.RLock()
	defer locMu.RUnlock()
	return locMode, chainStat.OK, chainStat.Fail
}

func locModeString() string {
	locMu.RLock()
	defer locMu.RUnlock()
	if locMode == "" {
		return "idle"
	}
	return locMode
}

func setChainStat(rt *totkChainRT) {
	locMu.Lock()
	chainStat.OK = rt.Rec.OK
	chainStat.Fail = rt.Rec.Fail
	chainStat.Ptr = rt.PtrAddr
	locMu.Unlock()
}

// ---- 校准请求轮询（GUI 写文件 → core 消费）----

type calibRequest struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"` // 高度（游戏内第二个数）
	Z  float64 `json:"z"`
	TS int64   `json:"ts"`
}

func init() {
	go calibPoller()
}

func calibPoller() {
	var lastTS int64
	path := calibFileName
	for {
		time.Sleep(time.Second)
		buf, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req calibRequest
		if json.Unmarshal(buf, &req) != nil || req.TS == 0 || req.TS == lastTS {
			continue
		}
		lastTS = req.TS
		fmt.Printf("  [calib] request: (%.1f, %.1f, %.1f)\n", req.X, req.Y, req.Z)
		msg, rt := runCalibration(req.X, req.Y, req.Z)
		locMu.Lock()
		if rt != nil {
			fmt.Printf("  [calib] OK: %s\n", msg)
		} else {
			fmt.Printf("  [calib] FAILED: %s\n", msg)
		}
		locMu.Unlock()
		os.Remove(path)
	}
}

// runCalibration 执行校准：匹配候选 → 连续验证 → 建链 → 通知 watch 循环。
func runCalibration(x, y, z float64) (string, *totkChainRT) {
	p := currentPlatform()
	if p == nil || currentGame == nil || !currentGame.ChainCapable() {
		return "当前游戏/平台不支持（仅 TOTK + Ryujinx）", nil
	}
	calibMu.Lock()
	groups := calibRead
	at := calibAt
	calibMu.Unlock()
	if len(groups) == 0 || time.Since(at) > calibFreshWindow {
		return "没有可用的扫描候选：请先启动自动定位（解锁状态下扫描一次）再校准", nil
	}
	h := p.Handle()

	// 游戏内坐标读数可能是 (x, alt, z) 或 (x, z, alt) 两种口误顺序——都试
	in1 := [3]float64{x, y, z}
	in2 := [3]float64{x, z, y}
	match := func(gx, gy, gz float32) bool {
		for _, in := range [2][3]float64{in1, in2} {
			if math.Abs(float64(gx)-in[0]) <= calibTol &&
				math.Abs(float64(gy)-in[1]) <= calibTol &&
				math.Abs(float64(gz)-in[2]) <= calibTol {
				return true
			}
		}
		return false
	}

	// 1) 在候选组里找匹配地址
	var cands []uintptr
	seen := map[uintptr]bool{}
	for _, g := range groups {
		gx, gy, gz := currentGame.Display([3]float32{g.X, g.Y, g.Z})
		if !match(gx, gy, gz) {
			continue
		}
		for _, ad := range g.Addrs {
			if !seen[ad] {
				seen[ad] = true
				cands = append(cands, ad)
			}
		}
	}
	if len(cands) == 0 {
		return fmt.Sprintf("候选里找不到 (%.0f, %.0f, %.0f) ±%.0fm 的地址：确认游戏内读数与输入一致，且人物在此位置", x, y, z, calibTol), nil
	}
	fmt.Printf("  [calib] %d candidate addr(s) matched\n", len(cands))

	// 2) 连续验证：每个候选连读 calibVerifyN 次，每次都必须仍在基准位置 ±容差
	for _, ad := range cands {
		okAll := true
		for i := 0; i < calibVerifyN; i++ {
			d := currentGame.DecodeAt(h, ad)
			if d == nil {
				okAll = false
				break
			}
			gx, gy, gz := currentGame.Display([3]float32{d[0], d[1], d[2]})
			if !match(gx, gy, gz) {
				okAll = false
				break
			}
			if i < calibVerifyN-1 {
				time.Sleep(calibVerifyGap)
			}
		}
		if !okAll {
			continue
		}
		// 3) 建链（全内存指针扫描 ~3s）
		d := currentGame.DecodeAt(h, ad)
		if d == nil {
			continue
		}
		rt := totkChainBuild(h, ad, [3]float32{d[0], d[1], d[2]})
		if rt == nil {
			return fmt.Sprintf("地址 0x%X 验证通过，但指针扫描未找到可用链（将保持自动定位）", ad), nil
		}
		// 4) 通知 watch 循环切 CHAIN（非阻塞；watch 忙时丢弃本次，候选下次再试）
		select {
		case calibSetCh <- rt:
		default:
		}
		return fmt.Sprintf("校准成功：地址 0x%X，链已启用（红点应钉在输入位置，随移动跟随）", ad), rt
	}
	return "候选验证失败：请站在输入坐标处保持 2 秒不动再校准", nil
}
