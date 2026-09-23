// M1 探测逻辑：实体候选数量/稳定性、表头（count/指针）排查、类型字段候选。
// 输出探测报告（控制台 + data/probe_report.json）。

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// TypeField 一个类型字段候选：偏移 + 各实体取值分布。
type TypeField struct {
	Off    int      `json:"off"`             // 相对结构起始的字节偏移
	Size   int      `json:"size"`            // 1/2/4 字节
	Values []uint32 `json:"values"`          // 各实体的取值（去重展示）
	Kinds  int      `json:"kinds"`           // 不同取值个数
}

// ProbeReport M1 探测报告。
type ProbeReport struct {
	Generated    string      `json:"generated"`
	Frames       int         `json:"frames"`
	EntitiesMin  int         `json:"entities_min"`
	EntitiesMax  int         `json:"entities_max"`
	EntitiesLast int         `json:"entities_last"`
	StableAddrs  int         `json:"stable_addrs"` // 跨帧地址稳定的实体数
	DriftAddrs   int         `json:"drift_addrs"`  // 跨帧地址漂移的实体数
	PlayerAddr   string      `json:"player_addr"`
	PlayerPos    []float32   `json:"player_pos"`
	AddrSpreadKB int         `json:"addr_spread_kb"` // 实体地址跨度（判断是否有表）
	TableHint    string      `json:"table_hint"`
	TypeFields   []TypeField `json:"type_fields"`
	Note         string      `json:"note"`
}

// findPlayerGroup 从候选组中找玩家槽：离存档锚点最近的组。
func findPlayerGroup(groups []*EntityGroup, anchors []*SaveAnchor) *EntityGroup {
	if len(anchors) == 0 {
		return nil
	}
	p := anchors[0].Pos
	best, bestD := (*EntityGroup)(nil), float64(1e18)
	for _, g := range groups {
		dx := float64(g.X) - float64(p[0])
		dy := float64(g.Y) - float64(p[1])
		dz := float64(g.Z) - float64(p[2])
		d := dx*dx + dy*dy + dz*dz
		if d < bestD {
			best, bestD = g, d
		}
	}
	if bestD < 2500 { // 50m 内
		return best
	}
	return nil
}

// addrSpread 实体地址跨度（KB）。地址集中 → 可能有表。
func addrSpread(groups []*EntityGroup) (minA, maxA uintptr) {
	if len(groups) == 0 {
		return 0, 0
	}
	minA, maxA = groups[0].Addrs[0], groups[0].Addrs[0]
	for _, g := range groups {
		for _, a := range g.Addrs {
			if a < minA {
				minA = a
			}
			if a > maxA {
				maxA = a
			}
		}
	}
	return
}

// scanTypeFields 类型字段候选排查：对每组结构取 128 字节，逐偏移统计
// u8/u16/u32 取值——值都是小整数、且在不同实体间有区分度的偏移即候选。
func scanTypeFields(h uintptr, groups []*EntityGroup) []TypeField {
	if len(groups) < 3 {
		return nil
	}
	const win = 128
	type valSet struct {
		vals map[uint32]bool
		n    int
	}
	sets := make([]valSet, win)
	for i := range sets {
		sets[i].vals = map[uint32]bool{}
	}
	for _, g := range groups[:min(len(groups), 60)] {
		buf := readMem(h, g.Addrs[0], win)
		if buf == nil {
			continue
		}
		for off := 0; off+1 <= len(buf); off++ {
			sets[off].vals[uint32(buf[off])] = true
			sets[off].n++
		}
	}
	var out []TypeField
	for off := 12; off < win-3; off++ { // 跳过前 12 字节（坐标区）
		s := sets[off]
		if s.n < 3 {
			continue
		}
		kinds := len(s.vals)
		// 候选条件：小整数域（所有值 <= 4095），不同实体间有区分度（2~64 种取值）
		allSmall := true
		for v := range s.vals {
			if v > 4095 {
				allSmall = false
				break
			}
		}
		if !allSmall || kinds < 2 || kinds > 64 {
			continue
		}
		out = append(out, TypeField{Off: off, Size: 1, Kinds: kinds})
	}
	// 限制输出数量，按区分度排序
	sort.Slice(out, func(i, j int) bool { return out[i].Kinds > out[j].Kinds })
	if len(out) > 12 {
		out = out[:12]
	}
	for i := range out {
		vals := make([]uint32, 0, len(sets[out[i].Off].vals))
		for v := range sets[out[i].Off].vals {
			vals = append(vals, v)
		}
		sort.Slice(vals, func(a, b int) bool { return vals[a] < vals[b] })
		out[i].Values = vals
	}
	return out
}

// runProbe M1 主探测：窗口扫描（锚点 ±win）连续 frames 帧，统计稳定性，输出报告。
// 关键：全内存扫描会淹没在噪声里（6000 万坐标三元组，绝大多数非实体），
// 必须用 live-go 验证过的窗口扫描模式——只扫锚点附近，再分组打分。
func runProbe(h uintptr, frames int) *ProbeReport {
	anchors := readSaveAnchors()
	rep := &ProbeReport{Generated: time.Now().Format("2006-01-02 15:04:05"), Frames: frames}
	rep.EntitiesMin, rep.EntitiesMax = 1<<30, 0

	if len(anchors) == 0 {
		rep.Note = "no save anchor; abort."
		return rep
	}
	refs := [][3]float32{anchors[0].Pos}

	// 帧间跟踪：按 RepAddr 匹配——上一帧出现过的地址在本帧仍在且 RepAddr 不变 = 稳定。
	prev := map[uintptr]bool{}
	var stable, drift int
	var lastGroups []*EntityGroup

	for f := 0; f < frames; f++ {
		groups, hits, secs := findEntitiesNear(h, refs, 400)
		lastGroups = groups
		n := len(groups)
		if n < rep.EntitiesMin {
			rep.EntitiesMin = n
		}
		if n > rep.EntitiesMax {
			rep.EntitiesMax = n
		}
		fmt.Printf("  frame %d/%d: %d entity candidates (window %d hits, %.1fs)\n", f+1, frames, n, hits, secs)
		if f < frames-1 {
			cur := map[uintptr]bool{}
			for _, g := range groups {
				a := g.RepAddr
				cur[a] = true
				if prev[a] {
					stable++
				}
			}
			for a := range prev {
				if !cur[a] {
					drift++
				}
			}
			prev = cur
			time.Sleep(1000 * time.Millisecond)
		}
	}

	// 最后一帧：玩家槽 + 非玩家组 + 类型字段 + 表头
	rep.EntitiesLast = len(lastGroups)
	player := findPlayerGroup(lastGroups, anchors)
	var nonPlayer []*EntityGroup
	for _, g := range lastGroups {
		if player != nil && g == player {
			continue
		}
		nonPlayer = append(nonPlayer, g)
	}
	if player != nil {
		rep.PlayerAddr = hexAddr(player.Addrs[0])
		rep.PlayerPos = []float32{player.X, player.Y, player.Z}
	}
	rep.StableAddrs, rep.DriftAddrs = stable, drift
	rep.TypeFields = scanTypeFields(h, nonPlayer)

	// struct 分布诊断：按 struct 计数直方图，帮助判断"多少是真槽、多少是巧合"
	hist := map[int]int{}
	for _, g := range nonPlayer {
		hist[g.Struct]++
	}
	fmt.Printf("  struct 分布（非玩家候选组）: ")
	for s := 1; s <= 16; s++ {
		if n := hist[s]; n > 0 {
			fmt.Printf("s%d:%d ", s, n)
		}
	}
	fmt.Println()

	minA, maxA := addrSpread(nonPlayer)
	spread := int((maxA - minA) / 1024)
	rep.AddrSpreadKB = spread
	switch {
	case len(nonPlayer) == 0:
		rep.TableHint = "no entities found"
	case spread < 1024:
		rep.TableHint = fmt.Sprintf("entities concentrated within %dKB -> table-like structure likely (0x%X..0x%X)", spread, minA, maxA)
	default:
		rep.TableHint = fmt.Sprintf("entities spread across %dKB -> scattered, table uncertain (0x%X..0x%X)", spread, minA, maxA)
	}
	rep.Note = "表头需进一步在 M3 用指针链探测确认；类型字段为启发式候选，需对照游戏内实体验证。"
	return rep
}

func saveReport(rep *ProbeReport) string {
	exe, _ := os.Executable()
	dir := filepath.Join(filepath.Dir(exe), "data")
	os.MkdirAll(dir, 0755)
	p := filepath.Join(dir, "probe_report.json")
	buf, _ := json.MarshalIndent(rep, "", "  ")
	os.WriteFile(p, buf, 0644)
	return p
}

func printReport(rep *ProbeReport) {
	fmt.Println("\n===== M1 探测报告 =====")
	fmt.Printf("  实体候选数量:  min=%d  max=%d  last=%d\n", rep.EntitiesMin, rep.EntitiesMax, rep.EntitiesLast)
	fmt.Printf("  稳定性:  stable=%d  drift=%d\n", rep.StableAddrs, rep.DriftAddrs)
	fmt.Printf("  玩家槽:  %s  pos=%v\n", rep.PlayerAddr, rep.PlayerPos)
	fmt.Printf("  地址跨度:  %d KB\n", rep.AddrSpreadKB)
	fmt.Printf("  表头提示:  %s\n", rep.TableHint)
	fmt.Printf("  类型字段候选 (%d):\n", len(rep.TypeFields))
	for _, tf := range rep.TypeFields {
		fmt.Printf("    +%d (u8): kinds=%d vals=%v\n", tf.Off, tf.Kinds, tf.Values)
	}
	fmt.Printf("  备注: %s\n", rep.Note)
}
