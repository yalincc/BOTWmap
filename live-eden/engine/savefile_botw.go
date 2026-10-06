// savefile_botw.go — BOTW 存档锚点读取（Eden 版）。
//
// 与 xnavi/savefile.go（Switch 小端 + Wii U 大端双平台已验证）同口径：
//   - 存档文件 game_data.sav：12 字节头 + 0x0C 起 [hash u32][value u32] 扁平表
//   - 玩家坐标 = PLAYER_POSITION hash (0xA40BA103) 连续三个 8 字节块（同 hash 重复三次）
//   - 内存序 = (X east, alt 高, Z south+)，无 elevBias（BOTW 高度直读，与 TOTK 的
//     Z_stored=alt+105 不同）
//   - 主档策略：最新 mtime 槽（BOTW 轮转写槽，槽号不可靠，照 live-cemu）
//   - 标题 ID 01007EF00011E000 过滤（Eden 存档根下与 TOTK 共存，防锚点污染）
//
// 本文件同时补回被删除的 TOTK 文件所依赖的公共符号：abs32 / dist3f / SaveAnchor /
// elevBias（BOTW = 0）。

package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// elevBias BOTW 无高度 bias（内存第二轴 = 高度直读）。保留符号名只为最小改动
// 共享层（decodePos / coords.go 的 zs-elevBias 表达式原样成立，值恒 0）。
const elevBias = 0.0

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func dist3f(a, b [3]float32) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// playerPositionHash = PLAYER_POSITION（xnavi/savefile.go 已验证）。
const playerPositionHash = 0xA40BA103

// botwTitleID 是《旷野之息》存档目录的标题 ID 段。Eden 存档根下同时存在
// BOTW（01007EF00011E000）与 TOTK（0100F2C0115B6000）——按标题 ID 过滤，
// 否则 TOTK 的 progress.sav（不同格式）不会误命中（文件名不同），主要防
// 目录遍历污染。
const botwTitleID = "01007EF00011E000"

// validSaveVersions 存档格式版本号（Switch/Wii U 共用，仅字节序相反）。
var validSaveVersions = []uint32{0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E}

// SaveAnchor 一条 BOTW 存档的解析结果：内存序三元组 = (X east, alt 高, Z south+)。
type SaveAnchor struct {
	Path     string
	Playtime uint32
	Koroks   uint32
	Pos      [3]float32
}

// botwSane 校验存档坐标是否合法（xnavi gameValidTriple 同判据范围）。
func botwSane(x, alt, z float32) bool {
	if x != x || alt != alt || z != z {
		return false
	}
	if x == 0 && alt == 0 && z == 0 {
		return false
	}
	if abs32(x) < 5 && abs32(z) < 5 {
		return false
	}
	if (abs32(x) < 5 && alt < 5) || (abs32(z) < 5 && alt < 5) {
		return false
	}
	if abs32(x) < 1e-20 || abs32(z) < 1e-20 {
		return false
	}
	if x <= -7000 || x >= 7000 || z <= -7000 || z >= 7000 || alt <= -600 || alt >= 5000 {
		return false
	}
	if abs32(x) < 100 && abs32(z) < 100 && alt < 100 {
		return false
	}
	return true
}

// detectEndian 按头部版本字段判定：true=小端（Switch），false=大端（Wii U/Cemu）。
func detectEndian(data []byte) bool {
	if len(data) >= 4 {
		vLE := binary.LittleEndian.Uint32(data[0:4])
		vBE := binary.BigEndian.Uint32(data[0:4])
		for _, v := range validSaveVersions {
			if v == vLE {
				return true
			}
			if v == vBE {
				return false
			}
		}
	}
	return true // Switch 默认小端
}

// parseBotwGameData 解析单个 game_data.sav → 玩家坐标 + 元信息（字节序自适应）。
func parseBotwGameData(path string) *SaveAnchor {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	little := detectEndian(data)
	u32 := binary.BigEndian.Uint32
	if little {
		u32 = binary.LittleEndian.Uint32
	}
	var playtime, koroks uint32
	var pos [3]float32
	hasPos := false
	for i := 0x0C; i+8 <= len(data); i += 8 {
		h := u32(data[i:])
		v := u32(data[i+4:])
		switch h {
		case playerPositionHash:
			// 坐标 = 连续三个 8 字节块（同 hash 重复三次：X / alt / Z 各一个 value 槽）。
			// 只取第一次命中：后续两次伪命中（i+8/i+16 的 hash 对齐错位）会读出
			// 垃圾值覆盖 pos（live-go parseSave 用 break 同理，2026-10-07 实测）。
			if !hasPos && i+24 <= len(data) {
				pos = [3]float32{
					math.Float32frombits(u32(data[i+4:])),
					math.Float32frombits(u32(data[i+12:])),
					math.Float32frombits(u32(data[i+20:])),
				}
				hasPos = true
			}
		case 0x73C29681: // PlayTime
			playtime = v
		case 0x8A94E07A: // HiddenKorok_Number
			koroks = v
		}
	}
	if !hasPos || !botwSane(pos[0], pos[1], pos[2]) {
		return nil
	}
	return &SaveAnchor{Path: path, Playtime: playtime, Koroks: koroks, Pos: pos}
}

// botwSaveRoot 返回 BOTW 存档根目录（Eden 版，照 savefile_totk.go 的推导链）：
// 环境变量 BOTW_SAVE_DIR → 正在运行的 eden.exe 所在目录推导 → 常见盘符便携
// 布局 → APPDATA 安装布局。
func botwSaveRoot() string {
	if v := os.Getenv("BOTW_SAVE_DIR"); v != "" {
		return v
	}
	if h := procHandleNow(); h != 0 {
		if p := processPath(h); p != "" {
			cand := filepath.Join(filepath.Dir(p), "user", "nand", "user", "save")
			if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
				return cand
			}
		}
	}
	for _, drive := range []string{"C:\\", "D:\\", "E:\\", "F:\\", "G:\\", "H:\\", "I:\\", "J:\\", "K:\\", "L:\\"} {
		for _, sub := range []string{
			filepath.Join(drive, "YUZU", "eden", "user", "nand", "user", "save"),
			filepath.Join(drive, "yuzu", "eden", "user", "nand", "user", "save"),
			filepath.Join(drive, "YUZU", "user", "nand", "user", "save"),
		} {
			if fi, err := os.Stat(sub); err == nil && fi.IsDir() {
				return sub
			}
		}
	}
	for _, c := range []string{
		filepath.Join(os.Getenv("APPDATA"), "Eden", "user", "nand", "user", "save"),
		filepath.Join(os.Getenv("APPDATA"), "yuzu", "user", "nand", "user", "save"),
	} {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return filepath.Join(os.Getenv("APPDATA"), "Eden", "user", "nand", "user", "save")
}

// findBotwSaveFiles 递归找 BOTW 标题目录下所有 game_data.sav。
func findBotwSaveFiles(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if strings.EqualFold(d.Name(), "game_data.sav") {
				if !strings.Contains(p, botwTitleID) {
					return nil // 非 BOTW 存档（如 TOTK 目录），跳过
				}
				out = append(out, p)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

type botwCand struct {
	mt   int64
	anch *SaveAnchor
}

// readBotwAnchors 读全部 BOTW 存档锚点 → 内存序三元组，按位置去重（保留 mtime 最新），
// 排序 = mtime 降序（最新存档优先作权威）。
func readBotwAnchors() []*SaveAnchor {
	var cands []botwCand
	seen := map[[3]int32]bool{}
	for _, p := range findBotwSaveFiles(botwSaveRoot()) {
		a := parseBotwGameData(p)
		if a == nil {
			continue
		}
		fi, err := os.Stat(p)
		mt := int64(0)
		if err == nil {
			mt = fi.ModTime().UnixNano()
		}
		key := [3]int32{
			int32(math.Round(float64(a.Pos[0]) * 10)),
			int32(math.Round(float64(a.Pos[1]) * 10)),
			int32(math.Round(float64(a.Pos[2]) * 10)),
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		cands = append(cands, botwCand{mt, a})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt > cands[j].mt })
	var out []*SaveAnchor
	for _, c := range cands {
		out = append(out, c.anch)
	}
	return out
}

// ---- 锚点缓存（照 TOTK 版：存档瞬态写入时读空回退缓存）----

var (
	botwAnchorCache   []*SaveAnchor
	botwAnchorCacheAt time.Time
)

const botwAnchorCacheTTL = 10 * time.Minute

func readBotwAnchorsCached() []*SaveAnchor {
	a := readBotwAnchors()
	if len(a) > 0 {
		botwAnchorCache = a
		botwAnchorCacheAt = time.Now()
		return a
	}
	if len(botwAnchorCache) > 0 && time.Since(botwAnchorCacheAt) < botwAnchorCacheTTL {
		return botwAnchorCache
	}
	return nil
}

// botwAnchorClusterRadius 锚点聚类半径（米）：与 TOTK 版同阈值——历史存档点
// 彼此 <300m，神庙/洞穴本地坐标距大地图 >1500m，800m 完整收下同区域锚点、
// 剔除本地场景坐标，杜绝窗口过滤被小数值占位区污染。
const botwAnchorClusterRadius = 800.0

// anchorRefs 锚点去重合并 + 离群剔除（与 TOTK 版同口径，权威点 = 最新 mtime 槽）：
// 以权威锚点为圆心，只保留与其距离 ≤ botwAnchorClusterRadius 的锚点；缺失时
// 退化为最大簇兜底；4m 内视为同一点去重，上限 limit。
func anchorRefs(anchors []*SaveAnchor, limit int, merge float32) [][3]float32 {
	if limit <= 0 {
		limit = 8
	}
	if merge <= 0 {
		merge = 4.0
	}

	// 1) 权威锚点 = 最新 mtime 槽（BOTW 轮转写槽，槽号不可靠，照 live-cemu）
	var auth *SaveAnchor
	if len(anchors) > 0 {
		auth = anchors[0]
	}

	// 2) 收集权威圆心半径内的锚点（同区域簇）
	var cluster []*SaveAnchor
	if auth != nil {
		cluster = append(cluster, auth)
		for _, a := range anchors {
			if a == auth {
				continue
			}
			if dist3f(a.Pos, auth.Pos) <= botwAnchorClusterRadius {
				cluster = append(cluster, a)
			}
		}
	} else {
		best := []*SaveAnchor{}
		for i, a := range anchors {
			c := []*SaveAnchor{a}
			for j, b := range anchors {
				if i != j && dist3f(a.Pos, b.Pos) <= botwAnchorClusterRadius {
					c = append(c, b)
				}
			}
			if len(c) > len(best) {
				best = c
			}
		}
		cluster = best
	}

	// 3) merge 去重 + 上限
	var out [][3]float32
	for _, a := range cluster {
		r := a.Pos
		dup := false
		for _, o := range out {
			if abs32(r[0]-o[0]) <= merge && abs32(r[1]-o[1]) <= merge && abs32(r[2]-o[2]) <= merge {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, r)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}
