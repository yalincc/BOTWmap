//go:build windows

// appid_windows.go — Windows 任务栏身份与图标。
//
// 无边框 Wails 窗口默认既不设置 WM_SETICON、也不声明 AppUserModelID，
// Windows 任务栏会按"默认程序"归组取图标，表现为图标偶发变成系统默认图标。
// 这里做两件事：启动前声明 AppUserModelID，启动后给主窗口显式设置 exe 内图标。
//
// 注意：所有 Win32 调用都先 Find() 判空，找不到就跳过，避免 LazyProc.Call 因
// 符号缺失直接 panic 把整个程序带崩。

package main

import (
	"syscall"
	"unsafe"
)

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	user32              = syscall.NewLazyDLL("user32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSetAppID        = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procEnumWindows     = user32.NewProc("EnumWindows")
	procGetWindowThread = user32.NewProc("GetWindowThreadProcessId")
	procPostMessage     = user32.NewProc("PostMessageW")
	procLoadImage       = user32.NewProc("LoadImageW")
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmSetIcon     = 0x0080
	iconBig       = 1
	iconSmall     = 0
	imageIcon     = 1
	lrDefaultSize = 0x0040
)

// ico 资源里常见的图标组 ID，逐个尝试。
var iconResIDs = []uintptr{1, 2, 3}

// enumWindowsProc 保持包级变量，避免回调被 GC 回收。
var enumWindowsProc = syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
	if procGetWindowThread.Find() != nil || procGetModuleHandle.Find() != nil ||
		procLoadImage.Find() != nil || procPostMessage.Find() != nil {
		return 1
	}
	var wpid uint32
	procGetWindowThread.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
	if wpid != uint32(syscall.Getpid()) {
		return 1
	}
	hInst, _, _ := procGetModuleHandle.Call(0)
	if hInst == 0 {
		return 1
	}
	for _, id := range iconResIDs {
		// MAKEINTRESOURCE：资源 ID 直接传值，不传指针
		img, _, _ := procLoadImage.Call(hInst, id, imageIcon, 0, 0, lrDefaultSize)
		if img == 0 {
			continue
		}
		// PostMessage 而非 SendMessage：避免跨线程同步等待造成死锁
		procPostMessage.Call(hwnd, wmSetIcon, iconBig, img)
		procPostMessage.Call(hwnd, wmSetIcon, iconSmall, img)
		break
	}
	return 1
})

// setAppUserModelID 声明进程的应用身份，需在窗口创建前调用。
func setAppUserModelID(id string) {
	if procSetAppID.Find() != nil {
		return
	}
	if p, err := syscall.UTF16PtrFromString(id); err == nil {
		procSetAppID.Call(uintptr(unsafe.Pointer(p)))
	}
}

// setWindowIcon 给本进程的所有顶层窗口设置 exe 内图标。
func setWindowIcon() {
	if procEnumWindows.Find() != nil {
		return
	}
	procEnumWindows.Call(enumWindowsProc, 0)
}
