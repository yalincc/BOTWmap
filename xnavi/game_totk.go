// game_totk.go — TOTK 游戏适配层（Game 接口实现）。
//
// 与 TOTKmap live-go（TOTKNavi V1.8.7）同口径：
//   - 内存三元组（小端）(X_east, Z_stored, Y_north)，Z_stored = 真实高度 + ELEV_BIAS(105)；
//     展示序 (gx=X, gy=-Y北, gz=alt)——注意 TOTK 第三轴是 +Y 北，与 BOTW 的 -Y 相反，不要取反两次；
//   - 地图换算 mx=-gy, my=gx（watch.go 原口径 state.mx,state.my = -cur[1],cur[0]）；
//   - 内存合法域 ±6000 / gz∈(-1300,3200)（decodePos），存档域 ±20000（totkSane）；
//   - layer 18 地面 / 19 地底 / 20 天空（sky_totk.go，area_sky.js 64 多边形 + markers.js layer=20）；
//   - 存档 progress.sav 固定偏移 0x532AC/0x53324（两份副本）+ caption.sav 镜像 0x1E0，
//     slot_00 优先、其余按 mtime 降序、4 米内位置去重合并；
//   - TOTK 无神庙/洞窟回跳语义（live-go 无 shrine 逻辑），Shrine 系列空实现。
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---- TOTK 常量 ----
const (
	totkElevBias     = 105.0
	totkProgressOff1 = 0x532AC // progress.sav 玩家坐标（副本1）
	totkProgressOff2 = 0x53324 // progress.sav 玩家坐标（副本2）
	totkCaptionOff   = 0x1E0   // caption.sav 镜像
)

// totkValidTriple 内存域校验（live-go decodePos 同口径）。
func totkValidTriple(gx, gy, gz float32) bool {
	if gx != gx || gy != gy || gz != gz {
		return false // NaN
	}
	if abs32(gx) < 5 && abs32(gy) < 5 {
		return false // 近零簇
	}
	if abs32(gx) < 1e-20 || abs32(gy) < 1e-20 {
		return false // 单轴 denormal
	}
	if gx <= -6000 || gx >= 6000 || gy <= -6000 || gy >= 6000 ||
		gz <= -1300 || gz >= 3200 {
		return false // 内存合法域
	}
	return true
}

// totkSane 存档域校验（live-go savefile_totk.go 同口径）。
func totkSane(gx, gy, gz float32) bool {
	return gx > -20000 && gx < 20000 && gy > -20000 && gy < 20000 &&
		gz > -2000 && gz < 5000 && (abs32(gx) > 1 || abs32(gy) > 1)
}

type gameTotk struct{}

func (g *gameTotk) Name() string   { return "totk" }
func (g *gameTotk) MapURL() string { return "https://totk.yalin.site/" }

// DecodeAt 读 12 字节（小端）→ 内存序 (X, Z_stored, Y北) → 展示序 [gx=X, gy=-Y北, gz=alt]。
func (g *gameTotk) DecodeAt(h uintptr, addr uintptr) []float32 {
	d := readMem(h, addr, 12)
	if len(d) < 12 {
		return nil
	}
	a := floats(d[:12])
	gx, zs, yn := a[0], a[1], a[2]
	gy, gz := -yn, zs-totkElevBias
	if !totkValidTriple(gx, gy, gz) {
		return nil
	}
	return []float32{gx, gy, gz}
}

// Display 恒等：DecodeAt 已返回展示序 (gx, gy, gz)。
func (g *gameTotk) Display(mem [3]float32) (gx, gy, gz float32) {
	return mem[0], mem[1], mem[2]
}

func (g *gameTotk) ToMap(gx, gy, gz float32) (mx, my float32) {
	return -gy, gx
}

func (g *gameTotk) LayerOf(gx, gy, gz float32) int {
	return totkLayerOf(gx, gy, gz)
}

// ValidRaw 第二道校验（固定偏移候选）：DecodeAt 已给展示序，直接按内存域校验。
func (g *gameTotk) ValidRaw(mem []float32, littleEndian bool) bool {
	return totkValidTriple(mem[0], mem[1], mem[2])
}

// KnownOffsets TOTK 仅 Ryujinx（TOTK 无 Wii U 版）。
// 偏移来自 TOTKmap known_addrs.json（1.8.7 实测积累，含 0x35DA00 低偏移条目），
// P2a 先硬编码 10 条，P4 扫描回写联动后转为 known_totk_ryujinx.json 持久化。
func (g *gameTotk) KnownOffsets(p Platform) []uintptr {
	if p != nil && p.Name() == "Ryujinx" {
		return []uintptr{
			0xEA24E3C0, 0xD049C4B0, 0xEA2479C0, 0xEA2474C0, 0xD8696F04,
			0xFC9C7714, 0xEDB1CAF0, 0xFC9C80B4, 0x35DA00, 0xEE86CFB0,
		}
	}
	return nil
}

// PickOffset TOTK：多副本共识仲裁（live-go tryOffsets 同口径）。
// 收集所有通过校验的偏移候选，统计"坐标几乎相同（<2）"的副本数，取共识最多的；
// 共识 <2 且候选 >1 时拒绝锁定——TOTK 内存里存在多个合法但非玩家的三元组
// （NPC/静态数据/旧会话残留），无共识即锁错对象（症状：锁定后不跟随、漂移卡死）。
func (g *gameTotk) PickOffset(cands []OffsetCand, p Platform) (OffsetCand, string, bool) {
	if len(cands) == 0 {
		return OffsetCand{}, "", false
	}
	best, bestN := cands[0], -1
	for _, a := range cands {
		n := 0
		for _, b := range cands {
			if abs32(a.Pos[0]-b.Pos[0]) < 2 && abs32(a.Pos[1]-b.Pos[1]) < 2 && abs32(a.Pos[2]-b.Pos[2]) < 2 {
				n++
			}
		}
		if n > bestN {
			best, bestN = a, n
		}
	}
	if bestN < 2 && len(cands) > 1 {
		return OffsetCand{}, fmt.Sprintf("no consensus (%d copies max, want >=2) - refusing to lock", bestN), false
	}
	return best, fmt.Sprintf("consensus=%d copies [confirmed]", bestN), true
}

// TOTK 无神庙回跳语义（live-go 无 shrine 逻辑）：空实现，状态机不会进入 shrineMode。
func (g *gameTotk) ShrineExit() float32                          { return 0 }
func (g *gameTotk) MatchShrine(gx, gy, gz float32) *ProgressPoint { return nil }
func (g *gameTotk) ShrineReturn() (time.Duration, float32)        { return 0, 0 }

func (g *gameTotk) SaveFileNames() []string {
	return []string{"progress.sav", "caption.sav"}
}

// SaveAnchors 读 progress.sav/caption.sav 固定偏移 → 展示序锚点 (gx, gy, gz)。
// 位置去重 + slot_00 优先 + 其余 mtime 降序（live-go readTotkAnchors 同口径）。
func (g *gameTotk) SaveAnchors(p Platform) []*SaveAnchor {
	type totkCand struct {
		mt   int64
		path string
		pos  [3]float32
	}
	var cands []totkCand
	seen := map[[3]int32]bool{}
	for _, f := range allSaveFiles(p) {
		var offs []int
		switch strings.ToLower(filepath.Base(f)) {
		case "progress.sav":
			offs = []int{totkProgressOff1, totkProgressOff2}
		case "caption.sav":
			offs = []int{totkCaptionOff}
		default:
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fi, _ := os.Stat(f)
		mt := int64(0)
		if fi != nil {
			mt = fi.ModTime().UnixNano()
		}
		for _, off := range offs {
			if off+12 > len(data) {
				continue
			}
			mx := math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
			mz := math.Float32frombits(binary.LittleEndian.Uint32(data[off+4:]))
			my := math.Float32frombits(binary.LittleEndian.Uint32(data[off+8:]))
			gx, gz, gy := mx, mz-totkElevBias, -my // 展示序 (X, -Y北, alt)
			if !totkSane(gx, gy, gz) {
				continue
			}
			key := [3]int32{
				int32(math.Round(float64(gx) * 10)),
				int32(math.Round(float64(gz) * 10)),
				int32(math.Round(float64(gy) * 10)),
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			cands = append(cands, totkCand{mt, f, [3]float32{gx, gy, gz}})
		}
	}
	// slot_00 优先，其余按 mtime 降序（小集合冒泡）。
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			bi, bj := totkSlotRank(cands[i].path), totkSlotRank(cands[j].path)
			if bi > bj || (bi == bj && cands[i].mt < cands[j].mt) {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	var out []*SaveAnchor
	for _, c := range cands {
		out = append(out, &SaveAnchor{Path: c.path, Pos: c.pos})
	}
	return out
}

// totkSlotRank slot_00=0，其余=1（越小越优先）。
func totkSlotRank(path string) int {
	if strings.Contains(path, "slot_00") {
		return 0
	}
	return 1
}
