// Windows API 封装：进程快照、OpenProcess、VirtualQueryEx、ReadProcessMemory。
// 零第三方依赖，仅用标准库 syscall。两平台（Ryujinx/Cemu）共用。
// findPids 精确匹配 + findPid 前缀匹配（Ryujinx 进程名多变，用前缀）。

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procVirtualQueryEx           = kernel32.NewProc("VirtualQueryEx")
	procReadProcessMemory        = kernel32.NewProc("ReadProcessMemory")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procSetConsoleTitleW         = kernel32.NewProc("SetConsoleTitleW")
)

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	th32csSnapprocess       = 0x00000002
	memCommit               = 0x1000
	memMapped               = 0x40000
	pageGuard               = 0x100
)

// MemoryBasicInformation 与 Windows MEMORY_BASIC_INFORMATION 布局一致（x64 = 48 字节）。
type MemoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionId       uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
}

// ProcessEntry32W 与 Windows PROCESSENTRY32W 布局一致。
type ProcessEntry32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

func init() {
	if unsafe.Sizeof(MemoryBasicInformation{}) != 48 {
		panic("MemoryBasicInformation size mismatch")
	}
}

func closeHandle(h uintptr) {
	if h != 0 {
		procCloseHandle.Call(h)
	}
}

func openProcess(pid uint32) (uintptr, error) {
	r, _, e := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if r == 0 {
		return 0, e
	}
	return r, nil
}

// findPids 按进程名精确匹配（不含 .exe，不区分大小写），返回所有匹配 pid。
// Cemu 重启可能残留多个实例，需枚举后按"有大块者优先"选择（见 platform_cemu.go）。
func findPids(name string) []uint32 {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return nil
	}
	defer closeHandle(snap)

	var out []uint32
	var e ProcessEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		exeName := utf16ToString(e.ExeFile[:])
		if strings.EqualFold(strings.TrimSuffix(exeName, ".exe"), name) {
			out = append(out, e.ProcessID)
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return out
}

// findPid 按进程名前缀匹配（不区分大小写），返回第一个匹配 pid。
func findPid(prefix string) uint32 {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return 0
	}
	defer closeHandle(snap)

	var e ProcessEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		name := utf16ToString(e.ExeFile[:])
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return e.ProcessID
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return 0
}

func utf16ToString(u []uint16) string {
	n := 0
	for n < len(u) && u[n] != 0 {
		n++
	}
	return string(utf16.Decode(u[:n]))
}

func virtualQueryEx(h uintptr, addr uintptr, mbi *MemoryBasicInformation) uintptr {
	r, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(mbi)), unsafe.Sizeof(*mbi))
	return r
}

// hLive 轻量句柄活性检查：进程已退出/句柄失效时 VirtualQueryEx 立即失败。
// 用于 ensureAttach 与平台 Attach 的句柄复用路径，防止死进程句柄导致 0 块卡死。
func hLive(h uintptr) bool {
	if h == 0 {
		return false
	}
	var mbi MemoryBasicInformation
	return virtualQueryEx(h, 0, &mbi) != 0
}

// vqRetry 查询内存区域，遇瞬态失败重试 3 次；仍失败返回 false。
// 内存枚举的容错基础：单次查询失败不再直接放弃整轮遍历
// （Ryujinx 重映射/加载瞬间会有瞬态失败，见 V2.2.0 卡死修复）。
func vqRetry(h uintptr, addr uintptr, mbi *MemoryBasicInformation) bool {
	for i := 0; i < 3; i++ {
		if virtualQueryEx(h, addr, mbi) != 0 {
			return true
		}
	}
	return false
}

// readMem 读取进程内存，成功返回实际读到的字节，失败返回 nil。
func readMem(h uintptr, addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var got uintptr
	r, _, _ := procReadProcessMemory.Call(h, addr, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&got)))
	if r == 0 {
		return nil
	}
	return buf[:got]
}

func setConsoleTitle(title string) {
	p, _ := syscall.UTF16PtrFromString(title)
	procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
}

// ---- 模拟器 exe 版本号（诊断用）----

var (
	versionDll                     = syscall.NewLazyDLL("version.dll")
	procGetFileVersionInfoSizeW    = versionDll.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW        = versionDll.NewProc("GetFileVersionInfoW")
	procVerQueryValueW             = versionDll.NewProc("VerQueryValueW")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

// processExePath 返回 pid 对应进程的完整 exe 路径。
func processExePath(pid uint32) string {
	h, err := openProcessLimited(pid)
	if err != nil || h == 0 {
		return ""
	}
	defer closeHandle(h)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return utf16ToString(buf[:n])
}

func openProcessLimited(pid uint32) (uintptr, error) {
	// PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	r, _, e := procOpenProcess.Call(0x1000, 0, uintptr(pid))
	if r == 0 {
		return 0, e
	}
	return r, nil
}

// fileVersion 返回 exe 的文件版本号（如 "1.2.3.4"），失败返回空。
// 用 VS_FIXEDFILEINFO 固定结构，不依赖字符串表语言码，兼容性最好。
func fileVersion(path string) string {
	if path == "" {
		return ""
	}
	pathW, _ := syscall.UTF16PtrFromString(path)
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(pathW)), 0)
	if size == 0 {
		return ""
	}
	buf := make([]byte, size)
	r, _, _ := procGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(pathW)), 0, size, uintptr(unsafe.Pointer(&buf[0])))
	if r == 0 {
		return ""
	}
	var pp uintptr
	var pu uintptr
	backslash := []uint16{'\\', 0}
	r, _, _ = procVerQueryValueW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&backslash[0])),
		uintptr(unsafe.Pointer(&pp)), uintptr(unsafe.Pointer(&pu)))
	if r == 0 || pp == 0 {
		return ""
	}
	// VS_FIXEDFILEINFO: dwFileVersionMS (offset 8), dwFileVersionLS (offset 12)
	ms := *(*uint32)(unsafe.Pointer(pp + 8))
	ls := *(*uint32)(unsafe.Pointer(pp + 12))
	return fmt.Sprintf("%d.%d.%d.%d", uint16(ms>>16), uint16(ms), uint16(ls>>16), uint16(ls))
}

// emulatorExeVersion 返回当前附加模拟器的版本号字符串（诊断用，失败返回空）。
func emulatorExeVersion() string {
	if procPID == 0 {
		return ""
	}
	return fileVersion(processExePath(procPID))
}
