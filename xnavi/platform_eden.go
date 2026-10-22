// platform_eden.go — Eden 平台驱动与内存扫描原语（移植自 enavi 极简成熟架构）。
//
// 架构遵循 PLAN-XNAVI-PIPELINE-REBUILD：
// - 彻底删除 known 磁盘缓存；
// - 专属 User Heap（Switch 内存 2.65GB）快速扫描与 WorldCoordOK 原语；
// - 强制 Run >= 2 门禁，绝不误锁静态死数据。

package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

// ---------- 进程内动态参考中心（单测与运行时传送锚定） ----------

var (
	edenRefMu      sync.RWMutex
	edenRefCenterV [3]float32
	edenRefCenterS bool
)

func EdenClearRefCenter() {
	edenRefMu.Lock()
	edenRefCenterS = false
	edenRefCenterV = [3]float32{}
	edenRefMu.Unlock()
}

func edenSetRefCenter(pos [3]float32) {
	if !WorldCoordOK(pos[0], pos[1], pos[2]) {
		return
	}
	edenRefMu.Lock()
	edenRefCenterV = pos
	edenRefCenterS = true
	edenRefMu.Unlock()
}

func edenRefCenter() ([3]float32, bool) {
	edenRefMu.RLock()
	defer edenRefMu.RUnlock()
	return edenRefCenterV, edenRefCenterS
}

func edenSetRefCenterIfEmpty(pos [3]float32) bool {
	if !WorldCoordOK(pos[0], pos[1], pos[2]) {
		return false
	}
	edenRefMu.Lock()
	defer edenRefMu.Unlock()
	if !edenRefCenterS {
		edenRefCenterV = pos
		edenRefCenterS = true
		return true
	}
	return false
}

// ---------- 存档目录与平台驱动 ----------

var edenSaveDirOverride string

func setEdenSaveDirOverride(d string) {
	edenSaveDirOverride = d
}

type edenPlatform struct {
	h   uintptr
	pid uint32
}

func init() {
	registerPlatform(&edenPlatform{})
}

func (p *edenPlatform) Name() string { return "Eden" }

func (p *edenPlatform) Attach() bool {
	pid := edenFindPid()
	if pid == 0 {
		if p.h != 0 {
			closeHandle(p.h)
		}
		p.h, p.pid = 0, 0
		return false
	}
	if pid == p.pid && p.h != 0 {
		if hLive(p.h) {
			return true
		}
		closeHandle(p.h)
		p.h = 0
	}
	if p.h != 0 {
		closeHandle(p.h)
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		p.h, p.pid = 0, 0
		return false
	}
	p.h, p.pid = h, pid
	return true
}

func (p *edenPlatform) Handle() uintptr     { return p.h }
func (p *edenPlatform) PID() uint32        { return p.pid }
func (p *edenPlatform) LittleEndian() bool { return true }

// Blocks 返回 User Heap 内的 RW 块（2.65GB 范围）。
func (p *edenPlatform) Blocks(minMB float64) []MemBlock {
	return edenHeapBlocks(p.h, minMB)
}

// LargestBlock 返回最大的 DRAM 块基址与大小。
func (p *edenPlatform) LargestBlock(minMB float64) (uintptr, uintptr) {
	arenaLow, size, ok := findEdenDram(p.h)
	if !ok {
		return 0, 0
	}
	return arenaLow, size
}

// SaveRoots 返回 Eden 存档根目录（优先级：手动指定 > 进程自省 > AppData > 盘符兜底）。
func (p *edenPlatform) SaveRoots() []string {
	// P0: 手动指定
	if sd := edenSaveDirOverride; sd != "" {
		return []string{sd}
	}
	var roots []string
	add := func(pp string) {
		if pp != "" {
			roots = append(roots, filepath.Clean(pp))
		}
	}
	if env := os.Getenv("BOTW_SAVE_DIR"); env != "" {
		add(env)
	}

	// P1: 进程自省（提取正在运行的 eden.exe 所在目录 edenDir）
	var edenExe string
	if p.h != 0 {
		edenExe = processExePath(p.h)
	} else if p.pid != 0 {
		edenExe = processExePathByPID(p.pid)
	}
	if edenExe != "" {
		dir := filepath.Dir(edenExe)
		add(filepath.Join(dir, "user", "nand", "user", "save"))
		add(filepath.Join(dir, "..", "user", "nand", "user", "save"))
		add(filepath.Join(dir, "user", "save"))
		add(filepath.Join(dir, "..", "..", "user", "nand", "user", "save"))
	}

	// P2: 检查 %APPDATA%/Eden/user/nand/user/save 及 %APPDATA%/yuzu/nand/user/save
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		add(filepath.Join(appdata, "Eden", "user", "nand", "user", "save"))
		add(filepath.Join(appdata, "eden", "user", "nand", "user", "save"))
		add(filepath.Join(appdata, "yuzu", "nand", "user", "save"))
		add(filepath.Join(appdata, "Yuzu", "nand", "user", "save"))
		add(filepath.Join(appdata, "yuzu", "user", "nand", "user", "save"))
	}

	// P3: 盘符扫描兜底
	for _, drive := range []string{"C:", "D:", "E:", "F:", "G:", "H:", "I:", "J:", "K:", "L:"} {
		d := drive + `\`
		add(filepath.Join(d, "YUZU", "eden", "user", "nand", "user", "save"))
		add(filepath.Join(d, "yuzu", "eden", "user", "nand", "user", "save"))
		add(filepath.Join(d, "Eden", "user", "nand", "user", "save"))
		add(filepath.Join(d, "eden", "user", "nand", "user", "save"))
		add(filepath.Join(d, "YUZU", "user", "nand", "user", "save"))
		add(filepath.Join(d, "yuzu", "nand", "user", "save"))
	}
	return roots
}

func edenFindPid() uint32 {
	self := uint32(os.Getpid())
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return 0
	}
	defer closeHandle(snap)
	var e ProcessEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		if e.ProcessID != self {
			name := strings.ToLower(utf16ToString(e.ExeFile[:]))
			if strings.TrimSuffix(name, ".exe") == "eden" && !strings.Contains(name, "core") {
				return e.ProcessID
			}
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return 0
}

// ---------- Eden User Heap 内存枚举 ----------

const (
	guestHeapLo = uintptr(0x2000000000)
	guestHeapHi = uintptr(0x3000000000)
)

// findEdenDram 定位 Switch DRAM 大块（MEM_MAPPED ≥ 1GB）。
func findEdenDram(h uintptr) (arenaLow uintptr, size uintptr, ok bool) {
	if h == 0 {
		return 0, 0, false
	}
	var maxBase, maxSize uintptr
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		rr, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
		if rr == 0 {
			break
		}
		b, sz := mbi.BaseAddress, mbi.RegionSize
		if mbi.State == memCommit && mbi.Type == memMapped && sz >= 1<<30 {
			if sz > maxSize {
				maxBase = b
				maxSize = sz
			}
		}
		nxt := b + sz
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	if maxSize == 0 {
		return 0, 0, false
	}
	return maxBase + maxSize, maxSize, true
}

// edenHeapBlocks 枚举 Switch User Heap 宿主区间的已提交可读写块。
func edenHeapBlocks(h uintptr, minMB float64) []MemBlock {
	if h == 0 {
		return nil
	}
	arenaLow, _, ok := findEdenDram(h)
	if !ok {
		return nil
	}
	heapStart := arenaLow + guestHeapLo
	heapEnd := arenaLow + guestHeapHi

	minSize := uintptr(minMB * 1048576.0)
	if minSize == 0 {
		minSize = 64 << 10 // 64KB 去噪下限
	}

	var blocks []MemBlock
	addr := heapStart
	for addr < heapEnd {
		var mbi MemoryBasicInformation
		rr, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
		if rr == 0 {
			break
		}
		b, sz := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		isRW := (p == 0x04 /*PAGE_READWRITE*/ || p == 0x40 /*PAGE_EXECUTE_READWRITE*/) && (mbi.Protect&pageGuard == 0)
		if mbi.State == memCommit && isRW && sz >= minSize {
			blocks = append(blocks, MemBlock{Base: b, Size: sz})
		}
		nxt := b + sz
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	return blocks
}

// edenGuestBlocksWithOptions 向后兼容辅助方法。
func edenGuestBlocksWithOptions(h uintptr, minMB float64, userHeapOnly bool, pid uint32) []MemBlock {
	return edenHeapBlocks(h, minMB)
}

// ---------- 坐标校验与扫描原语 (移植自 enavi) ----------

// WorldCoordOK 严格的海拉鲁世界坐标判据（移植自 enavi/internal/scan）。
func WorldCoordOK(x, y, z float32) bool {
	fx, fy, fz := float64(x), float64(y), float64(z)
	if math.IsNaN(fx) || math.IsNaN(fy) || math.IsNaN(fz) {
		return false
	}
	if math.IsInf(fx, 0) || math.IsInf(fy, 0) || math.IsInf(fz, 0) {
		return false
	}
	if math.Abs(fx) > 7000 || math.Abs(fz) > 7000 {
		return false
	}
	if fy < -600 || fy > 5000 {
		return false
	}
	// 近零向量剔除
	if math.Abs(fx) < 0.001 && math.Abs(fz) < 0.001 {
		return false
	}
	// ① X 与 Z 不能同时为 0
	if x == 0 && z == 0 {
		return false
	}
	// ② 排除世界原点附近的退化噪声
	if math.Abs(fx) < 10 && math.Abs(fz) < 10 && math.Abs(fy) < 5 {
		return false
	}
	// ③ 排除轴上退化坐标（参数表特征：X≈0 / alt≈1）
	if (x == 0 || z == 0) && math.Abs(fy-1.0) < 0.01 {
		return false
	}
	// ④ 排除 (0, 0, Z) 或 (X, 0, 0) 等退化标志位向量（如骑乘/盾滑时外观槽被置为 (0, 0, Z)）
	if (x == 0 || math.Abs(fx) < 0.001) && math.Abs(fy) < 0.001 {
		return false
	}
	if (z == 0 || math.Abs(fz) < 0.001) && math.Abs(fy) < 0.001 {
		return false
	}
	return true
}

type EdenHit struct {
	Addr   uintptr
	P      [3]float32
	Copies int
	Rot    bool
	Quat   bool
	Run    int
	Dist   float32
}

const (
	edenRecStride       = 0x40 // 64 字节步长
	edenQuatWindowBytes = 0x0C + 16
)

func IsQuatUnit(buf []byte, off int) bool {
	if off < 0 || off+16 > len(buf) {
		return false
	}
	n := 0.0
	for i := 0; i < 4; i++ {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[off+i*4:])))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		n += v * v
	}
	return math.Abs(n-1.0) < 0.02
}

func isValidRec(buf []byte, off int) bool {
	if off < 0 || off+edenQuatWindowBytes > len(buf) {
		return false
	}
	x := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
	if !WorldCoordOK(x, y, z) {
		return false
	}
	return IsQuatUnit(buf, off+0x0C)
}

func RunLength(buf []byte, off int) int {
	if !isValidRec(buf, off) {
		return 0
	}
	n := 1
	for k := 1; k <= 16; k++ {
		if !isValidRec(buf, off+k*edenRecStride) {
			break
		}
		n++
	}
	for k := 1; k <= 16; k++ {
		if off-k*edenRecStride < 0 || !isValidRec(buf, off-k*edenRecStride) {
			break
		}
		n++
	}
	return n
}

func IsAxisMatrix(buf []byte, off int) bool {
	if off < 0 || off+36 > len(buf) {
		return false
	}
	nz, ones := 0, 0
	for i := 0; i < 9; i++ {
		v := math.Abs(float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[off+i*4:]))))
		if v > 1e-6 {
			nz++
			if math.Abs(v-1.0) < 1e-4 {
				ones++
			}
		}
	}
	return nz == 3 && ones == 3
}

func Rot3OK(buf []byte, off int) bool {
	if off < 0 || off+36 > len(buf) {
		return false
	}
	var m [9]float64
	for i := 0; i < 9; i++ {
		f := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+i*4:]))
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return false
		}
		m[i] = float64(f)
	}
	for c := 0; c < 3; c++ {
		n := m[c]*m[c] + m[3+c]*m[3+c] + m[6+c]*m[6+c]
		if math.Abs(n-1.0) > 0.05 {
			return false
		}
	}
	dot := func(a, b int) float64 {
		return m[a]*m[b] + m[3+a]*m[3+b] + m[6+a]*m[6+b]
	}
	if math.Abs(dot(0, 1)) > 0.05 || math.Abs(dot(0, 2)) > 0.05 || math.Abs(dot(1, 2)) > 0.05 {
		return false
	}
	return true
}

func FindEdenTriples(buf []byte, base uintptr, anchor *[3]float32, radius float32) []EdenHit {
	var hits []EdenHit
	for off := 0; off+12+16 <= len(buf); off += 4 {
		x := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
		if !WorldCoordOK(x, y, z) {
			continue
		}
		d := float32(0)
		if anchor != nil {
			d = dist3([3]float32{x, y, z}, *anchor)
			if radius > 0 && d > radius {
				continue
			}
		}
		h := EdenHit{
			Addr: base + uintptr(off),
			P:    [3]float32{x, y, z},
			Dist: d,
		}
		if off+12+36 <= len(buf) && Rot3OK(buf, off+12) && !IsAxisMatrix(buf, off+12) {
			h.Rot = true
		}
		h.Quat = IsQuatUnit(buf, off+0x0C)
		if h.Quat {
			h.Run = RunLength(buf, off)
		}
		hits = append(hits, h)
	}
	return hits
}

func GroupEdenHits(hits []EdenHit) []EdenHit {
	if len(hits) == 0 {
		return nil
	}
	type key struct{ x, y, z int32 }
	idx := make(map[key]int, len(hits))
	out := make([]EdenHit, 0, len(hits))
	for _, h := range hits {
		k := key{
			int32(math.Round(float64(h.P[0]) * 10)),
			int32(math.Round(float64(h.P[1]) * 10)),
			int32(math.Round(float64(h.P[2]) * 10)),
		}
		if i, ok := idx[k]; ok {
			out[i].Copies++
			out[i].Rot = out[i].Rot || h.Rot
			out[i].Quat = out[i].Quat || h.Quat
			if h.Run > out[i].Run {
				out[i].Run = h.Run
			}
			continue
		}
		h.Copies = 1
		idx[k] = len(out)
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// ① 连续 Run 越长越优先（活玩家当前帧是一整条 Run）
		if (out[i].Run > 0) != (out[j].Run > 0) {
			return out[i].Run > 0
		}
		if out[i].Run != out[j].Run {
			return out[i].Run > out[j].Run
		}
		// ② 有四元数指纹优先
		if out[i].Quat != out[j].Quat {
			return out[i].Quat
		}
		// ③ 有 3x3 旋转矩阵优先
		if out[i].Rot != out[j].Rot {
			return out[i].Rot
		}
		// ④ 距锚点近优先
		return out[i].Dist < out[j].Dist
	})
	return out
}
