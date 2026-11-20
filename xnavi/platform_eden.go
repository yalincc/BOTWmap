// platform_eden.go — Eden 平台驱动与内存扫描原语（移植自 enavi 极简成熟架构）。
//
// 架构遵循 PLAN-XNAVI-PIPELINE-REBUILD：
// - 彻底删除 known 磁盘缓存；
// - 专属 User Heap（Switch 内存 2.65GB）快速扫描与 WorldCoordOK 原语；
// - 强制 Run >= 2 门禁，绝不误锁静态死数据。

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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

// edenFindPid 枚举全部 eden.exe 候选，返回第一个能打开句柄的进程。
// 踩坑固化（2026-11-18 真机）：残留/提权的 eden.exe（僵尸进程、管理员实例）
// 在快照枚举中排在真正跑游戏的进程前面，旧实现"取第一个名字匹配"会永远
// 撞上打不开的进程 → 状态机无限等待。与 findLiveCemu 同思路：打不开就跳过。
func edenFindPid() uint32 {
	self := uint32(os.Getpid())
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return 0
	}
	defer closeHandle(snap)
	var pids []uint32
	var e ProcessEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		if e.ProcessID != self {
			name := strings.ToLower(utf16ToString(e.ExeFile[:]))
			if strings.TrimSuffix(name, ".exe") == "eden" && !strings.Contains(name, "core") {
				pids = append(pids, e.ProcessID)
			}
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	var firstMatch uint32
	for _, pid := range pids {
		if firstMatch == 0 {
			firstMatch = pid
		}
		h, err := openProcess(pid)
		if err == nil && h != 0 {
			closeHandle(h)
			if len(pids) > 1 {
				fmt.Printf("  [platform] Eden 多实例：%d 个候选，跳过打不开的进程，选用 PID %d\n", len(pids), pid)
			}
			return pid
		}
	}
	return firstMatch // 全都打不开：返回首个匹配，让 Attach 报失败（保留旧行为下限）
}

// ---------- Eden User Heap 内存枚举 ----------

const (
	guestHeapLo = uintptr(0x2000000000)
	guestHeapHi = uintptr(0x3000000000)
)

// findEdenDram 定位 Switch DRAM 大块。
// 优先取最大的已提交 PAGE_READWRITE ≥1GB 块——真实 DRAM  backing 恒为 RW；
// 只读 MEM_MAPPED 大块是镜像/别名映射，它与 DRAM 的相对位置在游戏重启
// （尤其 TOTK↔BOTW 切换）后会变化，拿它当锚会算出错误的堆窗口
// （2026-11-18 真机：重启后固定窗口 0 块，持续"未发现活动玩家"）。
// 找不到 RW 大块时退回旧判据（MEM_MAPPED ≥1GB 最大块），保持老版本行为下限。
func findEdenDram(h uintptr) (arenaLow uintptr, size uintptr, ok bool) {
	if h == 0 {
		return 0, 0, false
	}
	const limit = uintptr(0x7FFFFFFFFFFF)
	scan := func(match func(mbi *MemoryBasicInformation) bool) (uintptr, uintptr) {
		var maxBase, maxSize uintptr
		addr := uintptr(0)
		for addr < limit {
			var mbi MemoryBasicInformation
			rr, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
			if rr == 0 {
				break
			}
			b, sz := mbi.BaseAddress, mbi.RegionSize
			if match(&mbi) && sz > maxSize {
				maxBase = b
				maxSize = sz
			}
			nxt := b + sz
			if nxt > addr {
				addr = nxt
			} else {
				addr += 0x1000
			}
		}
		return maxBase, maxSize
	}
	// ① 真实 DRAM：已提交 + PAGE_READWRITE + ≥1GB
	b, sz := scan(func(mbi *MemoryBasicInformation) bool {
		return mbi.State == memCommit && mbi.Protect == 0x04 && mbi.RegionSize >= 1<<30
	})
	if sz == 0 {
		// ② 兜底：旧判据 MEM_MAPPED ≥1GB
		b, sz = scan(func(mbi *MemoryBasicInformation) bool {
			return mbi.State == memCommit && mbi.Type == memMapped && mbi.RegionSize >= 1<<30
		})
	}
	if sz == 0 {
		return 0, 0, false
	}
	return b + sz, sz, true
}

// edenHeapBlocks 枚举 Switch User Heap 的已提交可读写块。
// 首选 DRAM 锚点 + 固定偏移窗口（快）；窗口内无块时（游戏重启后堆映射
// 位置变化，固定偏移失效）退回全进程枚举 MEM_MAPPED 型 RW 块——
// guest 堆恒为映射型，宿主自身内存是 PRIVATE，天然被排除。
func edenHeapBlocks(h uintptr, minMB float64) []MemBlock {
	if h == 0 {
		return nil
	}
	minSize := uintptr(minMB * 1048576.0)
	if minSize == 0 {
		minSize = 64 << 10 // 64KB 去噪下限
	}
	isRW := func(mbi *MemoryBasicInformation) bool {
		p := mbi.Protect & 0xFF
		return (p == 0x04 /*PAGE_READWRITE*/ || p == 0x40 /*PAGE_EXECUTE_READWRITE*/) && (mbi.Protect&pageGuard == 0)
	}
	enum := func(start, end uintptr, mappedOnly bool) []MemBlock {
		var blocks []MemBlock
		addr := start
		for addr < end {
			var mbi MemoryBasicInformation
			rr, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
			if rr == 0 {
				break
			}
			b, sz := mbi.BaseAddress, mbi.RegionSize
			if mbi.State == memCommit && isRW(&mbi) && sz >= minSize && (!mappedOnly || mbi.Type == memMapped) {
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
	if arenaLow, _, ok := findEdenDram(h); ok {
		if blocks := enum(arenaLow+guestHeapLo, arenaLow+guestHeapHi, false); len(blocks) > 0 {
			return blocks
		}
	}
	// 兜底：全进程 MEM_MAPPED RW 块（堆映射不在固定窗口内时）
	return enum(0, uintptr(0x7FFFFFFFFFFF), true)
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

// ShrineCoordOK 神庙本地尺度坐标校验。试炼场是独立小场景，坐标范围小
// （|x|,|z|<300、y<50），玩家可能恰好站在本地原点附近或轴上——大世界
// 校验里的近零/轴上退化剔除会把这些合法位置误杀（2026-11-19 真机：
// 进庙后读空 20s 掉锁）。这里只保留 NaN/Inf 与精确零向量剔除。
func ShrineCoordOK(x, y, z float32) bool {
	fx, fy, fz := float64(x), float64(y), float64(z)
	if math.IsNaN(fx) || math.IsNaN(fy) || math.IsNaN(fz) {
		return false
	}
	if math.IsInf(fx, 0) || math.IsInf(fy, 0) || math.IsInf(fz, 0) {
		return false
	}
	if x == 0 && y == 0 && z == 0 {
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

// GroupEdenHits 按 0.1m 网格分组去重。motion 可选（Eden 动力学快照）：
// 提供时按“运动学证据优先”排序，nil 时保持旧版 Run 优先排序（Ryujinx 兼容）。
func GroupEdenHits(hits []EdenHit, motion map[string]edenMotion) []EdenHit {
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
	// 运动学快照（可选）：Eden 实测动画/参数表能造出 Run=33 的长链，
	// 真玩家 RingBuffer 被 ±300 哨兵污染 Run 常只有 2~7——纯 Run 排序会把
	// 真身埋在噪声组下面（2026-11-18 真机：锁定 (300,-300,-300) 静态组 6 分钟）。
	// 有运动学证据时按“持续平移 > 单次变化 > 结构特征”排序；无证据时保持旧排序。
	motionOf := func(g EdenHit) (edenMotion, bool) {
		if motion == nil {
			return edenMotion{}, false
		}
		m, ok := motion[edenMotionKey(g.P)]
		return m, ok
	}
	sort.SliceStable(out, func(i, j int) bool {
		// ⓪ 运动学证据：持续平移（真身走动）> 单次变化 > 无证据
		mi, oki := motionOf(out[i])
		mj, okj := motionOf(out[j])
		if oki && okj {
			si, sj := mi.sustained(), mj.sustained()
			if si != sj {
				return si // 持续平移者优先
			}
			ci, cj := mi.changed(), mj.changed()
			if ci != cj {
				return ci // 单次变化者次之
			}
		}
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

// ---------- 候选运动学快照（锁定前甄别真身） ----------

// edenMotion 记录一个候选组在锁定前的两次位置采样。
// 真玩家坐标持续更新；静态参数/动画表坐标冻结——这是比 Run 长度更硬的活体判据。
type edenMotion struct {
	first, last [3]float32
	maxD        float32 // 相对首次采样的最大位移
	changes     int     // 发生 >=0.05m 变化的采样次数
}

func (m edenMotion) changed() bool { return m.maxD >= 0.05 }

// sustained 持续平移：>=2 次变化且累计位移 >=0.3m（真身走动特征）。
func (m edenMotion) sustained() bool { return m.changes >= 2 && m.maxD >= 0.3 }

// edenMotionKey 与 GroupEdenHits 相同的 0.1m 网格键。
func edenMotionKey(p [3]float32) string {
	return strconv.Itoa(int(math.Round(float64(p[0])*10))) + "," +
		strconv.Itoa(int(math.Round(float64(p[1])*10))) + "," +
		strconv.Itoa(int(math.Round(float64(p[2])*10)))
}

// edenSampleMotion 对候选组代表地址做 rounds 次采样（间隔 250ms），
// 返回每个 0.1m 网格组的运动学证据。只读候选自身地址（每组 28 字节），开销可忽略。
// 背景：Eden 的动画/参数表能造出 Run=33 的长链，真玩家 RingBuffer 被 ±300
// 哨兵污染 Run 常只有 2~7，纯结构排序会锁到静态组（2026-11-18 真机 6 分钟不验证）。
func edenSampleMotion(h uintptr, groups []EdenHit, rounds int) map[string]edenMotion {
	out := make(map[string]edenMotion, len(groups))
	if h == 0 || len(groups) == 0 || rounds < 2 {
		return out
	}
	addrs := make([]uintptr, len(groups))
	for i, g := range groups {
		addrs[i] = g.Addr
		out[edenMotionKey(g.P)] = edenMotion{first: g.P, last: g.P}
	}
	for r := 0; r < rounds; r++ {
		if r > 0 {
			time.Sleep(250 * time.Millisecond)
		}
		for i, addr := range addrs {
			buf := readMem(h, addr, 12)
			if len(buf) < 12 {
				continue
			}
			p := [3]float32{
				math.Float32frombits(binary.LittleEndian.Uint32(buf[0:])),
				math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
				math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
			}
			if !WorldCoordOK(p[0], p[1], p[2]) {
				continue
			}
			k := edenMotionKey(groups[i].P)
			m := out[k]
			if dist3(p, m.last) >= 0.05 {
				m.changes++
			}
			if dd := dist3(p, m.first); dd > m.maxD {
				m.maxD = dd
			}
			m.last = p
			out[k] = m
		}
	}
	return out
}

// edenSampleMotionAddrs 对候选组的全部副本地址采样（每轮间隔 250ms）。
// 真身是环形缓冲：每帧写入不同槽位、旧帧冻结——只采单一代表地址会漏掉
// 真身（2026-11-19 真机：真身 40+ 个运动槽位，代表地址却是冻结帧）。
// 每轮对每组取“离上次位置最近且 >=0.05m”的读值作为活槽证据；
// 冻结组的所有副本读数恒定，自然记为无运动。
func edenSampleMotionAddrs(h uintptr, groups []EdenHit, addrsByCell map[string][]uintptr, rounds int) map[string]edenMotion {
	return edenSampleMotionAddrsMode(h, groups, addrsByCell, rounds, false)
}

// edenSampleMotionAddrsMode shrine=true 时用 ShrineCoordOK 校验采样读值
// （试炼场本地坐标可能位于原点/轴上，大世界校验会误杀）。
func edenSampleMotionAddrsMode(h uintptr, groups []EdenHit, addrsByCell map[string][]uintptr, rounds int, shrine bool) map[string]edenMotion {
	out := make(map[string]edenMotion, len(groups))
	if h == 0 || len(groups) == 0 || rounds < 2 {
		return out
	}
	type job struct {
		key   string
		addrs []uintptr
	}
	jobs := make([]job, 0, len(groups))
	for _, g := range groups {
		k := edenMotionKey(g.P)
		addrs := addrsByCell[k]
		if len(addrs) == 0 {
			addrs = []uintptr{g.Addr}
		}
		jobs = append(jobs, job{key: k, addrs: addrs})
		out[k] = edenMotion{first: g.P, last: g.P}
	}
	for r := 0; r < rounds; r++ {
		if r > 0 {
			time.Sleep(250 * time.Millisecond)
		}
		for _, j := range jobs {
			m := out[j.key]
			bestStep := float32(-1)
			var bestP [3]float32
			for _, addr := range j.addrs {
				buf := readMem(h, addr, 12)
				if len(buf) < 12 {
					continue
				}
				p := [3]float32{
					math.Float32frombits(binary.LittleEndian.Uint32(buf[0:])),
					math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
					math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
				}
				if shrine {
					if !ShrineCoordOK(p[0], p[1], p[2]) {
						continue
					}
				} else if !WorldCoordOK(p[0], p[1], p[2]) {
					continue
				}
				step := dist3(p, m.last)
				if step < 0.05 || step >= 30.0 {
					continue // 冻结旧帧 / 异常跳变不计入走动证据
				}
				if bestStep < 0 || step < bestStep {
					bestStep, bestP = step, p
				}
			}
			if bestStep >= 0 {
				m.changes++
				if dd := dist3(bestP, m.first); dd > m.maxD {
					m.maxD = dd
				}
				m.last = bestP
				out[j.key] = m
			}
		}
	}
	return out
}
