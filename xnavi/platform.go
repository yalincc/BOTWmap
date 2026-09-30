// platform.go — 平台适配层。
//
// 把模拟器差异（进程识别、内存枚举、端序、存档发现、主档策略）全部隔离在
// 平台驱动里，定位状态机、扫描、进度、服务端均只面向 Platform 接口编程。
// 游戏层差异（坐标校验/地图换算/layer）在 game.go，与平台解耦。
//
// 设计依据：ds 老师《TOTKmap 定位程序（Go）多模块技术开发说明文档》第 3.1 节
// "平台适配层"——识别模拟器类型、附加进程、枚举模块、提供只读内存读取、
// 提供特征码扫描、处理不同模拟器的内存布局差异。

package main

import (
	"fmt"
	"sync"
)

// MemBlock 一块已提交的可读写内存区域。
type MemBlock struct {
	Base uintptr
	Size uintptr
}

// Platform 描述一个模拟器平台的只读访问能力。
// 实现必须保证：只读、不注入、不修改游戏内存。
type Platform interface {
	Name() string // "Ryujinx" / "Cemu"

	// Attach 确保已附加到"正在运行游戏"的模拟器实例；未运行返回 false。
	// 失败时内部不应持有半开句柄（可重试）。
	Attach() bool
	// Handle 返回当前进程句柄（Attach 成功后有效；0 = 未附加）。
	Handle() uintptr
	// PID 返回当前附加的进程 ID。
	PID() uint32

	// Blocks 返回 ≥minMB 的已提交可读写内存块（游戏内存所在区域）。
	Blocks(minMB float64) []MemBlock
	// LargestBlock 返回最大的一块（known 偏移重定位的稳定锚点）。
	LargestBlock(minMB float64) (uintptr, uintptr)

	// DecodeTriple 端序解码 12 字节 → 内存序三元组 (X, alt, Z)。
	// 读取失败或全零返回 ok=false。
	DecodeTriple(d []byte) (x, alt, z float32, ok bool)

	// LittleEndian 返回平台内存字节序是否小端（Switch=小端，Wii U=大端）。
	LittleEndian() bool

	// SaveRoots 存档根候选目录（递归找 game_data.sav 的起点）。
	SaveRoots() []string

	// KnownOffsets 返回该平台已知的玩家坐标偏移候选（相对 LargestBlock 基址），
	// 按优先级排序。启动时优先用这些偏移直接读，失败再回退全量扫描。
	KnownOffsets() []uintptr
}

// ---- 当前平台管理 ----

var (
	platMu    sync.Mutex
	curPlat   Platform
	probeList []Platform

	// autoPlatform 是否允许平台自动切换（--emu=auto 时 true；显式指定则不切换）。
	autoPlatform bool
)

// registerPlatform 注册一个平台驱动（init 阶段调用）。
func registerPlatform(p Platform) {
	probeList = append(probeList, p)
}

// setPlatform 固定当前平台（main 解析 --emu 参数后调用）。
func setPlatform(p Platform) {
	platMu.Lock()
	curPlat = p
	platMu.Unlock()
	if p != nil {
		fmt.Printf("  [platform] selected: %s\n", p.Name())
	}
}

// currentPlatform 返回当前平台；nil 表示尚未确定。
func currentPlatform() Platform {
	platMu.Lock()
	defer platMu.Unlock()
	return curPlat
}

// probePlatform 自动探测：按注册顺序找第一个"模拟器进程在运行"的平台。
// 都不在运行则保持当前选择（若未选择则固定第一个注册的平台，等 watchdog 追上来）。
func probePlatform() Platform {
	platMu.Lock()
	defer platMu.Unlock()
	for _, p := range probeList {
		if p.Attach() {
			if curPlat == nil || curPlat.Name() != p.Name() {
				fmt.Printf("  [platform] auto-detected: %s\n", p.Name())
			}
			curPlat = p
			return p
		}
	}
	if curPlat == nil && len(probeList) > 0 {
		fmt.Printf("  [platform] no emulator running yet, defaulting to %s (will keep looking)\n", probeList[0].Name())
		curPlat = probeList[0]
	}
	return curPlat
}

// ensureAttach 确保当前平台已附加；未附加则重试探测。
// 句柄存在但已失效（进程退出后句柄作废）时也会重开，防止死句柄 0 块卡死。
func ensureAttach() bool {
	p := currentPlatform()
	if p == nil {
		p = probePlatform()
		if p == nil {
			return false
		}
	}
	if p.Handle() != 0 && hLive(p.Handle()) {
		return true
	}
	return p.Attach()
}

// platformWithGame 找"进程在跑且已加载游戏大块内存（≥256MB）"的注册平台，
// 用于当前平台 0 块卡死（游戏未加载/切换模拟器）时的自动切换。
// 按名字排除当前平台（curPlat 与 probeList 里的实例可能是不同指针）。
func platformWithGame(exclude Platform) Platform {
	exName := ""
	if exclude != nil {
		exName = exclude.Name()
	}
	for _, p := range probeList {
		if p.Name() == exName {
			continue
		}
		if !p.Attach() {
			continue
		}
		if len(p.Blocks(256.0)) > 0 {
			fmt.Printf("  [platform] %s has game memory (>=256MB block)\n", p.Name())
			return p
		}
	}
	return nil
}
