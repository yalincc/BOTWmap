// winapi.go — 只读 Windows API 封装（进程发现 / 内存读取 / 区域枚举 / 共享句柄读档）。
// 全程只读：不注入、不写游戏内存、不改游戏目录。

package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	th32csSnapProcess       = 0x00000002
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	memCommit               = 0x1000
	pageReadOnly            = 0x02
	pageReadWrite           = 0x04
	maxPathW                = 260
)

var (
	k32                        = syscall.NewLazyDLL("kernel32.dll")
	pCreateToolhelp32Snapshot  = k32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW           = k32.NewProc("Process32FirstW")
	pProcess32NextW            = k32.NewProc("Process32NextW")
	pOpenProcess               = k32.NewProc("OpenProcess")
	pReadProcessMemory         = k32.NewProc("ReadProcessMemory")
	pVirtualQueryEx            = k32.NewProc("VirtualQueryEx")
	pQueryFullProcessImageName = k32.NewProc("QueryFullProcessImageNameW")
)

type processEntry32W struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [maxPathW]uint16
}

// memoryBasicInformation 与 Win32 MEMORY_BASIC_INFORMATION (x64) 同构。
type memoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	Reserved          uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	Pad               uint32
}

type memRegion struct {
	base    uintptr
	size    uintptr
	protect uint32
}

// 进程枚举缓冲区放在包级静态区：定位循环跑在子协程里，栈可能增长/搬迁，
// 若把栈上结构体地址以 uintptr 传给 Win32，API 会写回旧栈导致枚举全空。
// 静态区地址恒定，避免这类只在子协程复现的隐性故障。
// debugProbe 由 --debug 打开，用于排查进程枚举异常（不影响正常运行）。
var debugProbe atomic.Bool

// lastFallbackUnix / fallbackPID：兜底查询结果缓存 2 秒。
// tick 里一拍会调用两次进程发现，必须复用结果，否则第二次会被限流挡掉。
var (
	lastFallbackUnix atomic.Int64
	fallbackPID      atomic.Uint32
)

var (
	procEnumMu   sync.Mutex
	procEnumBuf  processEntry32W
	memQueryMu   sync.Mutex
	memQueryBuf  memoryBasicInformation
)

// findProcessPID 按进程名精确匹配（大小写不敏感），返回第一个命中的 PID。
func findProcessPID(want string) uint32 {
	procEnumMu.Lock()
	defer procEnumMu.Unlock()
	dbg := debugProbe.Load()
	e := &procEnumBuf
	snap, _, snapErr := pCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if dbg {
		fmt.Printf("  [probe] snapshot=0x%X err=%v sizeof=%d\n", snap, snapErr, unsafe.Sizeof(procEnumBuf))
	}
	if snap == 0 || snap == ^uintptr(0) {
		return 0
	}
	defer syscall.CloseHandle(syscall.Handle(snap))

	e.Size = uint32(unsafe.Sizeof(procEnumBuf))
	r, _, firstErr := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(e)))
	if dbg {
		fmt.Printf("  [probe] first=%d err=%v first=%q\n", r, firstErr, syscall.UTF16ToString(e.ExeFile[:]))
	}
	if r == 0 {
		return 0
	}
	n := 0
	for {
		name := syscall.UTF16ToString(e.ExeFile[:])
		n++
		if dbg && n <= 3 {
			fmt.Printf("  [probe] proc[%d]=%q\n", n, name)
		}
		if strings.EqualFold(name, want) {
			if dbg {
				fmt.Printf("  [probe] hit pid=%d after %d\n", e.ProcessID, n)
			}
			return e.ProcessID
		}
		r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(e)))
		if r == 0 {
			break
		}
	}
	if dbg {
		fmt.Printf("  [probe] no hit, enumerated=%d\n", n)
	}
	// 兜底：某些受限运行环境（沙箱 / 作业对象）下 Toolhelp32Snapshot 只返回
	// 本环境可见的少量进程，看不到用户手动启动的游戏。此时改用系统自带查询，
	// 它在该环境下仍能看到完整进程表。正常双击运行时上面的枚举直接命中，
	// 不会走到这里，零额外开销。
	now := time.Now().Unix()
	last := lastFallbackUnix.Load()
	if cached := fallbackPID.Load(); cached != 0 && now-last < 2 {
		return cached
	}
	if now-last >= 2 {
		lastFallbackUnix.Store(now)
		pid := findProcessPIDByQuery(want)
		fallbackPID.Store(pid)
		if dbg {
			fmt.Printf("  [probe] fallback pid=%d\n", pid)
		}
		return pid
	}
	return 0
}

// findProcessPIDByQuery 用系统自带进程查询兜底（隐藏控制台窗口，不闪黑窗）。
func findProcessPIDByQuery(want string) uint32 {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+want, "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if debugProbe.Load() {
		fmt.Printf("  [probe] tasklist err=%v raw=%q\n", err, string(out))
	}
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(line, "\",\"")
		if len(parts) < 2 {
			continue
		}
		name := strings.Trim(parts[0], "\" \r\n")
		if !strings.EqualFold(name, want) {
			continue
		}
		pid, e := strconv.Atoi(strings.Trim(parts[1], "\" \r\n"))
		if e == nil && pid > 0 {
			return uint32(pid)
		}
	}
	return 0
}

func openProcessRO(pid uint32) syscall.Handle {
	h, _, _ := pOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if h == 0 {
		return 0
	}
	return syscall.Handle(h)
}

func readProcess(h syscall.Handle, addr uintptr, n int) ([]byte, bool) {
	if h == 0 || n <= 0 {
		return nil, false
	}
	buf := make([]byte, n)
	var read uintptr
	r, _, _ := pReadProcessMemory.Call(
		uintptr(h), addr,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(n),
		uintptr(unsafe.Pointer(&read)),
	)
	runtime.KeepAlive(buf)
	return buf, r != 0 && int(read) == n
}

func processExePath(h syscall.Handle) string {
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := pQueryFullProcessImageName.Call(
		uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
	)
	if r == 0 || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

// enumRegions 枚举 [lo,hi) 内已提交且可读的内存区域。
func enumRegions(h syscall.Handle, lo, hi uintptr) []memRegion {
	memQueryMu.Lock()
	defer memQueryMu.Unlock()
	var out []memRegion
	mi := &memQueryBuf
	cur := lo
	for cur < hi {
		r, _, _ := pVirtualQueryEx.Call(
			uintptr(h), cur,
			uintptr(unsafe.Pointer(mi)), uintptr(unsafe.Sizeof(memQueryBuf)),
		)
		if r == 0 {
			break
		}
		if mi.State == memCommit && (mi.Protect == pageReadWrite || mi.Protect == pageReadOnly) {
			out = append(out, memRegion{base: mi.BaseAddress, size: mi.RegionSize, protect: mi.Protect})
		}
		if mi.RegionSize == 0 {
			break
		}
		cur = mi.BaseAddress + mi.RegionSize
	}
	return out
}

// readFileShared 以 FILE_SHARE_READ|WRITE|DELETE 打开并读取整个文件。
//
// ★ 必须用这个函数读存档：Savage Wind 与 TotK-Recomp 同源，提交存档时走
// move/rename 覆盖，需要 DELETE 共享权限。Go 的 os.ReadFile 不含 DELETE，
// 会挡住游戏写盘（error 5），导致玩家存档丢失。读完立即关闭句柄。
func readFileShared(path string) ([]byte, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(
		p,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h)

	var out []byte
	buf := make([]byte, 256*1024)
	for {
		var n uint32
		if err := syscall.ReadFile(h, buf, &n, nil); err != nil {
			break
		}
		if n == 0 {
			break
		}
		out = append(out, buf[:n]...)
	}
	if len(out) == 0 {
		return nil, syscall.ERROR_FILE_NOT_FOUND
	}
	return out, nil
}
