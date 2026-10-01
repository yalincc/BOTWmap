package main

// chain_totk.go — TOTK 指针链定位运行时（指针扫描定位方案 S2/S4，2026-10-01）
//
// 三级策略（用户拍板）：
//   L1 固定链列表快路径：known_pointers_totk_ryujinx.json 里积累的链形态快速验证
//   L2 指针扫描建链：可信活镜像（基准校准/自动确认）→ 范围指针扫描 → 环基址链
//   L3 兜底扫描：现有探针流程（watch.go 原路径，L1/L2 都失败才走）
//
// CHAIN 模式每拍只做 2 次 RPM（指针 8B + 环窗口 16KB）：
//   指针 → 环基址 B（游戏自己维护，天然跟随）→ 环窗口差分找"刚被写的槽"= 活镜像。
//   玩家站立 = 无槽变化 = 保持输出（正常，不触发任何扫描）。
//
// 隔离红线：本文件只被 ChainCapable()==true 的游戏（TOTK）调用，BOTW 零接触。

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

const (
	chainRingWindow  = 0x4000          // 环窗口字节数（与 ptrScanRangeBack 对应）
	chainFailLimit   = 3               // 连续失败次数 → 链断裂
	chainSlotTol     = 0.5             // 槽位"变化"判定阈值（米）
	chainContDist    = 150.0           // 输出连续性：新槽位与上帧输出距离上限（超=疑似垃圾切换）
	chainL1TrustSec  = 60              // L1 复用等待移动确认的时限（秒）
	chainFileName    = "known_pointers_totk_ryujinx.json"
)

// totkChainRecord 持久化的链形态（跨会话复用的最小要素）。
type totkChainRecord struct {
	PtrOff  uintptr   `json:"ptr_off_in_block"` // 指针地址在所属块内的偏移
	BlkSize uint64    `json:"block_size"`       // 所属块大小（会话间匹配用）
	OK      int       `json:"ok_count"`
	Fail    int       `json:"fail_count"`
	LastOK  time.Time `json:"last_ok"`
	Note    string    `json:"note,omitempty"`
}

type totkChainFile struct {
	Chains []totkChainRecord `json:"chains"`
}

// totkChainRT 链运行时（会话内状态）。
type totkChainRT struct {
	PtrAddr uintptr // 指针所在的内存地址（每拍读它得环基址）
	Rec     totkChainRecord

	prevWin  []byte     // 上一拍环窗口（差分用）
	hasPrev  bool
	out      [3]float32 // 上帧输出
	hasOut   bool
	failN    int
	moveN    int        // 移动确认计数（L1 信任判定复用）
	savedOK  bool       // 确认后是否已持久化强化（防重复写）
	lastLog  time.Time
}

// ---- L1：链列表快路径 ----

func totkChainFilePath() string {
	dir := "."
	if app := os.Getenv("LOCALAPPDATA"); app != "" {
		// 与 known.go 的存储策略保持一致：优先可写工作目录，这里直接用工作目录
		_ = app
	}
	return dir + "/" + chainFileName
}

func totkChainLoad() []totkChainRecord {
	buf, err := os.ReadFile(totkChainFilePath())
	if err != nil {
		return nil
	}
	var f totkChainFile
	if json.Unmarshal(buf, &f) != nil {
		return nil
	}
	sort.Slice(f.Chains, func(i, j int) bool { return f.Chains[i].OK > f.Chains[j].OK })
	return f.Chains
}

func totkChainSave(rec totkChainRecord) {
	recs := totkChainLoad()
	found := false
	for i := range recs {
		if recs[i].PtrOff == rec.PtrOff && recs[i].BlkSize == rec.BlkSize {
			recs[i].OK += rec.OK
			recs[i].Fail += rec.Fail
			if !rec.LastOK.IsZero() {
				recs[i].LastOK = rec.LastOK
			}
			found = true
			break
		}
	}
	if !found {
		recs = append(recs, rec)
	}
	f := totkChainFile{Chains: recs}
	buf, _ := json.MarshalIndent(f, "", "  ")
	os.WriteFile(totkChainFilePath(), buf, 0644)
}

// totkChainTryKnown L1：遍历持久化链形态，找到一个"解引用有效"的运行时。
// 返回 nil = 无可用链（调用方走 L2/L3）。
func totkChainTryKnown(h uintptr, blocks []MemBlock) *totkChainRT {
	recs := totkChainLoad()
	if len(recs) == 0 {
		return nil
	}
	for _, rec := range recs {
		// 按块大小匹配（Ryujinx guest 块大小跨会话相对稳定）
		var blkBase uintptr
		for _, b := range blocks {
			if uint64(b.Size) == rec.BlkSize {
				blkBase = b.Base
				break
			}
		}
		if blkBase == 0 {
			continue
		}
		rt := &totkChainRT{PtrAddr: blkBase + rec.PtrOff, Rec: rec}
		if _, ok := rt.read(h, blocks); ok {
			fmt.Printf("  [chain] L1 record ok: ptr=0x%X (blk+0x%X, ok=%d) — 等待移动确认\n",
				rt.PtrAddr, rec.PtrOff, rec.OK)
			return rt
		}
		fmt.Printf("  [chain] L1 record stale: blk+0x%X deref invalid\n", rec.PtrOff)
	}
	return nil
}

// ---- L2：从可信活镜像建链 ----

// totkChainBuild 从范围指针扫描结果里选出最佳环基址，构造链运行时。
// 选择标准：包含当前镜像坐标的槽（slotHits>0）优先，其次指针数量多者；
// 指针本身要求解引用稳定（连读 3 次一致）。
func totkChainBuild(h uintptr, mirror uintptr, coords [3]float32) *totkChainRT {
	p := currentPlatform()
	if p == nil {
		return nil
	}
	aggs := pointerScanCollect(mirror, coords)
	if len(aggs) == 0 {
		fmt.Printf("  [chain] build failed: no range pointers for mirror 0x%X\n", mirror)
		return nil
	}
	for _, a := range aggs {
		if a.SlotHits == 0 {
			continue
		}
		// 找一个指向该基址的指针，验证解引用稳定（3 次 300ms 间隔读一致）
		for _, pa := range a.PtrAddrs {
			buf0 := readMem(h, pa, 8)
			if len(buf0) < 8 {
				continue
			}
			first := binary.LittleEndian.Uint64(buf0)
			stable := first != 0
			for i := 0; stable && i < 2; i++ {
				time.Sleep(300 * time.Millisecond)
				buf := readMem(h, pa, 8)
				if len(buf) < 8 || binary.LittleEndian.Uint64(buf) != first {
					stable = false
				}
			}
			if !stable {
				continue
			}
			// 归属块（持久化用）：指针地址 - 块基址 = 跨会话复用偏移
			var blkSize uint64
			var ptrOff uintptr
			for _, b := range p.Blocks(minBlockMB) {
				if pa >= b.Base && pa < b.Base+b.Size {
					blkSize = uint64(b.Size)
					ptrOff = pa - b.Base
					break
				}
			}
			if blkSize == 0 {
				continue
			}
			rt := &totkChainRT{
				PtrAddr: pa,
				Rec: totkChainRecord{
					PtrOff:  ptrOff,
					BlkSize: blkSize,
					Note:    fmt.Sprintf("base=0x%X slotHits=%d", a.Base, a.SlotHits),
				},
			}
			fmt.Printf("  [chain] built: ptr=0x%X base=0x%X slotHits=%d exactOff=%#x (mirrorOff=%#x)\n",
				pa, a.Base, a.SlotHits, a.ExactOff, a.Mirror-a.Base)
			return rt
		}
	}
	fmt.Printf("  [chain] build failed: no stable pointer among %d bases\n", len(aggs))
	return nil
}

// ---- 稳态读 ----

// read 每拍调用：指针 → 环基址 → 环窗口差分 → 活镜像坐标。
// 返回 (坐标, 是否有效)。结构失效累计 failN≥chainFailLimit 后返回永久失败，
// 调用方应退出链模式（本函数内部置 failN=上限防重复判断）。
func (rt *totkChainRT) read(h uintptr, blocks []MemBlock) ([3]float32, bool) {
	var zero [3]float32
	// 1) 指针 → 环基址
	pb := readMem(h, rt.PtrAddr, 8)
	if len(pb) < 8 {
		return rt.fail(zero)
	}
	base := uintptr(binary.LittleEndian.Uint64(pb))
	if !addrInBlocks(base, blocks) {
		return rt.fail(zero)
	}
	// 2) 环窗口
	win := readMem(h, base, chainRingWindow)
	if len(win) < 12 {
		return rt.fail(zero)
	}
	defer func() { rt.prevWin = win; rt.hasPrev = true }()

	// 3) 差分找"刚被写的槽"：与上帧窗口比较，值变化 >0.5m 的候选；
	//    有输出历史时优先选与上帧输出连续（<150m）的候选（过滤共享槽乱跳）
	type cand struct {
		off uintptr
		v   [3]float32
	}
	var cands []cand
	if rt.hasPrev && len(rt.prevWin) == len(win) {
		for i := 0; i+12 <= len(win); i += 4 {
			x := math.Float32frombits(binary.LittleEndian.Uint32(win[i:]))
			y := math.Float32frombits(binary.LittleEndian.Uint32(win[i+4:]))
			z := math.Float32frombits(binary.LittleEndian.Uint32(win[i+8:]))
			px := math.Float32frombits(binary.LittleEndian.Uint32(rt.prevWin[i:]))
			py := math.Float32frombits(binary.LittleEndian.Uint32(rt.prevWin[i+4:]))
			pz := math.Float32frombits(binary.LittleEndian.Uint32(rt.prevWin[i+8:]))
			if totkSlotValid(x, y, z) && totkSlotValid(px, py, pz) &&
				(math.Abs(float64(x-px)) > chainSlotTol ||
					math.Abs(float64(y-py)) > chainSlotTol ||
					math.Abs(float64(z-pz)) > chainSlotTol) {
				cands = append(cands, cand{off: uintptr(i), v: [3]float32{x, y, z}})
			}
		}
	}
	if len(cands) == 0 {
		// 站立/无变化：保持上帧输出（正常，不算失败）
		rt.failN = 0
		return rt.out, rt.hasOut
	}
	pick := cands[0]
	if rt.hasOut {
		bestD := dist3(pick.v, rt.out)
		for _, c := range cands[1:] {
			if d := dist3(c.v, rt.out); d < bestD {
				pick, bestD = c, d
			}
		}
		if bestD > chainContDist {
			// 所有变化候选都与上帧输出不连续：疑似环被复用，按结构失效处理
			return rt.fail(zero)
		}
	}
	rt.failN = 0
	rt.out = pick.v
	rt.hasOut = true
	if time.Since(rt.lastLog) > 30*time.Second {
		rt.lastLog = time.Now()
		fmt.Printf("  [chain] slot off=%#x mem=(%.1f,%.1f,%.1f)\n", pick.off, pick.v[0], pick.v[1], pick.v[2])
	}
	return rt.out, true
}

func (rt *totkChainRT) fail(zero [3]float32) ([3]float32, bool) {
	rt.failN++
	if rt.failN >= chainFailLimit {
		rt.failN = chainFailLimit + 1 // 固定在上限，调用方读 failN>=limit 判定
		return zero, false
	}
	return rt.out, rt.hasOut // 偶发读失败：保持上帧输出
}

// Broken 链是否已断（调用方据此退出链模式）。
func (rt *totkChainRT) Broken() bool { return rt.failN > chainFailLimit }

// Moved 移动确认计数推进（L1 信任判定用）。
func (rt *totkChainRT) Moved(cur [3]float32) bool {
	if rt.hasOut && dist3(cur, rt.out) > 0.5 {
		rt.moveN++
	} else {
		rt.moveN = 0
	}
	return rt.moveN >= moveConfirmN
}

// ---- 工具 ----

// totkSlotValid TOTK 坐标合理性（与 game_totk.DecodeAt 同口径的轻量版）。
func totkSlotValid(x, y, z float32) bool {
	// 展示序 (X, alt, Z)：X/Z ∈ [-5000, 5000]，alt ∈ [-1000, 4000]，全零=未初始化
	if x == 0 && y == 0 && z == 0 {
		return false
	}
	return math.Abs(float64(x)) <= 5000 && math.Abs(float64(z)) <= 5000 && y >= -1000 && y <= 4000
}

func addrInBlocks(addr uintptr, blocks []MemBlock) bool {
	for _, b := range blocks {
		if addr >= b.Base && addr < b.Base+b.Size {
			return true
		}
	}
	return false
}
