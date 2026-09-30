// win_title.go — 游戏窗口标题探测（V2.2.0 Q8 游戏识别主判定）。
//
// Ryujinx 游戏窗口标题自带游戏名 + Title ID（本机实测）：
//   "Ryujinx 1.3.351 / 塞尔达传说 王国之泪 / v1.2.1 / (0100F2C0115B6000) (64-bit)"
//
// Title ID 是模拟器自己报告的硬标识：零内存扫描、无假阳性、不受传送/加载期
// 读数瞬时性影响。这是 Cheat Engine、VNPresence 等模拟器工具的通用做法。
// 接入新游戏 = titleTIDs 加一行映射。
package main

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32Mod                    = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32Mod.NewProc("EnumWindows")
	procGetWindowTextW           = user32Mod.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32Mod.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcessId = user32Mod.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32Mod.NewProc("IsWindowVisible")
)

// titleTIDs Title ID → 游戏标识（小写匹配）。新增游戏加一行。
var titleTIDs = []struct {
	tid  string
	game string
}{
	{"0100f2c0115b6000", "totk"}, // 塞尔达传说 王国之泪
	{"01007ef00011e000", "botw"}, // 塞尔达传说 旷野之息
}

// detectGameByTitle 枚举 pid 进程的可见顶层窗口，标题含已知 Title ID → 对应游戏。
// 找不到（窗口未就绪/非 Nintendo Switch 平台）返回 ""，由调用方走内存感知兜底。
func detectGameByTitle(pid uint32) string {
	if pid == 0 {
		return ""
	}
	found := ""
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1 // 继续枚举
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		n, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, n+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
		title := strings.ToLower(syscall.UTF16ToString(buf))
		for _, m := range titleTIDs {
			if strings.Contains(title, m.tid) {
				found = m.game
				return 0 // 命中，停止枚举
			}
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}
