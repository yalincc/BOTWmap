// module: live_float
// status: M4 交互完善
// Win32 窗口 + WebView2 嵌入：无边框置顶小窗，不抢游戏焦点，可拖动，位置记忆。
// 注意：透明度由页面 CSS opacity 控制（WebView2 与 WS_EX_LAYERED 不兼容，见 README）。

package main

import (
	"log"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

// ---------- Win32 常量 ----------
const (
	WS_POPUP        = 0x80000000
	WS_CLIPCHILDREN = 0x02000000
	WS_VISIBLE      = 0x10000000

	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000
	WS_EX_TOPMOST    = 0x00000008

	WM_DESTROY = 0x0002
	WM_SIZE    = 0x0005
	WM_CLOSE   = 0x0010
	WM_QUIT    = 0x0012

	GWL_EXSTYLE = -20

	SWP_NOSIZE     = 0x0001
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010

	HWND_TOPMOST = -1

	COINIT_APARTMENTTHREADED = 0x2

	COLOR_WINDOW = 5
	CS_HREDRAW   = 0x0001
	CS_VREDRAW   = 0x0002
)

// ---------- Win32 proc ----------
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")

	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	ole32             = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx = ole32.NewProc("CoInitializeEx")
)

// ---------- 结构 ----------
type rect struct {
	Left, Top, Right, Bottom int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   uintptr
	ClassName  uintptr
	IconSm     uintptr
}

// ---------- 窗口 ----------
type floatWindow struct {
	hwnd   uintptr
	w, h   int32
	chrome *edge.Chromium
	execCh chan string // UI 线程执行队列（WebView2 COM 必须在其创建线程调用）
}

func newFloatWindow(w, h int32) *floatWindow {
	return &floatWindow{w: w, h: h, execCh: make(chan string, 8)}
}

// evalOnUI 将 JS 推入 UI 线程执行队列（非阻塞；WebView2 跨线程调用会崩溃）
func (f *floatWindow) evalOnUI(js string) {
	select {
	case f.execCh <- js:
	default:
	}
}

func (f *floatWindow) getRect() rect {
	var r rect
	procGetWindowRect.Call(f.hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func (f *floatWindow) moveBy(dx, dy int) {
	r := f.getRect()
	procSetWindowPos.Call(f.hwnd, 0,
		uintptr(r.Left)+uintptr(int32(dx)),
		uintptr(r.Top)+uintptr(int32(dy)),
		0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
}

func (f *floatWindow) saveCurrentPos() {
	r := f.getRect()
	// 窗口已销毁时 GetWindowRect 返回全 0，跳过以免覆盖已存位置
	if r.Left == 0 && r.Top == 0 && r.Right == 0 && r.Bottom == 0 {
		return
	}
	savePos(posInfo{X: int(r.Left), Y: int(r.Top)})
}

func (f *floatWindow) quit() {
	// 先保存位置再关窗口，避免销毁后页面 beforeunload 兜底写入 0,0
	f.saveCurrentPos()
	procPostMessageW.Call(f.hwnd, WM_CLOSE, 0, 0)
}

// ---------- WndProc ----------
var (
	wndProcCallback uintptr
	globalWindowPtr uintptr
)

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_SIZE:
		if globalWindowPtr != 0 {
			w := int32(lParam & 0xFFFF)
			h := int32((lParam >> 16) & 0xFFFF)
			if w > 0 && h > 0 {
				win := (*floatWindow)(unsafe.Pointer(globalWindowPtr))
				resizeWebView(win.chrome, w, h)
			}
		}
	case WM_CLOSE:
		procDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// ---------- 创建窗口 ----------
func (f *floatWindow) create() {
	procSetProcessDPIAware.Call()

	// COM 初始化（STA）；S_FALSE（本线程已初始化）也视为成功
	hr, _, _ := procCoInitializeEx.Call(0, COINIT_APARTMENTTHREADED)
	_ = hr

	hinst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("LiveFloatWnd")

	wndProcCallback = syscall.NewCallback(wndProc)

	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		Style:      CS_HREDRAW | CS_VREDRAW,
		WndProc:    wndProcCallback,
		Instance:   hinst,
		Background: COLOR_WINDOW + 1,
		ClassName:  uintptr(unsafe.Pointer(className)),
	}
	clsRet, _, clsErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	log.Printf("[win32] RegisterClassExW ret=%d err=%v", clsRet, clsErr)

	// 初始位置：记忆位置或默认
	p := loadPos()
	x := int32(p.X)
	y := int32(p.Y)
	if x == 0 && y == 0 {
		x = 120
		y = 120
	}

	title, _ := syscall.UTF16PtrFromString("小窗口导航")

	// 关键：先记录窗口对象，窗口创建期间 WndProc 会立即收到消息
	globalWindowPtr = uintptr(unsafe.Pointer(f))

	hwnd, _, winErr := procCreateWindowExW.Call(
		WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE|WS_EX_TOPMOST,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		WS_POPUP|WS_CLIPCHILDREN|WS_VISIBLE,
		uintptr(x), uintptr(y),
		uintptr(f.w), uintptr(f.h),
		0, 0, hinst, 0,
	)
	log.Printf("[win32] CreateWindowExW hwnd=%d err=%v", hwnd, winErr)
	f.hwnd = hwnd
}

// ---------- 消息循环 ----------
// 注意：GetMessageW 在无消息时阻塞，会饿死 execCh（UI 线程执行队列）。
// 改用 PeekMessage 轮询（10ms），保证 WebView2 Eval 任务及时消费。
func (f *floatWindow) run() {
	f.create()
	embedWebView(f, f.hwnd) // 阻塞到 WebView2 就绪
	var m msg
	quit := false
	for !quit {
		select {
		case js := <-f.execCh:
			if f.chrome != nil {
				f.chrome.Eval(js)
			}
		default:
		}
		for {
			r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1) // PM_REMOVE
			if r == 0 {
				break
			}
			if m.Message == WM_QUIT {
				quit = true
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		if !quit {
			time.Sleep(10 * time.Millisecond)
		}
	}
	log.Printf("[live-float] 窗口已退出")
}
