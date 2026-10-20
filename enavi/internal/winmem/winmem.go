// Package winmem 提供 Windows 进程的「只读」内存访问。
//
// 设计红线（Enavi 是纯只读大地图伴侣）：
//   - 只申请 PROCESS_QUERY_INFORMATION | PROCESS_VM_READ；
//   - 永不申请 PROCESS_VM_WRITE / PROCESS_VM_OPERATION；
//   - 不注入、不 patch、不建远程线程。
// 也就是说：即使上层代码写错，本包在权限层面也不可能改到游戏内存。
//
// 零第三方依赖，只用标准库 syscall。
package winmem

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procVirtualQueryEx             = kernel32.NewProc("VirtualQueryEx")
	procReadProcessMemory          = kernel32.NewProc("ReadProcessMemory")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procCreateToolhelp32Snapshot   = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = kernel32.NewProc("Process32FirstW")
	procProcess32NextW             = kernel32.NewProc("Process32NextW")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetProcessTimes            = kernel32.NewProc("GetProcessTimes")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
)

const (
	ProcessQueryInformation = 0x0400
	ProcessVMRead           = 0x0010

	TH32CSSnapProcess = 0x00000002

	MemCommit  = 0x1000
	MemReserve = 0x2000
	MemFree    = 0x10000
	MemMapped  = 0x40000
	MemPrivate = 0x20000

	PageReadOnly  = 0x02
	PageReadWrite = 0x04
	PageGuard     = 0x100

	// 64 位 Windows 用户态地址上限。
	addrLimit = uintptr(0x7FFFFFFFFFFF)

	// Switch guest 用户态堆在 guest VA 空间中的位置（Horizon OS 64 位布局）：
	// User Heap = [0x2000000000, 0x3000000000)，宿主侧实测约 2.5~2.6GB。
	guestHeapLo = uintptr(0x2000000000)
	guestHeapHi = uintptr(0x3000000000)
)

// MBI 与 Windows MEMORY_BASIC_INFORMATION 布局一致（x64 = 48 字节）。
type MBI struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
}

// PROCESSENTRY32W 与 Windows 同构。
type PROCESSENTRY32W struct {
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
	if unsafe.Sizeof(MBI{}) != 48 {
		panic(fmt.Sprintf("winmem: MEMORY_BASIC_INFORMATION 尺寸不符: %d", unsafe.Sizeof(MBI{})))
	}
}

// Region 一段已提交内存。
type Region struct {
	Base    uintptr
	Size    uintptr
	State   uint32
	Protect uint32
	Type    uint32
}

// End 返回区间末尾（不含）。
func (r Region) End() uintptr { return r.Base + r.Size }

// Readable 是否可读（排除 PAGE_NOACCESS / PAGE_GUARD）。
func (r Region) Readable() bool {
	if r.State != MemCommit {
		return false
	}
	if r.Protect&PageGuard != 0 {
		return false
	}
	p := r.Protect & 0xFF
	return p == PageReadOnly || p == PageReadWrite || p == 0x08 /*WRITECOPY*/ ||
		p == 0x20 /*EXECUTE_READ*/ || p == 0x40 /*EXECUTE_READWRITE*/ || p == 0x80
}

// RW 是否可读写（候选扫描只看这类，与 xnavi 的口径一致）。
func (r Region) RW() bool {
	if r.State != MemCommit {
		return false
	}
	if r.Protect&PageGuard != 0 {
		return false
	}
	p := r.Protect & 0xFF
	return p == PageReadWrite || p == 0x40
}

// TypeName 可读的类型名。
func (r Region) TypeName() string {
	switch r.Type {
	case MemMapped:
		return "MAPPED"
	case MemPrivate:
		return "PRIVATE"
	case 0x1000000:
		return "IMAGE"
	default:
		return fmt.Sprintf("0x%X", r.Type)
	}
}

// String 便于日志输出。
func (r Region) String() string {
	return fmt.Sprintf("0x%X +%7.1fMB %-8s prot=0x%02X", r.Base, float64(r.Size)/1048576.0, r.TypeName(), r.Protect&0xFF)
}

// Process 只读进程句柄。
type Process struct {
	PID    uint32
	Handle uintptr
}

// Open 以只读权限打开进程（无写权限位）。
func Open(pid uint32) (*Process, error) {
	r, _, e := procOpenProcess.Call(uintptr(ProcessQueryInformation|ProcessVMRead), 0, uintptr(pid))
	if r == 0 {
		return nil, fmt.Errorf("OpenProcess(pid=%d) 失败: %w（请同权限运行；Eden 若是管理员启动，Enavi 也要管理员）", pid, e)
	}
	return &Process{PID: pid, Handle: r}, nil
}

// Close 释放句柄。
func (p *Process) Close() {
	if p != nil && p.Handle != 0 {
		procCloseHandle.Call(p.Handle)
		p.Handle = 0
	}
}

// Alive 句柄与进程是否仍有效。
func (p *Process) Alive() bool {
	if p == nil || p.Handle == 0 {
		return false
	}
	var m MBI
	return virtualQueryEx(p.Handle, 0, &m) != 0
}

// FindPIDs 按进程名精确匹配（可带或不带 .exe，大小写不敏感），返回全部 pid。
// Eden 允许双开，故返回列表，由上层决定用哪个（M2 做多实例选择）。
func FindPIDs(name string) []uint32 {
	want := strings.ToLower(strings.TrimSuffix(name, ".exe"))
	snap, _, _ := procCreateToolhelp32Snapshot.Call(TH32CSSnapProcess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return nil
	}
	defer procCloseHandle.Call(snap)

	var out []uint32
	var e PROCESSENTRY32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		if strings.ToLower(strings.TrimSuffix(utf16ToString(e.ExeFile[:]), ".exe")) == want {
			out = append(out, e.ProcessID)
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return out
}

// StartedAt 进程启动时间（Unix 秒），用于识别"同一次 Eden 会话"。
// 缓存地址只在同一会话内可信（ASLR），重启后必须重扫。
func (p *Process) StartedAt() int64 {
	if p == nil || p.Handle == 0 {
		return 0
	}
	var creation, exit, kernel, user struct {
		Low  uint32
		High uint32
	}
	r, _, _ := procGetProcessTimes.Call(p.Handle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0
	}
	// FILETIME = 100ns since 1601-01-01；转 Unix 秒
	ft := uint64(creation.High)<<32 | uint64(creation.Low)
	const epochDiff = 116444736000000000 // 1601→1970 的 100ns 数
	if ft < epochDiff {
		return 0
	}
	return int64((ft - epochDiff) / 10000000)
}

// ExePath 进程主模块完整路径（诊断用）。
func (p *Process) ExePath() string {
	if p == nil || p.Handle == 0 {
		return ""
	}
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(p.Handle, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 || n == 0 {
		return ""
	}
	return string(utf16.Decode(buf[:n]))
}

func utf16ToString(u []uint16) string {
	n := 0
	for n < len(u) && u[n] != 0 {
		n++
	}
	return string(utf16.Decode(u[:n]))
}

// MainWindowTitle 返回进程主窗口标题（可见窗口）。
// 两个用途：① 判断 Eden 是否真的载入了游戏（未载入时标题只有版本号）；
// ② M2 多实例选择（双开 Eden 时按窗口标题区分）。
func MainWindowTitle(pid uint32) string {
	var title string
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1 // 继续枚举
		}
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		buf := make([]uint16, 512)
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		title = utf16ToString(buf[:n])
		return 0 // 找到即停
	})
	procEnumWindows.Call(cb, 0)
	return title
}

// GameLikelyLoaded 用窗口标题粗判"游戏是否已载入"。
// Eden 空载时标题形如 "Eden | v0.2.1 | Clang 22.1.4"；载入游戏后会带游戏名/标题 ID。
// 这只是提示用的启发式，定位逻辑不依赖它（真正的判据是 User Heap 有没有块）。
func GameLikelyLoaded(pid uint32) (bool, string) {
	t := MainWindowTitle(pid)
	if t == "" {
		return false, t
	}
	low := strings.ToLower(t)
	// 空载特征：只有 "eden | vX.Y.Z | Clang ..."（没有游戏名）
	if strings.Contains(low, "eden") && strings.Contains(low, "clang") &&
		!gameNameInTitle(t) {
		return false, t
	}
	return true, t
}

// gameNameInTitle 窗口标题里是否含游戏名（多语言都要认）。
//
// 踩坑：Eden 的标题是中文「塞尔达传说 旷野之息」时，
// 只匹配 "zelda"/"breath" 会误判成"未载入游戏"，
// 于是每次都打一行吓人的警告（其实游戏好好跑着）。
func gameNameInTitle(t string) bool {
	low := strings.ToLower(t)
	for _, k := range []string{
		"zelda", "breath", "wild", "01007ef0", "botw",
		"塞尔达", "旷野", "旷野之息", // 中文标题
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

func virtualQueryEx(h, addr uintptr, m *MBI) uintptr {
	r, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(m)), unsafe.Sizeof(*m))
	return r
}

// vq 查询内存区，瞬态失败重试 3 次（模拟器重映射瞬间会失败）。
func vq(h, addr uintptr, m *MBI) bool {
	for i := 0; i < 3; i++ {
		if virtualQueryEx(h, addr, m) != 0 {
			return true
		}
	}
	return false
}

// Regions 遍历全部地址空间，fn 返回 false 可提前结束。
// lo/hi 为宿主地址范围（hi=0 表示到 addrLimit）。
func (p *Process) Regions(lo, hi uintptr, fn func(Region) bool) {
	if p == nil || p.Handle == 0 {
		return
	}
	if hi == 0 || hi > addrLimit {
		hi = addrLimit
	}
	addr := lo
	for addr < hi {
		var m MBI
		if !vq(p.Handle, addr, &m) {
			return // 走到未映射的空洞尽头
		}
		base, size := m.BaseAddress, m.RegionSize
		if size == 0 {
			addr += 0x1000
			continue
		}
		if fn(Region{Base: base, Size: size, State: m.State, Protect: m.Protect, Type: m.Type}) {
			return
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
}

// Committed 返回全部已提交可读区域。
func (p *Process) Committed(minSize uintptr) []Region {
	var out []Region
	p.Regions(0, 0, func(r Region) bool {
		if r.State == MemCommit && r.Size >= minSize {
			out = append(out, r)
		}
		return false
	})
	return out
}

// Read 读取内存，失败重试 3 次（1ms 间隔）。读取长度不足按实际返回。
func (p *Process) Read(addr uintptr, size int) ([]byte, error) {
	if p == nil || p.Handle == 0 {
		return nil, fmt.Errorf("句柄已失效")
	}
	if size <= 0 {
		return nil, fmt.Errorf("非法读取长度 %d", size)
	}
	buf := make([]byte, size)
	var got uintptr
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		r, _, e := procReadProcessMemory.Call(p.Handle, addr, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&got)))
		if r != 0 && got > 0 {
			return buf[:got], nil
		}
		last = e
		if attempt < 2 {
			time.Sleep(time.Millisecond)
		}
	}
	return nil, fmt.Errorf("ReadProcessMemory(0x%X, %d) 失败: %v", addr, size, last)
}

// ReadTimeout 在超时时间内读取；超时返回错误而不是无限阻塞。
//
// 为什么需要：ReadProcessMemory 是**同步阻塞**的 —— 目标进程（Eden）如果正忙
// （比如长时间 JIT/加载），内核会在目标进程的页表上等待，这次读就会挂住。
// 定位器是长驻服务，一次挂住就等于整个服务卡死。
//
// 实现：把阻塞读放到后台 goroutine，主流程用 select 等。
// 注意：超时的那次后台读可能仍悬着（内核层面），但它只占一个 goroutine，
// 不影响后续读；实测 Eden 恢复后该读会返回。
func (p *Process) ReadTimeout(addr uintptr, size int, timeout time.Duration) ([]byte, error) {
	type result struct {
		buf []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := p.Read(addr, size)
		ch <- result{b, err}
	}()
	select {
	case r := <-ch:
		return r.buf, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("ReadProcessMemory(0x%X, %d) 超时(%v)", addr, size, timeout)
	}
}

// ReadExact 必须读满，否则报错（定位热路径用）。
func (p *Process) ReadExact(addr uintptr, size int) ([]byte, error) {
	b, err := p.Read(addr, size)
	if err != nil {
		return nil, err
	}
	if len(b) != size {
		return nil, fmt.Errorf("短读: 期望 %d 实得 %d", size, len(b))
	}
	return b, nil
}

// ---- Switch / Eden 内存布局 ----

// Dram 描述宿主侧 Switch DRAM 大块映射。
type Dram struct {
	BlockBase uintptr // 大块在宿主进程内的起始地址
	BlockSize uintptr // 实测为 4GB（6G/8G 配置下可能不同，M0 要测）
	ArenaLow  uintptr // = BlockBase + BlockSize；guestVA → 宿主地址的偏移基
}

// HeapLo/HeapHi 返回 User Heap 对应的宿主地址区间。
func (d Dram) HeapLo() uintptr { return d.ArenaLow + guestHeapLo }
func (d Dram) HeapHi() uintptr { return d.ArenaLow + guestHeapHi }

// GuestToHost guest 虚拟地址 → 宿主地址。
// 依据 xnavi platform_eden.go 的实测结论：User Heap 在宿主侧位于 arenArenaLow + guestVA。
func (d Dram) GuestToHost(gva uintptr) uintptr { return d.ArenaLow + gva }

// FindDram 定位 Switch DRAM 大块。
// 判据：已提交 + MEM_MAPPED + 尺寸 ≥ 1GB；取最大者为 DRAM。
// 同时返回全部候选，便于 M0 台账记录（4G/6G/8G 形态差异是待测项）。
func (p *Process) FindDram() (Dram, []Region, error) {
	var cands []Region
	p.Regions(0, 0, func(r Region) bool {
		if r.State == MemCommit && r.Type == MemMapped && r.Size >= 1<<30 {
			cands = append(cands, r)
		}
		return false
	})
	if len(cands) == 0 {
		return Dram{}, nil, fmt.Errorf("未找到 ≥1GB 的 MEM_MAPPED 大块（Eden 是否已启动游戏？）")
	}
	best := cands[0]
	for _, r := range cands[1:] {
		if r.Size > best.Size {
			best = r
		}
	}
	return Dram{BlockBase: best.Base, BlockSize: best.Size, ArenaLow: best.Base + best.Size}, cands, nil
}
