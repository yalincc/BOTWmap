// chain.go — Savage Wind（BOTW PC 重编译版）静态指针链定位
//
// ★ 本文件是整个定位的地基，改动前请先读 docs/spec/LOCATE-SAVAGEWIND.md
//
// 实机事实（2026-11-06 实测，PID 3784，跨传送/进出神庙验证）：
//
//   1) 宿主地址 = 0x7000000000 + guest VA，但是**稀疏映射**：
//      整个 guest 区由 56+ 段独立分配组成（映射起始处只有约 3.2GB），
//      玩家坐标在 guest VA 0x24486FBA8（9.7GB 处）。任何"固定窗口"都会漏，
//      枚举必须从 guestBase 起逐个 VirtualQueryEx。
//
//   2) 玩家对象结构（对象基址 obj，坐标在 obj+0xD8）：
//        obj+0x00   → 指向静态区的指针（类型/vtable）
//        obj+0xD8   → 世界坐标 X, Y, Z（三个连续 float32）
//        obj+0x120  → 自指（intrusive list 头：值 == obj+0x120）
//
//   3) 静态区（映射起始 512MB）里有两处"注册表记录"（间隔 0x18）指向该对象。
//      槽地址会随版本变，所以**不写死**，每次现场找。
//
//   4) 判别漏斗（实测唯一）：
//        静态区出站指针目标 155291 个
//        → 目标 +0xD8 是合法世界坐标      192~201 个
//        → 目标 +0x120 自指                    1 个 ← 玩家
//
// 这套判据里没有任何写死地址，游戏重新编译（堆地址全变、槽地址全变）后依然成立。
// 唯一写死的游戏常识是"坐标是三个连续 float32"。

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const (
	chainGuestBase  = uintptr(0x7000000000)
	chainGuestLimit = uintptr(0x7000000000) + 0x20000000000 // 仅用于枚举上界
	staticScanSpan  = uintptr(512 << 20)                    // 静态区扫描范围（实测 49MB / 50ms）
	posFieldDefault = uintptr(0xD8)                         // 坐标在对象内的默认偏移（可自动学习）
	chainSelfRefOff = uintptr(0x120)                        // 自指字段偏移
	scanChunk       = 8 << 20                               // 分块读取大小
	maxChainScan    = 12 * time.Second                      // 全窗口兜底硬超时
)

// chainLock 一次锁定的结果。槽位以「相对 guestBase 的偏移」保存，便于落盘。
type chainLock struct {
	SlotOff  uintptr `json:"slotOff"`  // 静态槽 guest VA 偏移（主）
	Slot2Off uintptr `json:"slot2Off"` // 第二槽（同一对象，间隔 0x18）
	Field    uintptr `json:"field"`    // 对象内坐标偏移
}

func (c chainLock) absSlot() uintptr  { return chainGuestBase + c.SlotOff }
func (c chainLock) absSlot2() uintptr { return chainGuestBase + c.Slot2Off }

// chainCache 落盘的定位缓存。指纹一致时可直接秒锁，不必重扫。
type chainCache struct {
	Fingerprint string  `json:"fingerprint"`
	SlotOff     uintptr `json:"slotOff"`
	Slot2Off    uintptr `json:"slot2Off"`
	Field       uintptr `json:"field"`
	ObjHint     uintptr `json:"objHint"`
	SavedAt     string  `json:"savedAt"`
	Note        string  `json:"note"`
}

func chainCachePath() string { return filepath.Join(workDir(), "xnavi-pc-chain.json") }

var chainCacheMu sync.Mutex

func loadChainCache() *chainCache {
	chainCacheMu.Lock()
	defer chainCacheMu.Unlock()
	buf, err := os.ReadFile(chainCachePath())
	if err != nil {
		return nil
	}
	var c chainCache
	if json.Unmarshal(buf, &c) != nil {
		return nil
	}
	return &c
}

func saveChainCache(c *chainCache) {
	chainCacheMu.Lock()
	defer chainCacheMu.Unlock()
	buf, _ := json.MarshalIndent(c, "", " ")
	_ = os.WriteFile(chainCachePath(), buf, 0644)
}

// gameFingerprint 游戏构建指纹：exe 与 swgame.dll 的大小+mtime，外加 dll.id。
// 指纹变化 = 游戏更新过 → 缓存槽位作废，必须重找。
func gameFingerprint(exePath string) string {
	dir := filepath.Dir(exePath)
	part := func(p string) string {
		fi, err := os.Stat(p)
		if err != nil {
			return "0-0"
		}
		return fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().Unix())
	}
	id := ""
	if b, err := os.ReadFile(filepath.Join(dir, "extracted", "swgame.dll.id")); err == nil {
		id = strings.TrimSpace(string(b))
	}
	return fmt.Sprintf("%s|%s|%s", part(exePath), part(filepath.Join(dir, "extracted", "swgame.dll")), id)
}

// ---- 小缓冲读取（漏斗要读十几万次，避免每次分配） ----
var tinyBuf [64]byte

func (l *locator) readSmall(addr uintptr, n uintptr) ([]byte, bool) {
	if l.h == 0 || n > uintptr(len(tinyBuf)) {
		return nil, false
	}
	var got uintptr
	r, _, _ := pReadProcessMemory.Call(
		uintptr(l.h), addr,
		uintptr(unsafe.Pointer(&tinyBuf[0])), n,
		uintptr(unsafe.Pointer(&got)),
	)
	if r == 0 || got != n {
		return nil, false
	}
	return tinyBuf[:n], true
}

func (l *locator) ptrAt(addr uintptr) (uintptr, bool) {
	b, ok := l.readSmall(addr, 8)
	if !ok {
		return 0, false
	}
	return uintptr(binary.LittleEndian.Uint64(b)), true
}

// tripleAt 读 12 字节并校验为合法世界坐标。
func (l *locator) tripleAt(addr uintptr) ([3]float32, bool) {
	b, ok := l.readSmall(addr, 12)
	if !ok {
		return [3]float32{}, false
	}
	x := math.Float32frombits(binary.LittleEndian.Uint32(b[0:4]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(b[4:8]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(b[8:12]))
	if !validTriple(x, y, z) {
		return [3]float32{}, false
	}
	return [3]float32{x, y, z}, true
}

// selfRefOK 判断 obj+0x120 是否自指（intrusive list 头特征）。
func (l *locator) selfRefOK(obj uintptr) bool {
	v, ok := l.ptrAt(obj + chainSelfRefOff)
	return ok && v == obj+chainSelfRefOff
}

// staticPtrAt0 obj+0x00 是否指向静态区（类型/vtable）。
func (l *locator) staticPtrAt0(obj uintptr) bool {
	v, ok := l.ptrAt(obj)
	return ok && v >= chainGuestBase && v < chainGuestBase+staticScanSpan
}

// ---- L1：静态区重找 ----

// resolveByStaticSlots 扫静态区收集出站指针 → 排除无坐标的 → 用自指唯一化。
// 返回锁、对象地址与人类可读的诊断串。
func (l *locator) resolveByStaticSlots() (chainLock, uintptr, string, bool) {
	t0 := time.Now()
	regs := enumRegions(l.h, chainGuestBase, chainGuestBase+staticScanSpan)
	if len(regs) == 0 {
		return chainLock{}, 0, "静态区不可读", false
	}

	// 目标 → 前两个引用槽
	refs := make(map[uintptr][2]uintptr, 1<<18)
	var ptrCount int
	for _, r := range regs {
		if r.protect != pageReadWrite {
			continue
		}
		for off := uintptr(0); off < r.size; off += scanChunk {
			n := uintptr(scanChunk)
			if off+n > r.size {
				n = r.size - off
			}
			buf, ok := readProcess(l.h, r.base+off, int(n))
			if !ok {
				continue
			}
			for i := 0; i+8 <= len(buf); i += 8 {
				v := uintptr(binary.LittleEndian.Uint64(buf[i:]))
				if v < chainGuestBase || v >= chainGuestLimit {
					continue
				}
				ptrCount++
				at := r.base + off + uintptr(i)
				e := refs[v]
				if e[0] == 0 {
					e[0] = at
				} else if e[1] == 0 {
					e[1] = at
				}
				refs[v] = e
			}
		}
	}
	scanMS := time.Since(t0).Milliseconds()

	// 漏斗 ①：+0xD8 是合法坐标
	var stage1 []uintptr
	for tgt := range refs {
		if _, ok := l.tripleAt(tgt + posFieldDefault); ok {
			stage1 = append(stage1, tgt)
		}
	}
	// 漏斗 ②：+0x120 自指
	var stage2 []uintptr
	for _, tgt := range stage1 {
		if l.selfRefOK(tgt) {
			stage2 = append(stage2, tgt)
		}
	}

	diag := fmt.Sprintf("静态区 %d 指针 / %d 目标 → 坐标合法 %d → 自指 %d（%dms）",
		ptrCount, len(refs), len(stage1), len(stage2), scanMS)

	if len(stage2) == 1 {
		obj := stage2[0]
		lk := chainLock{Field: posFieldDefault}
		if e := refs[obj]; e[0] != 0 {
			lk.SlotOff = e[0] - chainGuestBase
		}
		if e := refs[obj]; e[1] != 0 {
			lk.Slot2Off = e[1] - chainGuestBase
		}
		return lk, obj, diag, true
	}

	// ★ 不在这里"猜"坐标偏移。2026-11-06 踩过：游戏加载未完成时 +0xD8 还是 0，
	// 旧代码会去对象内乱找，把 +0x8 处的指针字节当成坐标（denormal 混过值域校验），
	// 并把 field=0x8 写进缓存 → 之后永久读垃圾。
	// 现在只认 posFieldDefault；认不出就返回失败，由 L2（F10/日志真值锚点）兜底。
	if len(stage2) == 0 {
		diag += " / 无自指对象：游戏可能未加载完成，或注册表结构已变"
	}
	return chainLock{}, 0, diag, false
}

// ---- L2：全窗口兜底（需要真值锚点） ----

// fullScanWithAnchor 用真值坐标扫全部 guest 区，找对象。
// 只在 L1 完全失败时使用（罕见），耗时 1.5~3s，带硬超时。
func (l *locator) fullScanWithAnchor(anchor [3]float32) (chainLock, uintptr, bool) {
	deadline := time.Now().Add(maxChainScan)
	field := posFieldDefault
	if l.lock.Field != 0 {
		field = l.lock.Field
	}
	// float 位模式按有符号整数比较 = 浮点比较顺序（NaN 除外），整数比较比浮点快得多
	lo := int32(math.Float32bits(anchor[0] - 0.05))
	hi := int32(math.Float32bits(anchor[0] + 0.05))

	regs := enumRegions(l.h, chainGuestBase, chainGuestLimit)
	for _, r := range regs {
		if r.protect != pageReadWrite {
			continue
		}
		for off := uintptr(0); off < r.size; off += scanChunk {
			if time.Now().After(deadline) {
				return chainLock{}, 0, false
			}
			n := uintptr(scanChunk)
			if off+n > r.size {
				n = r.size - off
			}
			buf, ok := readProcess(l.h, r.base+off, int(n))
			if !ok {
				continue
			}
			for i := 0; i+12 <= len(buf); i += 4 {
				if int32(binary.LittleEndian.Uint32(buf[i:])) < lo ||
					int32(binary.LittleEndian.Uint32(buf[i:])) > hi {
					continue
				}
				x := math.Float32frombits(binary.LittleEndian.Uint32(buf[i:]))
				y := math.Float32frombits(binary.LittleEndian.Uint32(buf[i+4:]))
				z := math.Float32frombits(binary.LittleEndian.Uint32(buf[i+8:]))
				if math.Abs(float64(x-anchor[0])) > 0.05 ||
					math.Abs(float64(y-anchor[1])) > 0.05 ||
					math.Abs(float64(z-anchor[2])) > 0.05 {
					continue
				}
				addr := r.base + off + uintptr(i)
				// 验证对象身份：自指 或 首字段指向静态区
				if obj := addr - field; l.selfRefOK(obj) || l.staticPtrAt0(obj) {
					lk := chainLock{Field: field}
					if slot, ok := l.findSlotFor(obj); ok {
						lk.SlotOff = slot - chainGuestBase
					}
					return lk, obj, true
				}
			}
		}
	}
	return chainLock{}, 0, false
}

// findSlotFor 在静态区里找指向 obj 的槽（L2 命中后补槽位，供缓存）。
func (l *locator) findSlotFor(obj uintptr) (uintptr, bool) {
	regs := enumRegions(l.h, chainGuestBase, chainGuestBase+staticScanSpan)
	for _, r := range regs {
		if r.protect != pageReadWrite {
			continue
		}
		for off := uintptr(0); off < r.size; off += scanChunk {
			n := uintptr(scanChunk)
			if off+n > r.size {
				n = r.size - off
			}
			buf, ok := readProcess(l.h, r.base+off, int(n))
			if !ok {
				continue
			}
			for i := 0; i+8 <= len(buf); i += 8 {
				if uintptr(binary.LittleEndian.Uint64(buf[i:])) == obj {
					return r.base + off + uintptr(i), true
				}
			}
		}
	}
	return 0, false
}

// ---- 真值锚点：游戏日志 [place] 行 ----

// latestPlaceAnchor 从 savagewind.log 尾部取最近的 [place] 行。
// 格式：[place] swr:1 x=900.10 y=131.34 z=157.02 yaw=-106.8 pitch=-0.0 hour=1.17
// 游戏在每次场景切换（读档/传送/进出神庙）时打印，正好是需要重锚的时刻。
func latestPlaceAnchor(exePath string) ([3]float32, bool) {
	var out [3]float32
	if exePath == "" {
		return out, false
	}
	p := filepath.Join(filepath.Dir(exePath), "savagewind.log")
	fi, err := os.Stat(p)
	if err != nil {
		return out, false
	}
	// 太旧的锚点没意义（玩家早走远了）
	if time.Since(fi.ModTime()) > 60*time.Second {
		return out, false
	}
	f, err := os.Open(p)
	if err != nil {
		return out, false
	}
	defer f.Close()
	const tail = 128 << 10
	start := int64(0)
	if fi.Size() > tail {
		start = fi.Size() - tail
	}
	if _, err := f.Seek(start, 0); err != nil {
		return out, false
	}
	buf := make([]byte, fi.Size()-start)
	n, _ := f.Read(buf)
	lines := strings.Split(string(buf[:n]), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(ln, "[place]") {
			continue
		}
		var x, y, z float64
		if _, err := fmt.Sscanf(ln[7:], " swr:1 x=%f y=%f z=%f", &x, &y, &z); err != nil {
			continue
		}
		return [3]float32{float32(x), float32(y), float32(z)}, true
	}
	return out, false
}
