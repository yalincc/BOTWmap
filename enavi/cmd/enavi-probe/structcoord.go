package main

// cmdStructCoord —— B 路线：扫模块全局变量区，用「数据指纹」认对象
//
// ★ 与 §14 之前的 structcoord 完全不同：那版用了 `+0x560` 之类的预设偏移
// （已被证伪：全 .text 2472 处复用）。本版**不预设任何偏移**。
//
// 方法：
//   1. 扫模块区的**可写段**（prot=0x04 PAGE_READWRITE）= .data/.bss，
//      那里存放全局变量，包括单例指针
//   2. 对每个 8 字节值 v，若 v + arenaLow 落在 UserHeap → v 是一个指向堆对象的全局指针
//   3. 对指针指向的对象 O，读它前 N 字节，在**其中**找「位置历史缓冲」的数据指纹：
//        0x28 步长 + [Vector3f 位置][四元数 w∈(0.9,1.0)][朝向角≤180]
//      且要求**连续 2 个以上**元素命中（这是 poshist 验证过的强判据）
//   4. 命中 → 得到两个东西：
//        · 全局指针在模块内的偏移 G（**跨 Eden 会话不变** = 基址+固定偏移）
//        · 位置缓冲在对象内的偏移 K
//      ⇒ 以后读坐标 = 模块基址 + G → 对象 → +K → 12 字节，**零扫描**
//
// 为什么这样可信：判据是**纯数据**的，不依赖任何结构偏移假设。
// 四元数指纹 + 0x28 步长 + 连续多元素，这个组合在 12GB 内存里几乎唯一
// （poshist.Scan 实测：加指纹后命中从 1457 万降到 7513）。

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	"botwmap/enavi/internal/saveanchor"
	"botwmap/enavi/internal/winmem"
)

// pickLatestSaveAnchor 取最新存档坐标（作交叉验证参照，不交互）。
func pickLatestSaveAnchor(p *winmem.Process) ([3]float32, bool) {
	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) == 0 {
		return [3]float32{}, false
	}
	return anchors[0].Pos, true
}

// sc2ObjScan 对象内扫多远（位置缓冲通常在对象头部附近）
const sc2ObjScan = 0x4000

// sc2NeedElems 至少几个连续带指纹的元素才算命中
const sc2NeedElems = 3

type sc2Hit struct {
	gva     uintptr // 全局指针在模块内的 guest VA（= 基址+固定偏移）
	ptrHost uintptr // 指针所在处宿主地址
	objHost uintptr // 指针指向的对象宿主地址
	elemOff uintptr // 位置缓冲在对象内的偏移
	elems   int     // 连续带指纹的元素数
	pos     [3]float32
	dist    float64 // 与存档坐标的距离（米）；<0 = 无参照
}

func cmdStructCoord(p *winmem.Process) {
	d, _, err := p.FindDram()
	if err != nil {
		printf("[sc] 找不到 DRAM 块：%v\n", err)
		return
	}
	runStructCoord(p, d)
}

func runStructCoord(p *winmem.Process, d winmem.Dram) {
	printf("\n════════ B 路线：模块全局变量 → 数据指纹认对象 ════════\n\n")

	// ---- 存档参照 ----
	var anchor [3]float32
	anchorValid := false
	if a, ok := pickLatestSaveAnchor(p); ok {
		anchor, anchorValid = a, true
		printf("[sc] 存档参照坐标 = (%.2f, %.2f, %.2f)\n", anchor[0], anchor[1], anchor[2])
	} else {
		printf("[sc] ⚠️ 没有存档参照坐标\n")
	}

	// ---- 模块区可写段（.data/.bss = 全局变量）----
	modLo, modHi := d.ArenaLow, d.ArenaLow+0x2000_0000_000
	printf("[sc] 模块区 guest VA 0x%X ~ 0x%X（8GB）\n", modLo, modHi)

	var segs []winmem.Region
	var totalBytes int
	p.Regions(modLo, modHi, func(r winmem.Region) bool {
		if !r.Readable() || r.Size < 4096 {
			return false
		}
		// ★ 只要**可写**段 —— 全局变量/单例指针在 .data/.bss，都是可写的；
		//   只读段是代码和常量表，没有指针。
		if !sc2IsRW(r) {
			return false
		}
		segs = append(segs, r)
		totalBytes += int(r.Size)
		return false
	})
	printf("[sc] 模块区可写段 %d 个，共 %.2f MB\n", len(segs), float64(totalBytes)/1048576.0)
	if len(segs) == 0 {
		printf("[sc] 没有可写段 —— Eden 可能没载入游戏\n")
		return
	}

	// ---- 扫全局指针 ----
	var hits []sc2Hit
	seenObj := make(map[uintptr]bool)

	scannedBytes := 0
	for _, sg := range segs {
		printf("[sc] 段 gva=0x%X 大小 %.2f MB\n", sg.Base-d.ArenaLow, float64(sg.Size)/1048576.0)
		// 分块读（32MB）：Eden 模块区是一整块 MEM_MAPPED，绝大多数页未提交
		const chunk = 32 << 20
		for off := uintptr(0); off < sg.Size; off += chunk {
			n := sg.Size - off
			if n > chunk {
				n = chunk
			}
			buf, err := p.Read(sg.Base+off, int(n))
			if err != nil {
				continue // 未提交页，跳过
			}
			for i := 0; i+8 <= len(buf); i += 8 {
				v := binary.LittleEndian.Uint64(buf[i:])
				if v == 0 {
					continue
				}
				// 指针指向堆？（两种口径都试）
				obj, ok := sc2HeapAddr(d, uintptr(v))
				if !ok || seenObj[obj] {
					continue
				}
				seenObj[obj] = true
				ptrHost := sg.Base + off + uintptr(i)

				// 在该对象内找位置缓冲的数据指纹
				elemOff, elems, pos, ok := sc2FindInObj(p, d, obj)
				if !ok {
					continue
				}
				h := sc2Hit{
					gva: ptrHost - d.ArenaLow, ptrHost: ptrHost,
					objHost: obj, elemOff: elemOff, elems: elems, pos: pos, dist: -1,
				}
				if anchorValid {
					h.dist = math.Hypot(float64(pos[0]-anchor[0]), float64(pos[2]-anchor[2]))
				}
				hits = append(hits, h)
				printf("[sc] ★ 命中：全局指针 gva=0x%X → 对象 0x%X，缓冲在对象内 +0x%X（%d 个连续元素）\n",
					h.gva, obj, elemOff, elems)
				printf("[sc]     坐标 (%.2f, %.2f, %.2f)", pos[0], pos[1], pos[2])
				if anchorValid {
					printf("   离存档 %.1f m", h.dist)
				}
				printf("\n")
			}
			time.Sleep(4 * time.Millisecond) // 限速（实测 20ms 时 4583 段要跑几小时，4ms 仍不会拖死模拟器）
			scannedBytes += int(n)
		}
		printf("[sc]   已扫 %.1f MB，指针候选 %d，命中 %d\n",
			float64(scannedBytes)/1048576.0, len(seenObj), len(hits))
	}

	printf("\n[sc] 扫描完成：命中 %d 个\n", len(hits))
	if len(hits) == 0 {
		printf("[sc] 没有命中。可能：\n")
		printf("[sc]   · 位置缓冲不在任何「被全局指针指向的对象」里（内嵌在栈/临时对象）\n")
		printf("[sc]   · 游戏还没进（玩家坐标未初始化）\n")
		printf("[sc]   · 判据太严（可调 sc2NeedElems）\n")
		return
	}

	// 排序：连续元素多的优先；有存档参照时距离近的优先
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].elems != hits[j].elems {
			return hits[i].elems > hits[j].elems
		}
		if anchorValid && hits[i].dist >= 0 && hits[j].dist >= 0 {
			return hits[i].dist < hits[j].dist
		}
		return false
	})
	printf("\n[sc] ── 排名 ──\n")
	for i, h := range hits {
		if i >= 8 {
			break
		}
		printf("[sc] #%d  gva=0x%X  对象内+0x%X  元素数=%d  坐标(%.1f, %.1f, %.1f)",
			i+1, h.gva, h.elemOff, h.elems, h.pos[0], h.pos[1], h.pos[2])
		if h.dist >= 0 {
			printf("  离存档 %.0f m", h.dist)
		}
		printf("\n")
	}
	printf("\n[sc] ★ 若上面第 1 名可信：记下 gva=0x%X 与对象内偏移 0x%X\n", hits[0].gva, hits[0].elemOff)
	printf("[sc]   重启 Eden 后验证 gva 不变 → 得到「基址+固定偏移」，彻底零扫描。\n")

	// ---- 二阶段：会变验证（唯一不依赖启发式的判据）----
	//
	// 存档锚点会过时，坐标范围/常量排除也挡不住所有巧合。
	// 唯一可靠的判据是「**这个槽随玩家移动而变**」——
	// NPC/物体/常量表的坐标不会变。
	//
	// 做法：把所有命中地址的坐标读一遍 → 等几秒 → 再读一遍 → 找变了的。
	// 代价极低（每个只读 12 字节）。
	sc2MotionVerify(p, hits)
}

// sc2MotionVerify 对所有命中做「会变验证」，找出玩家的那个。
func sc2MotionVerify(p *winmem.Process, hits []sc2Hit) {
	printf("\n[sc] ── 二阶段：会变验证（%d 个候选）──\n", len(hits))
	printf("[sc] ★ 请在游戏里走动几秒…\n")

	type snap struct {
		h  sc2Hit
		v1 [3]float32
	}
	var snaps []snap
	for _, h := range hits {
		addr := h.objHost + h.elemOff
		v, ok := sc2ReadVec3(p, addr)
		if !ok {
			continue
		}
		snaps = append(snaps, snap{h, v})
	}
	printf("[sc] 已读 %d 个坐标，等 4 秒…\n", len(snaps))
	time.Sleep(4 * time.Second)

	var moved []float64
	var bestMoved = -1
	var bestDelta float64
	for i, sn := range snaps {
		addr := sn.h.objHost + sn.h.elemOff
		v2, ok := sc2ReadVec3(p, addr)
		if !ok {
			continue
		}
		dx := float64(v2[0] - sn.v1[0])
		dz := float64(v2[2] - sn.v1[2])
		delta := math.Hypot(dx, dz)
		if delta > bestDelta {
			bestDelta = delta
			bestMoved = i
		}
		if delta > 0.05 {
			moved = append(moved, delta)
		}
	}
	printf("[sc] 有 %d 个坐标发生了变化\n", len(moved))
	if bestMoved < 0 {
		printf("[sc] ⚠️ 没有任何坐标变化 —— 玩家可能完全静止。\n")
		printf("[sc]   （站着不动时连玩家的坐标也不变，这是正常现象）\n")
		return
	}
	b := snaps[bestMoved]
	printf("\n[sc] ★★ 变化最大的候选（极可能就是你）:\n")
	printf("[sc]   全局指针 gva = 0x%X   ← 模块内固定偏移，重启不变\n", b.h.gva)
	printf("[sc]   对象内偏移   = 0x%X\n", b.h.elemOff)
	printf("[sc]   4 秒内移动   = %.2f 米\n", bestDelta)
	printf("[sc]   当前坐标     = (%.2f, %.2f, %.2f)\n", b.h.pos[0], b.h.pos[1], b.h.pos[2])
	printf("\n[sc] ★ 下一步：重启 Eden 后重新扫一次，验证 gva 是否仍是 0x%X。\n", b.h.gva)
	printf("[sc]   若不变 → 得到「模块基址 + 固定偏移 + 成员偏移」，彻底零扫描。\n")
}

// sc2IsRW 是否可写段（PAGE_READWRITE / PAGE_EXECUTE_READWRITE）
func sc2IsRW(r winmem.Region) bool {
	if r.State != 0x1000 { // MEM_COMMIT
		return false
	}
	p := r.Protect & 0xFF
	return p == 0x04 || p == 0x40 || p == 0x08
}

// sc2HeapAddr 指针值 → 堆内宿主地址（两种口径都试）
func sc2HeapAddr(d winmem.Dram, v uintptr) (uintptr, bool) {
	if v >= d.HeapLo() && v < d.HeapHi() {
		return v, true
	}
	if v < 0x4000_0000_000 {
		h := v + d.ArenaLow
		if h >= d.HeapLo() && h < d.HeapHi() {
			return h, true
		}
	}
	return 0, false
}

// sc2HasQuat 位置的 +0x0C 起是否朝向四元数指纹
// （与 poshist 同一套判据：w∈(0.9,1.0)、|x|,|y|<0.5、朝向角≤180）
func sc2HasQuat(b []byte) bool {
	if len(b) < 0x1C {
		return false
	}
	f := func(o int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(b[o:]))
	}
	w, x, y, z := f(0x0C), f(0x10), f(0x14), f(0x18)
	for _, v := range []float64{float64(w), float64(x), float64(y), float64(z)} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if w <= 0.90 || w > 1.0 {
		return false
	}
	if x < -0.5 || x > 0.5 || y < -0.5 || y > 0.5 {
		return false
	}
	if z < -180 || z > 180 {
		return false
	}
	return true
}

// sc2WorldOK 是否像世界坐标
func sc2WorldOK(v [3]float32) bool {
	x, alt, z := v[0], v[1], v[2]
	for _, f := range []float64{float64(x), float64(alt), float64(z)} {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	if math.Abs(float64(x)) > 7000 || math.Abs(float64(z)) > 7000 {
		return false
	}
	if alt < -600 || alt > 5000 {
		return false
	}
	if x == 0 && z == 0 {
		return false // 退化坐标
	}
	// 整数三元组是参数表特征
	if math.Abs(float64(x)-math.Round(float64(x))) < 0.001 &&
		math.Abs(float64(z)-math.Round(float64(z))) < 0.001 {
		return false
	}
	// ★ 排除数学常量：实测大量假阳性的坐标是 (1.57, 0, 3.14) / (3.14, 3.14, 3.14)
	//   即 π/2、π、2π —— 引擎里的角度常量表，不是位置。
	if sc2IsPiLike(x) || sc2IsPiLike(alt) || sc2IsPiLike(z) {
		return false
	}
	return true
}

// sc2IsPiLike 是否是 π 的整数倍（角度常量表的特征）
func sc2IsPiLike(f float32) bool {
	v := float64(f)
	for _, k := range []float64{
		0.7853981633974483, 1.5707963267948966, 3.141592653589793,
		6.283185307179586, 9.42477796076938, 12.566370614359172,
		1.0471975511965976, 2.0943951023931953, 0.5235987755982988,
	} {
		if math.Abs(v-k) < 0.002 || math.Abs(v+k) < 0.002 {
			return true
		}
	}
	return false
}

// sc2FindInObj 在对象内找位置历史缓冲的数据指纹。
// 返回（元素在对象内的偏移, 连续元素数, 该元素的位置）
//
// ★ 两级读取：先只读 2KB 快速筛，命中才读大的。
//   模块区全局指针可能上万个，每个都读 16KB 会把时间拖到几分钟起。
func sc2FindInObj(p *winmem.Process, d winmem.Dram, obj uintptr) (uintptr, int, [3]float32, bool) {
	const quick = 0x800
	buf, err := p.ReadTimeout(obj, quick, 800*time.Millisecond)
	if err != nil || len(buf) < 0x40 {
		return 0, 0, [3]float32{}, false
	}
	if off, n, pos, ok := sc2ScanBuf(buf); ok {
		return off, n, pos, true
	}
	// 快筛没中 → 读大范围再试一次
	buf, err = p.ReadTimeout(obj, sc2ObjScan, 1500*time.Millisecond)
	if err != nil || len(buf) < 0x40 {
		return 0, 0, [3]float32{}, false
	}
	if off, n, pos, ok := sc2ScanBuf(buf); ok {
		return off, n, pos, true
	}
	return 0, 0, [3]float32{}, false
}

// sc2ScanBuf 在一块内存里找位置缓冲指纹
func sc2ScanBuf(buf []byte) (uintptr, int, [3]float32, bool) {
	for off := 0; off+0x28+0x1C <= len(buf); off += 4 {
		if !sc2HasQuat(buf[off:]) {
			continue
		}
		pos := sc2ReadF3(buf[off:])
		if !sc2WorldOK(pos) {
			continue
		}
		// 数连续多少个元素也带指纹 + 坐标合法
		n := 1
		for n < 8 {
			e := off + n*0x28
			if e+0x28+0x1C > len(buf) {
				break
			}
			if !sc2HasQuat(buf[e:]) {
				break
			}
			q := sc2ReadF3(buf[e:])
			if !sc2WorldOK(q) {
				break
			}
			// 相邻帧的水平距离要合理（玩家移动有限）
			if math.Hypot(float64(q[0]-pos[0]), float64(q[2]-pos[2])) > 200 {
				break
			}
			n++
		}
		if n >= sc2NeedElems {
			return uintptr(off), n, pos, true
		}
	}
	return 0, 0, [3]float32{}, false
}

// sc2ReadVec3 带超时读 12 字节
func sc2ReadVec3(p *winmem.Process, a uintptr) ([3]float32, bool) {
	b, err := p.ReadTimeout(a, 12, 800*time.Millisecond)
	if err != nil || len(b) < 12 {
		return [3]float32{}, false
	}
	return sc2ReadF3(b), true
}

func sc2ReadF3(b []byte) [3]float32 {
	return [3]float32{
		math.Float32frombits(binary.LittleEndian.Uint32(b[0:])),
		math.Float32frombits(binary.LittleEndian.Uint32(b[4:])),
		math.Float32frombits(binary.LittleEndian.Uint32(b[8:])),
	}
}

var _ = fmt.Sprintf