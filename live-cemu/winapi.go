// Windows API 封装：进程快照、OpenProcess、VirtualQueryEx、ReadProcessMemory。
// 零第三方依赖，仅用标准库 syscall。与 BotwNavi(live-go)/winapi.go 同构。

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

// findPids 按进程名精确匹配（不区分大小写），返回所有匹配的 pid。
// 注意：必须精确匹配——"cemu" 前缀会把 CemuNavi 自己匹配进去，
// 导致 findLiveCemu 在 Cemu 无大块时退化 attach 到自己（定位永远失败）。
// Cemu 重启可能残留多个实例，findPid 只取第一个会 attach 到僵尸进程。
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

// findPid 返回第一个匹配的 pid（保留给简单场景；attach 请用 findLiveCemu）。
func findPid(prefix string) uint32 {
	pids := findPids(prefix)
	if len(pids) == 0 {
		return 0
	}
	return pids[0]
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

// rwBlocks 枚举所有"已提交 + 可读写"的连续区域（相邻同保护区合并），返回 ≥minMB 的块。
// Cemu 的模拟内存（MEM2/TEXT_AREA/MEM1）是宿主进程里的大块提交区（MEM_PRIVATE 或 MEM_MAPPED），
// 与 Ryujinx 只枚举 MEM_MAPPED 不同：这里不限制 Type，靠大小与位置交叉校验。
func rwBlocks(h uintptr, minMB float64) []struct{ Base, Size uintptr } {
	minSize := uintptr(minMB * 1048576.0)
	var out []struct{ Base, Size uintptr }
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, addr, &mbi) == 0 {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit && (p == 0x02 || p == 0x04) && (mbi.Protect&pageGuard) == 0 {
			if n := len(out); n > 0 && base == out[n-1].Base+out[n-1].Size {
				out[n-1].Size += size
			} else {
				out = append(out, struct{ Base, Size uintptr }{base, size})
			}
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	var filtered []struct{ Base, Size uintptr }
	for _, b := range out {
		if b.Size >= minSize {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// largestRWBlock 返回最大的一块已提交可读写区域（known offsets 锚点）。
// Cemu 的模拟内存通常表现为一块 1~4GB 的主提交区，基址每次启动变、偏移稳定；
// 用它做 known_addrs 的锚比依赖 memory_base 猜测更稳（不需要知道精确基址）。
func largestRWBlock(h uintptr) (uintptr, uintptr) {
	best, bestSize := uintptr(0), uintptr(0)
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, addr, &mbi) == 0 {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		if mbi.State == memCommit &&
			((mbi.Protect&0xFF) == 0x02 || (mbi.Protect&0xFF) == 0x04) &&
			(mbi.Protect&pageGuard) == 0 && size > bestSize {
			best, bestSize = base, size
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	return best, bestSize
}

// rwBlockT 带 AllocationBase 的块描述。
// ⚠️ Cemu 2.x 本机实测：最大 RW 块（3616MB）的 BaseAddress 与 AllocationBase 相差 0x2000000，
//    真正的 memory_base / 固定针位锚 = AllocationBase（见 _情报-社区固定针位.md）。
type rwBlockT struct {
	Base  uintptr // 首个已提交区域的 BaseAddress
	Alloc uintptr // 所在分配的 AllocationBase（= 真 memory_base）
	Size  uintptr
}

// rwBlocksAlloc 同 rwBlocks，但每个合并块附带首个区域的 AllocationBase。
func rwBlocksAlloc(h uintptr, minMB float64) []rwBlockT {
	minSize := uintptr(minMB * 1048576.0)
	var out []rwBlockT
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, addr, &mbi) == 0 {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit && (p == 0x02 || p == 0x04) && (mbi.Protect&pageGuard) == 0 {
			if n := len(out); n > 0 && base == out[n-1].Base+out[n-1].Size {
				out[n-1].Size += size
			} else {
				out = append(out, rwBlockT{base, mbi.AllocationBase, size})
			}
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	var filtered []rwBlockT
	for _, b := range out {
		if b.Size >= minSize {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// largestRWAlloc 返回最大 RW 块的 (BaseAddress, AllocationBase, Size)。
func largestRWAlloc(h uintptr) (uintptr, uintptr, uintptr) {
	best, bestA, bestS := uintptr(0), uintptr(0), uintptr(0)
	for _, b := range rwBlocksAlloc(h, 1.0) {
		if b.Size > bestS {
			best, bestA, bestS = b.Base, b.Alloc, b.Size
		}
	}
	return best, bestA, bestS
}

// hasCommittedRW 检查 addr 起始的连续已提交可读区域合计是否 ≥ want 字节。
// addr 可能落在某个大区域内部：从 addr 所在区域起向后累计，不要求区域正好从 addr 开始。
// 允许 Exec 类保护（0x20/0x40/0x80），供校验用。
func hasCommittedRW(h uintptr, addr uintptr, want uintptr) bool {
	total := uintptr(0)
	cur := addr
	for total < want {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, cur, &mbi) == 0 {
			return false
		}
		if mbi.State != memCommit {
			return false
		}
		p := mbi.Protect & 0xFF
		if p != 0x02 && p != 0x04 && p != 0x20 && p != 0x40 && p != 0x80 {
			return false
		}
		total += mbi.RegionSize
		cur += mbi.RegionSize
	}
	return true
}

func setConsoleTitle(title string) {
	p, _ := syscall.UTF16PtrFromString(title)
	procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
}
