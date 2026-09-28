// Windows API 封装：进程快照、OpenProcess、VirtualQueryEx、ReadProcessMemory。
// 零第三方依赖，仅用标准库 syscall。两平台（Ryujinx/Cemu）共用。
// findPids 精确匹配 + findPid 前缀匹配（Ryujinx 进程名多变，用前缀）。

package main

import (
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
