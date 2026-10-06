// probe_botw — BOTW×Eden 只读诊断探针（不写内存、不写 known）。
//
// 用途（实测项 1/2/3）：
//   probe_botw.exe scan <x> <y> <z>   — 玩家站桩，输入游戏 HUD 坐标 (X, Y北, Z高)，
//                                       全内存按 6 种轴序排列扫 12 字节三元组（±1.5m），
//                                       报告每种排列的命中情况 → 确定 BOTW 真实内存轴序
//                                       与 elevBias（若 (X, alt, Z) 命中则高度直读；
//                                       若 (X, Z_stored, Y) 命中则第二轴带 bias）。
//   probe_botw.exe save [path]        — 验证 game_data.sav 坐标解析（默认自动找
//                                       Cemu %APPDATA% 根 + Eden 存档根，字节序自适应）。
//   probe_botw.exe ryu                — 试读 Ryujinx 版 BOTW 已知偏移 0xA77FCBBC，
//                                       判断 Eden 上是否可用（大概率失效）。
//
// 用法示例：probe_botw.exe scan 320 -1540 950

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess  = kernel32.NewProc("OpenProcess")
	procVQueryEx     = kernel32.NewProc("VirtualQueryEx")
	procReadPMem     = kernel32.NewProc("ReadProcessMemory")
	procCloseHandle  = kernel32.NewProc("CloseHandle")
	procToolhelpSnap = kernel32.NewProc("CreateToolhelp32Snapshot")
	procP32FirstW    = kernel32.NewProc("Process32FirstW")
	procP32NextW     = kernel32.NewProc("Process32NextW")
)

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	th32csSnapprocess       = 0x00000002
	memCommit               = 0x1000
	pageRW                  = 0x04
	pageExecuteRW           = 0x40
	chunkSize               = 8 << 20
	ryuOffset               = uintptr(0xA77FCBBC) // Ryujinx 版 BOTW 已知偏移（xnavi 验证）
)

type MBI struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionId       uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
}

type ProcEntry32W struct {
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

func findPid(prefix string) uint32 {
	snap, _, _ := procToolhelpSnap.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return 0
	}
	defer procCloseHandle.Call(snap)
	var e ProcEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procP32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		name := utf16Str(e.ExeFile[:])
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return e.ProcessID
		}
		r, _, _ = procP32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return 0
}

func utf16Str(u []uint16) string {
	n := 0
	for n < len(u) && u[n] != 0 {
		n++
	}
	runes := make([]uint16, n)
	copy(runes, u[:n])
	return string(utf16.Decode(runes))
}

func openProcess(pid uint32) (uintptr, error) {
	r, _, e := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if r == 0 {
		return 0, e
	}
	return r, nil
}

func readMem(h uintptr, addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var got uintptr
	r, _, _ := procReadPMem.Call(h, addr, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&got)))
	if r == 0 {
		return nil
	}
	return buf[:got]
}

func floats(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// guestBlocks 枚举 RW 可读区域（照 engine winapi.go：MEM_COMMIT + RW，≥64KB，
// 跳过 0 基址页与 DRAM 幻影区外的杂项）。
func guestBlocks(h uintptr) [][2]uintptr { // [base, size]
	var out [][2]uintptr
	addr := uintptr(0)
	for {
		var m MBI
		r, _, _ := procVQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
		if r == 0 {
			break
		}
		if m.State == memCommit && m.RegionSize > 0 &&
			(m.Protect&0xFF == pageRW || m.Protect&0xFF == pageExecuteRW) &&
			(m.Type == 0x20000 || m.Type == 0x40000) &&
			m.RegionSize >= 64*1024 && m.BaseAddress != 0 {
			out = append(out, [2]uintptr{m.BaseAddress, m.RegionSize})
		}
		if m.RegionSize == 0 {
			break
		}
		next := m.BaseAddress + m.RegionSize
		if next <= addr {
			break
		}
		addr = next
	}
	return out
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

type permHit struct {
	Addr  uintptr
	Raw   [3]float32
	Count int
}

// scanPerm 按给定排列匹配：buf 内 12 字节三元组（4 对齐步进），与目标三元组
// (t0,t1,t2) 三轴 ±tol 匹配。perm 说明内存三轴 (a0,a1,a2) 对应目标的哪个轴。
func scanRegion(h uintptr, base uintptr, size uintptr, target [3]float32, tol float32, perm [3]int, hits *[]permHit, mu *sync.Mutex) {
	for off := uintptr(0); off+12 <= size; off += 4 {
		d := readMem(h, base+off, 12)
		if d == nil || len(d) < 12 {
			continue
		}
		f := floats(d)
		a := [3]float32{f[0], f[1], f[2]}
		ok := true
		for i := 0; i < 3; i++ {
			tv := float32(0)
			switch perm[i] {
			case 0:
				tv = target[0]
			case 1:
				tv = target[1]
			case 2:
				tv = target[2]
			}
			if absf(a[i]-tv) > tol {
				ok = false
				break
			}
		}
		if ok {
			mu.Lock()
			if len(*hits) < 20 {
				*hits = append(*hits, permHit{base + off, a, 0})
			}
			(*hits)[0].Count++
			mu.Unlock()
		}
	}
}

// scanAll 6 排列并行扫描（按区域分片）。
func scanAll(h uintptr, blocks [][2]uintptr, target [3]float32, tol float32) map[string][]permHit {
	perms := map[string][3]int{
		"(X, alt, Z)":   {0, 1, 2}, // 内存 (X, alt, Z) → 目标 (X, Z高, Z南)
		"(X, Z, alt)":   {0, 2, 1}, // 内存 (X, Z, alt) → 目标 (X, Z南, Z高)
		"(alt, X, Z)":   {1, 0, 2},
		"(alt, Z, X)":   {1, 2, 0},
		"(Z, X, alt)":   {2, 0, 1},
		"(Z, alt, X)":   {2, 1, 0},
	}
	out := map[string][]permHit{}
	for name := range perms {
		out[name] = nil
	}
	type job struct {
		base uintptr
		size uintptr
	}
	jobs := make(chan job, len(blocks))
	var mu sync.Mutex
	results := map[string]*[]permHit{}
	for name := range perms {
		h := &[]permHit{}
		results[name] = h
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				for name, perm := range perms {
					scanRegion(h, j.base, j.size, target, tol, perm, results[name], &mu)
				}
			}
		}()
	}
	for _, b := range blocks {
		jobs <- job{b[0], b[1]}
	}
	close(jobs)
	wg.Wait()
	for name := range perms {
		out[name] = *results[name]
	}
	return out
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("usage: probe_botw.exe scan <x> <y> <z> | save [path] | ryu")
		return
	}
	switch args[0] {
	case "save":
		cmdSave(args)
	case "ryu":
		cmdRyu()
	case "scan":
		if len(args) < 4 {
			fmt.Println("usage: probe_botw.exe scan <x> <y> <z>")
			return
		}
		var x, y, z float64
		fmt.Sscanf(args[1], "%f", &x)
		fmt.Sscanf(args[2], "%f", &y)
		fmt.Sscanf(args[3], "%f", &z)
		cmdScan(float32(x), float32(y), float32(z))
	default:
		fmt.Println("unknown subcommand")
	}
}

// cmdScan HUD (X, Y北, Z高) → 内存目标候选：alt=Z高, Z南=-Y北
func cmdScan(x, y, z float32) {
	pid := findPid("eden")
	if pid == 0 {
		fmt.Println("Eden not running - start it and load BOTW first.")
		return
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed (run as admin?).")
		return
	}
	defer procCloseHandle.Call(h)
	fmt.Printf("Eden pid=%d  HUD (%.2f, %.2f, %.2f) -> alt=%.2f Z_south=%.2f\n", pid, x, y, z, z, -y)
	t0 := time.Now()
	blocks := guestBlocks(h)
	total := uint64(0)
	for _, b := range blocks {
		total += uint64(b[1])
	}
	fmt.Printf("guest RW blocks: %d, total %.1f GB\n", len(blocks), float64(total)/1073741824.0)
	target := [3]float32{x, z, -y} // (X, alt, Z南)
	res := scanAll(h, blocks, target, 1.5)
	fmt.Printf("scan done in %s\n\n", time.Since(t0).Round(time.Millisecond))
	for _, name := range []string{"(X, alt, Z)", "(X, Z, alt)", "(alt, X, Z)", "(alt, Z, X)", "(Z, X, alt)", "(Z, alt, X)"} {
		hits := res[name]
		if len(hits) == 0 {
			fmt.Printf("[%s] 0 hits\n", name)
			continue
		}
		fmt.Printf("[%s] %d hits  first %d:\n", name, hits[0].Count, min(6, len(hits)))
		for i, hh := range hits {
			if i >= 6 {
				break
			}
			fmt.Printf("   0x%012X  mem=(%.3f, %.3f, %.3f)\n", hh.Addr, hh.Raw[0], hh.Raw[1], hh.Raw[2])
		}
	}
	fmt.Println("\n命中排列即真实内存轴序；若 (X, alt, Z) 命中且第三轴≈-Y北 → 高度直读无 bias；")
	fmt.Println("若 (X, Z, alt) 命中且第二轴≈Z高+105 → TOTK 同款 elevBias=105。")
}

// cmdSave 验证 game_data.sav 解析（字节序自适应；自动找 Cemu %APPDATA% 根 + Eden 根）。
func cmdSave(args []string) {
	var paths []string
	if len(args) >= 2 {
		paths = append(paths, args[1])
	} else {
		// Cemu Wii U 根（大端）
		cemuRoot := filepath.Join(os.Getenv("APPDATA"), "Cemu", "mlc01", "usr", "save")
		if fi, err := os.Stat(cemuRoot); err == nil && fi.IsDir() {
			paths = append(paths, findSaveFiles(cemuRoot, "")...)
		}
		// Eden 根（小端，标题 ID 过滤）
		for _, cand := range edenSaveRoots() {
			if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
				paths = append(paths, findSaveFiles(cand, "01007EF00011E000")...)
			}
		}
	}
	if len(paths) == 0 {
		fmt.Println("no game_data.sav found (pass path explicitly if it lives elsewhere).")
		return
	}
	found := 0
	for _, p := range paths {
		a := parseGameData(p)
		if a == nil {
			fmt.Printf("skip (unparseable/invalid): %s\n", p)
			continue
		}
		found++
		fmt.Printf("OK %s  mem=(%.1f, %.1f, %.1f)  playtime=%d\n", p, a[0], a[1], a[2], playtimeOf(p))
	}
	if found == 0 {
		fmt.Println("no valid BOTW save anchor found.")
	}
}

func findSaveFiles(root string, titleID string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "game_data.sav") {
			if titleID != "" && !strings.Contains(p, titleID) {
				return nil
			}
			out = append(out, p)
		}
		return nil
	})
	return out
}

func edenSaveRoots() []string {
	var out []string
	if h := findPid("eden"); h != 0 {
		// 进程路径推导（照 engine savefile_totk.go）
		if p := procPath(h); p != "" {
			out = append(out, filepath.Join(filepath.Dir(p), "user", "nand", "user", "save"))
		}
	}
	for _, c := range []string{
		filepath.Join(os.Getenv("APPDATA"), "Eden", "user", "nand", "user", "save"),
		filepath.Join(os.Getenv("APPDATA"), "yuzu", "user", "nand", "user", "save"),
	} {
		out = append(out, c)
	}
	return out
}

func procPath(pid uint32) string {
	// 简化：通过 VirtualQueryEx 不可行，用 QueryFullProcessImageNameW
	procQFPIN := kernel32.NewProc("QueryFullProcessImageNameW")
	var buf [1024]uint16
	var sz uint32 = 1024
	r, _, _ := procQFPIN.Call(uintptr(pid), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
	if r == 0 {
		return ""
	}
	return utf16Str(buf[:sz])
}

var saveVersions = []uint32{0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E}

func detectEndian(data []byte) bool {
	if len(data) >= 4 {
		vLE := binary.LittleEndian.Uint32(data[0:4])
		vBE := binary.BigEndian.Uint32(data[0:4])
		for _, v := range saveVersions {
			if v == vLE {
				return true
			}
			if v == vBE {
				return false
			}
		}
	}
	return true
}

func parseGameData(path string) *[3]float32 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	little := detectEndian(data)
	u32 := binary.BigEndian.Uint32
	if little {
		u32 = binary.LittleEndian.Uint32
	}
	var pos [3]float32
	has := false
	for i := 0x0C; i+24 <= len(data); i += 8 {
		if u32(data[i:]) == 0xA40BA103 { // PLAYER_POSITION
			pos = [3]float32{
				math.Float32frombits(u32(data[i+4:])),
				math.Float32frombits(u32(data[i+12:])),
				math.Float32frombits(u32(data[i+20:])),
			}
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	return &pos
}

func playtimeOf(path string) uint32 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	little := detectEndian(data)
	u32 := binary.BigEndian.Uint32
	if little {
		u32 = binary.LittleEndian.Uint32
	}
	for i := 0x0C; i+8 <= len(data); i += 8 {
		if u32(data[i:]) == 0x73C29681 {
			return u32(data[i+4:])
		}
	}
	return 0
}

// cmdRyu 试读 Ryujinx 版 BOTW 偏移 0xA77FCBBC（大概率失效，结果仅供参考）。
func cmdRyu() {
	pid := findPid("eden")
	if pid == 0 {
		fmt.Println("Eden not running.")
		return
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed.")
		return
	}
	defer procCloseHandle.Call(h)
	var m MBI
	r, _, _ := procVQueryEx.Call(h, ryuOffset, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
	if r == 0 {
		fmt.Printf("0x%X: unreadable region (offset invalid on Eden)\n", ryuOffset)
		return
	}
	d := readMem(h, ryuOffset, 12)
	if d == nil {
		fmt.Printf("0x%X: region readable but read failed\n", ryuOffset)
		return
	}
	f := floats(d)
	fmt.Printf("0x%X: region base=0x%X size=%.1fMB type=0x%X prot=0x%X\n", ryuOffset, m.BaseAddress, float64(m.RegionSize)/1048576.0, m.Type, m.Protect&0xFF)
	if len(f) >= 3 {
		fmt.Printf("  f=(%.3f, %.3f, %.3f)\n", f[0], f[1], f[2])
		abs := absf(f[0]) + absf(f[1]) + absf(f[2])
		if abs > 100 && absf(f[0]) < 7000 && absf(f[1]) < 7000 && absf(f[2]) < 7000 {
			fmt.Println("  -> 看起来像玩家坐标（偏移在 Eden 上可能可用！）")
		} else {
			fmt.Println("  -> 非坐标样数据（偏移在 Eden 上不可用）")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
