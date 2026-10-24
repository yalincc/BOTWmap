// tracker.go — 统一追踪管道接口与全局状态发布。
//
// 契约设计遵循 PLAN-XNAVI-PIPELINE-REBUILD：
// 各平台追踪管道（Cemu、Ryujinx、Eden）全生命周期自闭环，
// 输出只有统一原子状态发布 publishState。

package main

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// RescanRequest 传递给当前活跃 Tracker 的重扫或校准请求。
type RescanRequest struct {
	HasTarget bool
	TargetX   float32
	TargetZ   float32
}

// Tracker 是统一的定位追踪管道接口。
// 每个平台/游戏组合作为一个独立的 Tracker。
type Tracker interface {
	Name() string
	Start(ctx context.Context)
	Stop()
	Rescan(req *RescanRequest)
}

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

var (
	rescanReqCh = make(chan *RescanRequest, 5)
	useSaveScan = true
)

// primaryPort HTTP 服务实际主端口（v3.2.2 端口自愈：8766 被占时顺延，status.json 回报给 GUI）。
var primaryPort = 8766

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
	fmt.Printf("  [sm] -> UNLOCKED (%s)\n", reason)
}

// publishState 发布全局统一位置状态。
func publishState(ok bool, gx, gy, gz float32, verified bool, copies int, source string) {
	state.mu.Lock()
	state.ok = ok
	if ok {
		state.gx, state.gy, state.gz = gx, gy, gz
		if currentGame != nil {
			state.mx, state.my = currentGame.ToMap(gx, gy, gz)
			state.layer = currentGame.LayerOf(gx, gy, gz)
		}
		state.age = float64(time.Now().UnixNano()) / 1e9
		state.verified = verified
		state.copies = copies
	}
	state.source = source
	state.mu.Unlock()
}

func requestRescan() {
	select {
	case rescanReqCh <- &RescanRequest{}:
	default:
	}
}

func requestRescanWithTarget(x, z float32) {
	select {
	case rescanReqCh <- &RescanRequest{HasTarget: true, TargetX: x, TargetZ: z}:
	default:
	}
}

func dist3(a, b [3]float32) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// isShrineScale 严格的神庙试炼场地下本地尺度判定。
// 物理核心判据：
//  1. 垂直海拔：海拔 Y < 50m（海拉鲁平原中心大世界地面海拔普遍在 Y=100m~138m，平原之塔 Y≈250m，
//     大世界原点周边地表绝无可能 Y<50m；神庙试炼场地板恒在 Y∈[-10m, 45m] 处于地下空间）；
//  2. 水平范围：|X| < 300m && |Z| < 300m（涵盖 DLC 罗塔·奇格尔神庙 Z=108.9m 等超长长廊神庙及剑之试炼大厅）。
func isShrineScale(pos [3]float32) bool {
	return float64(pos[1]) < 50.0 &&
		math.Abs(float64(pos[0])) < 300.0 &&
		math.Abs(float64(pos[2])) < 300.0
}

// formatDestName 识别传送或神庙位置对应的地标名称。
func formatDestName(pos [3]float32) string {
	if currentGame != nil {
		if pnt := currentGame.MatchShrine(pos[0], pos[1], pos[2]); pnt != nil {
			if pnt.T == "tower" {
				return "高塔: " + pnt.Cn
			}
			return "神庙: " + pnt.Cn
		}
	}
	return "大世界"
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

