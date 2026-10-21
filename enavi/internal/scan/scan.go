// Package scan 提供 Enavi M0/M1 共用的内存扫描与解算原语。
//
// 设计原则：
//   - 只做「读出来的字节怎么解释」，不碰进程句柄；
//   - 判据集中在本文件，上层状态机不重复写阈值（阈值口径见 enavi/PLAN.md §4）。
package scan

import (
	"encoding/binary"
	"math"
	"sort"

	"botwmap/enavi/internal/winmem"
)

// BOTW 世界坐标合法范围（照搬 xnavi game_botw.go 的实测口径）。
const (
	MaxXZ    = 7000.0
	MinAlt   = -600.0
	MaxAlt   = 5000.0
	ShrineXY = 300.0 // |X|,|Z| 都小于此值 = 神庙内本地尺度
)

// Vec3 世界坐标。
type Vec3 struct{ X, Y, Z float32 }

func (v Vec3) Float() [3]float64 {
	return [3]float64{float64(v.X), float64(v.Y), float64(v.Z)}
}

func (v Vec3) Dist2D(o Vec3) float64 {
	dx, dz := float64(v.X-o.X), float64(v.Z-o.Z)
	return math.Hypot(dx, dz)
}

func (v Vec3) Dist3D(o Vec3) float64 {
	dx, dy, dz := float64(v.X-o.X), float64(v.Y-o.Y), float64(v.Z-o.Z)
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// Valid 单点合法性：非 NaN/Inf、在世界范围内、不是近零点。
func (v Vec3) Valid() bool {
	f := []float64{float64(v.X), float64(v.Y), float64(v.Z)}
	for _, x := range f {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	if math.Abs(float64(v.X)) > MaxXZ || math.Abs(float64(v.Z)) > MaxXZ {
		return false
	}
	if float64(v.Y) < MinAlt || float64(v.Y) > MaxAlt {
		return false
	}
	// 近零向量（占位池/未初始化结构）剔除
	if math.Abs(float64(v.X)) < 0.001 && math.Abs(float64(v.Z)) < 0.001 {
		return false
	}
	return true
}

// WorldCoordOK 更严格的「像不像海拉鲁世界坐标」判据。
//
// ★ 为什么要比 Valid() 严（2026-10-21 实测事故）：
//   Valid() 只挡 NaN/越界/近零，但**挡不住参数表里的退化坐标**。
//   实测踩到的坑：Actor 死了之后重扫，锁到了 (107.7, 1.0, 0.0) ——
//   X 和 Z 都在轴上、alt 恰好 = 1，这是引擎参数表的典型形态，不是玩家位置。
//   Valid() 对它返回 true（→ 以为锁上了真玩家，其实离玩家 2485m）。
//
// 三条补充判据（与 poshist 包的 worldCoordOK 同口径，统一到一处）：
//  1. X 与 Z 不能同时为 0（世界坐标不会正好落在坐标轴上）；
//  2. 排除世界原点附近（|X|<10 且 |Z|<10）—— 原点在海外，玩家不可能在那儿；
//  3. 排除「轴上退化坐标」：X 或 Z 恰好为 0 时要求 alt 不为 1
//     （参数表特征是 X≈0 / alt≈1）。
//
// 注意：**不排除整数三元组** —— 实测真实玩家坐标就在 (-717, 618) 这种整数格上，
// 早期版本按「X 与 Z 同时为整数」排除，把真玩家误杀了。
func (v Vec3) WorldCoordOK() bool {
	if !v.Valid() {
		return false
	}
	dx, dy, dz := float64(v.X), float64(v.Y), float64(v.Z)
	// ① X 与 Z 不能同时为 0
	if v.X == 0 && v.Z == 0 {
		return false
	}
	// ② 排除世界原点附近的**退化**坐标。
	//   神庙试炼场的坐标是合法的小数值（|X|<100, |Z|<100, Y<50），
	//   可能落在原点附近 —— 那是**合法**的（神庙在地下，世界原点不在那）。
	//   区分办法：退化坐标是「几乎为零」的噪声（±0.1 量级、alt 也是 0 附近），
	//   真神庙坐标是「像样的小数值」（Y 明显是海拔值，如 12m）。
	//   所以：原点附近 且 alt 也在 0 附近 → 退化垃圾；否则放过。
	if math.Abs(dx) < 10 && math.Abs(dz) < 10 && math.Abs(dy) < 5 {
		return false
	}
	// ③ 轴上退化坐标（参数表特征：X≈0 / alt≈1）
	if (v.X == 0 || v.Z == 0) && math.Abs(dy-1.0) < 0.01 {
		return false
	}
	return true
}

// InShrine 是否落在神庙内本地尺度。
func (v Vec3) InShrine() bool {
	return math.Abs(float64(v.X)) < ShrineXY && math.Abs(float64(v.Z)) < ShrineXY
}

// Hit 一次坐标命中。
type Hit struct {
	Addr   uintptr
	P      Vec3
	Copies int     // 坐标相同（0.1m 内）的地址数，仅作参考，不作身份判据
	Rot    bool    // 邻近是否有 3x3 旋转矩阵（Actor 结构特征）
	Quat   bool    // pos+0x0C 处是否是单位模长四元数（位置历史记录特征）
	Dist   float64 // 与锚点的 3D 距离（锚点为 nil 时为 0）
}

// ---------- ★ 身份判据（2026-10-21 实测重新确立） ----------

// QuatWindowBytes 容纳「pos + 四元数(16B)」所需的最小长度。
const QuatWindowBytes = 0x0C + 16

// IsQuatUnit 判断 buf[off:off+16] 是否是单位模长四元数。
//
// ★ 为什么用「单位模长」而不是「w∈(0.9,1.0]」：
//   w = cos(θ/2)，玩家一转身 w 就离开 (0.9,1.0]（实测 w=-0.311）——
//   那个判据只在面朝特定方向时成立，方向无关的判据是 w²+x²+y²+z² ≈ 1。
func IsQuatUnit(buf []byte, off int) bool {
	if off < 0 || off+16 > len(buf) {
		return false
	}
	n := 0.0
	for i := 0; i < 4; i++ {
		v := float64(f32b(buf, off+i*4))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		n += v * v
	}
	return math.Abs(n-1.0) < 0.02
}

// IsAxisMatrix 判断 buf[off:off+36] 是不是「单位轴矩阵」。
//
// ★ 为什么必须排除它：
//   轴矩阵（9 个元素里正好 3 个非零、且都是 ±1，如 0,1,0 / -1,0,0 / 0,0,1）
//   **完全满足** Rot3OK 的全部判据（三列单位长、两两正交），
//   但它在引擎里到处都是（骨骼基向量、方向轴、欧拉角编码），
//   不是「活跃 3D Actor」的证据。实测它就是误锁的元凶之一。
func IsAxisMatrix(buf []byte, off int) bool {
	if off < 0 || off+36 > len(buf) {
		return false
	}
	nz, ones := 0, 0
	for i := 0; i < 9; i++ {
		v := math.Abs(float64(f32b(buf, off+i*4)))
		if v > 1e-6 {
			nz++
			if math.Abs(v-1.0) < 1e-4 {
				ones++
			}
		}
	}
	return nz == 3 && ones == 3
}

// Options 扫描参数。
type Options struct {
	Anchor *Vec3  // 非空 = 窗口模式，只收锚点半径内的命中
	Radius float32 // 窗口半径（米）
	MinMB  float64 // 只扫 ≥ 该尺寸的区域（默认 0.0625 = 64KB）
}

// FindTriples 在一块连续内存里找「3 个连续 float32 都合法」的位置。
// overlap 由调用方的分块逻辑保证（建议 ≥256 字节），这里不做边界回补。
func FindTriples(buf []byte, base uintptr, opt Options) []Hit {
	var hits []Hit
	if opt.MinMB <= 0 {
		opt.MinMB = 0.0625
	}
	// 需要后方 16 字节（旋转矩阵第一行）做结构校验，故末尾留 16 字节余量。
	for off := 0; off+12+16 <= len(buf); off += 4 {
		x := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
		v := Vec3{x, y, z}
		if !v.Valid() {
			continue
		}
		if opt.Anchor != nil && opt.Radius > 0 && v.Dist3D(*opt.Anchor) > float64(opt.Radius) {
			continue
		}
		h := Hit{Addr: base + uintptr(off), P: v}
		if opt.Anchor != nil {
			h.Dist = v.Dist3D(*opt.Anchor)
		}
		// Rot：3x3 正交旋转矩阵（Actor 特征）—— 但**排除轴矩阵**。
		//   轴矩阵满足全部正交判据却不是 Actor（骨骼基向量、方向轴到处都是）。
		if ro := RotOffset(buf, off); ro >= 0 && !IsAxisMatrix(buf, off+ro) {
			h.Rot = true
		}
		// Quat：pos + 单位模长四元数（位置历史记录特征）。
		//   这是**当前实机最可靠的身份判据**（见 ENAVI-HANDBOOK §6）。
		h.Quat = IsQuatUnit(buf, off+0x0C)
		hits = append(hits, h)
	}
	return hits
}

// Rot3OK 校验 off 起的 3x3 float32 是否为旋转矩阵（三列单位正交）。
// BOTW Actor 的 Transform 布局为 [pos 12B][rot 3x3 = 36B][scale 12B]。
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
	// 列向量单位长度
	for c := 0; c < 3; c++ {
		n := m[c]*m[c] + m[3+c]*m[3+c] + m[6+c]*m[6+c]
		if math.Abs(n-1.0) > 0.05 {
			return false
		}
	}
	// 列向量两两点积 ≈ 0
	dot := func(a, b int) float64 {
		return m[a]*m[b] + m[3+a]*m[3+b] + m[6+a]*m[6+b]
	}
	if math.Abs(dot(0, 1)) > 0.05 || math.Abs(dot(0, 2)) > 0.05 || math.Abs(dot(1, 2)) > 0.05 {
		return false
	}
	return true
}

// Quat 从 off 起读 4 个 float32 当四元数（x, y, z, w）并校验单位模长。
// 注意字段顺序：BOTW 内部为 (x,y,z,w)，但内存布局可能是 (w,x,y,z)，
// M0 要把两种顺序都打出来对比实测（见 PLAN.md M0-R2）。
func Quat(buf []byte, off int) (q [4]float32, ok bool) {
	if off < 0 || off+16 > len(buf) {
		return q, false
	}
	var n float64
	for i := 0; i < 4; i++ {
		f := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+i*4:]))
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) || math.Abs(float64(f)) > 1.5 {
			return q, false
		}
		q[i] = f
		n += float64(f) * float64(f)
	}
	return q, math.Abs(n-1.0) < 0.05
}

// YawFromQuat 四元数 (x,y,z,w) → 航向角（0~360，正北 0°，顺时针）。
// 推导与 BOTW/TOTK 一致，见 docs/notes/HANDOFF-MOUNT-AND-HEADING-FOR-TOTK.md §3.2。
func YawFromQuat(qx, qy, qz, qw float32) float64 {
	vx := 2.0 * (float64(qx)*float64(qz) + float64(qw)*float64(qy))
	vz := 1.0 - 2.0*(float64(qx)*float64(qx)+float64(qy)*float64(qy))
	deg := math.Atan2(vx, -vz) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}
	return deg
}

// Group 按坐标（0.1m 取整）分组，回填 Copies 并排序。
// 排序口径（Enavi 与 xnavi 的关键差异）：
//   1) 有「pos+单位四元数」的记录优先（位置历史记录 = 当前实机最可靠的身份判据）；
//   2) 其次有 3x3 正交旋转矩阵的结构优先（Actor 特征，轴矩阵已排除）；
//   3) 最后按「距锚点近」优先。
//   copies 仅写进结果供人看，不参与排序 —— 2026-10-20 实测已证伪它当身份判据。
func Group(hits []Hit) []Hit {
	if len(hits) == 0 {
		return nil
	}
	type key struct{ x, y, z int32 }
	idx := make(map[key]int, len(hits))
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		k := key{
			int32(math.Round(float64(h.P.X) * 10)),
			int32(math.Round(float64(h.P.Y) * 10)),
			int32(math.Round(float64(h.P.Z) * 10)),
		}
		if i, ok := idx[k]; ok {
			out[i].Copies++
			out[i].Rot = out[i].Rot || h.Rot
			out[i].Quat = out[i].Quat || h.Quat
			continue
		}
		h.Copies = 1
		idx[k] = len(out)
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Quat != out[j].Quat {
			return out[i].Quat
		}
		if out[i].Rot != out[j].Rot {
			return out[i].Rot
		}
		return out[i].Dist < out[j].Dist
	})
	return out
}

// FindASCII 找 ASCII 串（区分大小写），返回全部出现地址。
func FindASCII(buf []byte, base uintptr, needle string) []uintptr {
	if needle == "" || len(buf) < len(needle) {
		return nil
	}
	var out []uintptr
	last := 0
	for {
		i := indexFrom(buf, needle, last)
		if i < 0 {
			return out
		}
		out = append(out, base+uintptr(i))
		last = i + 1
	}
}

func indexFrom(buf []byte, needle string, from int) int {
	if from >= len(buf) {
		return -1
	}
	sub := buf[from:]
	n := len(needle)
	for i := 0; i+n <= len(sub); i++ {
		match := true
		for j := 0; j < n; j++ {
			if sub[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return from + i
		}
	}
	return -1
}

// Chunk 分块遍历内存区域并回调。
// chunkSize 建议 8MB；overlap 256B（保证 12B 坐标 + 36B 旋转矩阵 + 16B 四元数跨块不断裂）。
func Chunk(p *winmem.Process, regions []winmem.Region, chunkSize, overlap int, fn func(base uintptr, buf []byte)) {
	if chunkSize <= 0 {
		chunkSize = 8 << 20
	}
	if overlap < 0 {
		overlap = 0
	}
	for _, r := range regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += uintptr(chunkSize) {
			addr := r.Base + off
			n := chunkSize
			if remain := int(r.Size - off); remain < n {
				n = remain
			}
			if int(off)+n < int(r.Size) {
				n += overlap
			}
			buf, err := p.Read(addr, n)
			if err != nil || len(buf) < 12 {
				continue
			}
			fn(addr, buf)
		}
	}
}

// HeapRegions 过滤出 User Heap 宿主区间的可读写块（Eden 主扫描区）。
// 注意：Eden 内存碎片化，64KB 以下的小块也可能命中，故下限只做去噪。
func HeapRegions(p *winmem.Process, d winmem.Dram, minSize uintptr) []winmem.Region {
	if minSize == 0 {
		minSize = 64 << 10
	}
	lo, hi := d.HeapLo(), d.HeapHi()
	var out []winmem.Region
	p.Regions(lo, hi, func(r winmem.Region) bool {
		if r.RW() && r.Size >= minSize {
			out = append(out, r)
		}
		return false
	})
	return out
}

// BigRWRegions 全宿主可读写大块（兜底扫描用，慎用：19GB 级）。
func BigRWRegions(p *winmem.Process, minSize uintptr) []winmem.Region {
	if minSize == 0 {
		minSize = 64 << 10
	}
	var out []winmem.Region
	p.Regions(0, 0, func(r winmem.Region) bool {
		if r.RW() && r.Size >= minSize {
			out = append(out, r)
		}
		return false
	})
	return out
}

// f32b 读一个小端 float32。
func f32b(buf []byte, off int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
}

// RotOffset 在 pos 之后找正交 3x3 旋转矩阵，返回相对 pos 的偏移（找不到返回 -1）。
//
// 关键：不假设矩阵固定在 +12。xnavi 的 rotOK 口径是「坐标后 128 字节内任意位置有正交 3x3」，
// 本项目实测（2026-10-20）按 [pos][rot@+12][scale@+48] 的严格布局去扫，265 个命中全是
// (0,0,1) 单位变换、世界尺度 0 个 —— 说明真实 Actor 的排布不是那个固定形状。
// 这里改成布局无关：从 +12 起按 4 字节步长最多试 9 个窗口。
func RotOffset(buf []byte, off int) int {
	return RotOffsetIn(buf, off, RotSearchLo, RotSearchHi)
}

// RotOffsetIn 在相对 posOff 的 [lo, hi] 字节区间（4 字节步长）内找正交 3x3。
// lo 可以为负：朝向矩阵有时排在坐标之前（读取时要把坐标前也读进来）。
func RotOffsetIn(buf []byte, posOff, lo, hi int) int {
	for k := lo; k <= hi; k += 4 {
		start := posOff + k
		if start < 0 || start+36 > len(buf) {
			continue
		}
		// 便宜预筛：矩阵首元素必在 [-1,1]
		if v := f32b(buf, start); v < -1.05 || v > 1.05 {
			continue
		}
		if Rot3OK(buf, start) {
			return k
		}
	}
	return -1
}

// RotSearchHi 旋转矩阵相对 pos 的搜索上界（字节）。
//
// 实测（2026-10-21）：矩阵出现在 +0x14 / +0x18 / +0x1C / +0x20 / +0x24 / +0x2C。
// 上界取 44（= 0x2C）留 4 字节余量。
const (
	RotSearchLo = 12
	RotSearchHi = 44
	// RotWindowBytes 完整容纳「最远偏移 + 3x3 矩阵(36字节)」所需的最小缓冲长度
	// = 44 + 36 = 80。读不够这个长度就**不能**断言「没有矩阵」。
	RotWindowBytes = RotSearchHi + 36
)

// TransformLoose pos 三个 float 合法 + 其后邻域存在正交 3x3（布局无关，实测口径）。
// 返回 pos、旋转矩阵相对偏移 rotOff。
//
// ★ 缓冲长度要求 = RotWindowBytes(80)，不是 60 ——
//   矩阵最远在 +0x2C，要读完 3x3 需要 +0x2C+36 = 80 字节。
//   读 60 字节时 +0x2C 处的矩阵根本看不到 → 会把「活着的 Actor」误判成死的。
func TransformLoose(buf []byte, off int) (pos Vec3, rotOff int, ok bool) {
	if off < 0 || off+RotWindowBytes > len(buf) {
		return pos, -1, false
	}
	pos = Vec3{f32b(buf, off), f32b(buf, off+4), f32b(buf, off+8)}
	if !pos.Valid() {
		return pos, -1, false
	}
	k := RotOffset(buf, off)
	if k < 0 {
		return pos, -1, false
	}
	return pos, k, true
}

// Matrix 读 off 起的 3x3 旋转矩阵（行优先存放顺序：m[0..2] 是第一行）。
func Matrix(buf []byte, off int) [9]float32 {
	var m [9]float32
	for i := 0; i < 9; i++ {
		m[i] = f32b(buf, off+i*4)
	}
	return m
}

// YawFromMatrix 从 3x3 旋转矩阵解算航向角，同时给出两种存储顺序的结果。
//   - 行优先（第三行为前向量）：常见于把矩阵按 row-major 存的结构
//   - 列优先（第三列为前向量）：OpenGL/sead 常见
// 哪个对由实机验证决定（朝北走应≈0°、朝东走应≈90°），不做假设。
func YawFromMatrix(m [9]float32) (yawRow, yawCol float64) {
	deg := func(fx, fz float64) float64 {
		d := math.Atan2(fx, -fz) * 180 / math.Pi
		if d < 0 {
			d += 360
		}
		return d
	}
	yawRow = deg(float64(m[2]), float64(m[8]))
	yawCol = deg(float64(m[6]), float64(m[8]))
	return
}

// HistoryCopyAt 判断 addr 处的坐标是否落在「位置历史副本链」上。
// 判据：±32/±64 字节处还有合法世界坐标，且与 self 相距 < 60m（同物体近期轨迹）。
// 实测样本（2026-10-20，BOTW 1.6.0 / Eden）：1126.9,117.0,1800.5 → 1135.5,117.6,1796.9 → 1130.0,117.5,1796.7，
// 三串以 32 字节步长堆在同一个块里，位移差分时会被误当成"玩家在动"。
func HistoryCopyAt(p *winmem.Process, addr uintptr, self Vec3) bool {
	const pre = 80
	buf, err := p.Read(addr-pre, pre+160)
	if err != nil {
		return false
	}
	hits := 0
	for _, d := range []int{-64, -32, 32, 64} {
		o := pre + d
		if o < 0 || o+12 > len(buf) {
			continue
		}
		v := Vec3{
			X: math.Float32frombits(binary.LittleEndian.Uint32(buf[o:])),
			Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[o+4:])),
			Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[o+8:])),
		}
		if v.Valid() && v.Dist3D(self) < 60 {
			hits++
		}
	}
	return hits >= 2
}

// ReadVec3 读地址处的三个 float32 当坐标。
func ReadVec3(p *winmem.Process, addr uintptr) (Vec3, bool) {
	buf, err := p.Read(addr, 12)
	if err != nil || len(buf) < 12 {
		return Vec3{}, false
	}
	v := Vec3{
		X: math.Float32frombits(binary.LittleEndian.Uint32(buf[0:])),
		Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
		Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
	}
	return v, v.Valid()
}
