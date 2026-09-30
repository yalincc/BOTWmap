// Cemu 进程内存 접근层：多实例择优 + guest base 定位 + Link 坐标读取 + 自愈。
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// 已知的 Link 坐标偏移候选（相对 guest base），按优先级排序。
// 1) 0x1055300C —— 社区公开针位（koko-yl/BotWRamWatch），本机实测有效，
//    版本内固定，可零扫描直接命中，这是本方案相对"全空间 motion 扫描"的核心优势。
// 2) 0xC1F8BF4  —— motion 扫描得到的另一份副本，作为 fallback。
var coordOffsets = []uintptr{0x1055300C, 0xC1F8BF4}

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	th32csSnapprocess       = 0x00000002
	memCommit               = 0x1000
	memPrivate              = 0x20000
	pageGuard               = 0x100
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procReadProcessMemory  = kernel32.NewProc("ReadProcessMemory")
	procVirtualQueryEx     = kernel32.NewProc("VirtualQueryEx")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procCreateToolhelpSnap = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW    = kernel32.NewProc("Process32FirstW")
	procProcess32NextW     = kernel32.NewProc("Process32NextW")
)

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

// findAllPid 列出所有匹配进程名的 pid
func findAllPid(name string) []uint32 {
	var out []uint32
	snap, _, _ := procCreateToolhelpSnap.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return out
	}
	defer procCloseHandle.Call(snap)
	var e ProcessEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		n := 0
		for n < 260 && e.ExeFile[n] != 0 {
			n++
		}
		s := string(utf16Decode(e.ExeFile[:n]))
		if strings.EqualFold(s, name+".exe") || strings.EqualFold(s, name) {
			out = append(out, e.ProcessID)
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return out
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		var r rune
		if u[i] < 0xD800 || u[i] > 0xDFFF {
			r = rune(u[i])
		} else if i+1 < len(u) {
			r = 0x10000 + (rune(u[i]-0xD800) << 10) + rune(u[i+1]-0xD800)
			i++
		} else {
			r = 0xFFFD
		}
		out = append(out, r)
	}
	return out
}

func openProc(pid uint32) uintptr {
	h, _, _ := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	return h
}

func readMem(h uintptr, addr uintptr, size int) ([]byte, bool) {
	if h == 0 || addr == 0 {
		return nil, false
	}
	buf := make([]byte, size)
	var got uintptr
	r, _, _ := procReadProcessMemory.Call(h, addr, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&got)))
	if r == 0 || int(got) != size {
		return nil, false
	}
	return buf, true
}

// f32BE 读 PowerPC 大端 float
func f32BE(h uintptr, addr uintptr) (float64, bool) {
	b, ok := readMem(h, addr, 4)
	if !ok {
		return 0, false
	}
	return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), true
}

// MemoryBasicInformation 与 Windows x64 结构一致（48 字节）
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

func init() {
	if unsafe.Sizeof(MemoryBasicInformation{}) != 48 {
		panic("MBI size mismatch")
	}
}

// rwBlocks 枚举进程内所有 >= minMB 的可读写私有提交块，返回 AllocationBase 去重列表（按块大小降序）
func rwBlocks(h uintptr, minMB int) []struct {
	Base uintptr
	Size uintptr
} {
	type pair struct {
		Base uintptr
		Size uintptr
	}
	var raw []pair
	if minMB <= 0 {
		minMB = 256
	}
	minSize := uintptr(minMB) * 1024 * 1024
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		rr, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
		if rr == 0 {
			break
		}
		b, sz := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit && mbi.Type == memPrivate &&
			(mbi.Protect&pageGuard) == 0 && (p == 0x02 || p == 0x04) && sz >= minSize {
			raw = append(raw, pair{mbi.AllocationBase, sz})
		}
		nxt := b + sz
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].Size > raw[j].Size })
	// 去重（保留最大）
	var out []struct {
		Base uintptr
		Size uintptr
	}
	seen := map[uintptr]bool{}
	for _, r := range raw {
		if r.Base == 0 || seen[r.Base] {
			continue
		}
		seen[r.Base] = true
		out = append(out, struct {
			Base uintptr
			Size uintptr
		}{r.Base, r.Size})
	}
	return out
}

// plausible 坐标合理性自检
func plausible(x, y, z float64) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) {
		return false
	}
	if math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsInf(z, 0) {
		return false
	}
	// 世界 X/Z ∈ ±6000，留余量到 8000；Y ∈ -1000..6000
	if math.Abs(x) > 8000 || math.Abs(z) > 8000 {
		return false
	}
	if y < -1000 || y > 6000 {
		return false
	}
	// 全零 = 未加载
	if x == 0 && z == 0 {
		return false
	}
	return true
}

// tryLocate 在给定进程句柄上定位玩家坐标槽
func tryLocate(h uintptr) (uintptr, uintptr, float64, float64, float64, bool) {
	blocks := rwBlocks(h, 256)
	for _, b := range blocks {
		for _, off := range coordOffsets {
			x, o1 := f32BE(h, b.Base+off)
			y, o2 := f32BE(h, b.Base+off+4)
			z, o3 := f32BE(h, b.Base+off+8)
			if o1 && o2 && o3 && plausible(x, y, z) {
				return b.Base, off, x, y, z, true
			}
		}
	}
	return 0, 0, 0, 0, 0, false
}

// ---------- 会话（Seat） ----------

type Seat struct {
	Pid   uint32
	H     uintptr
	Base  uintptr
	Off   uintptr
	Since time.Time
}

func (s *Seat) Close() {
	if s.H != 0 {
		procCloseHandle.Call(s.H)
	}
	s.H = 0
}

// acquireSeat 在所有 Cemu 进程里挑一个能读到坐标的（多实例场景关键）
func acquireSeat() (*Seat, string) {
	pids := findAllPid("Cemu")
	if len(pids) == 0 {
		return nil, "Cemu not running"
	}
	// 新进程优先（老大手动重开时通常新实例才是有效实例）
	sort.Slice(pids, func(i, j int) bool { return pids[i] > pids[j] })
	tried := []string{}
	for _, pid := range pids {
		h := openProc(pid)
		if h == 0 {
			tried = append(tried, fmt.Sprintf("pid=%d:open-failed", pid))
			continue
		}
		base, off, x, y, z, ok := tryLocate(h)
		if !ok {
			procCloseHandle.Call(h)
			tried = append(tried, fmt.Sprintf("pid=%d:no-slot", pid))
			continue
		}
		s := &Seat{Pid: pid, H: h, Base: base, Off: off, Since: time.Now()}
		return s, fmt.Sprintf("pid=%d base=0x%X off=0x%X pos=(%.1f,%.1f,%.1f)", pid, base, off, x, y, z)
	}
	return nil, "no Cemu instance readable; " + strings.Join(tried, ", ")
}
