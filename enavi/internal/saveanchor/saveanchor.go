// Package saveanchor 从《旷野之息》存档里读玩家坐标，作为内存扫描的「锚点」。
//
// 为什么需要它：2026-10-20 实测证明，只靠内存特征（三个 float 合法 + 附近有旋转矩阵）
// 在 2.69GB 的 User Heap 里会捞出 6 万个候选（其中 8 千个还是世界尺度），
// 无法唯一确定林克。存档锚点是确定性的：它给出一组精确世界坐标，
// 把扫描窗口从「整个堆」缩到「锚点 ±几十米」，命中数立刻降到个位数。
//
// 格式（xnavi 已实证，Switch/Wii U 通用）：
//
//	文件头 12 字节，之后是 [hash u32][value u32] 的扁平表；
//	玩家坐标 = PLAYER_POSITION(0xA40BA103) 连续三个 8 字节块（同 hash 重复三次），
//	三个 value 分别是 (X, alt, Z) 的 float32 位模式。
package saveanchor

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	hashPlayerPosition = 0xA40BA103 // PLAYER_POSITION
	hashPlaytime       = 0x73C29681 // PlayTime
	hashKorokCounter   = 0x8A94E07A // HiddenKorok_Number
	// 玩家属性（用于在内存里唯一定位 PlayerInfo 区块）
	hashMaxHart           = 688321319  // MaxHartValue   = 心数 × 4
	hashCurrentHart       = 3197880723 // CurrentHart    = 当前心心 × 4
	hashStaminaCurrentMax = 1487888044 // StaminaCurrentMax（1000 = 一圈）
	hashStaminaMax        = 987754567  // StaminaMax
	// BOTW 在 Eden/Nyujinx 上的标题 ID
	botwTitleID = "01007EF00011E000"
)

// validVersions 已知存档格式版本（Switch 小端 / Wii U 大端，仅字节序相反）。
var validVersions = []uint32{0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E}

// Anchor 一条存档的解析结果。
type Anchor struct {
	Path     string
	Pos      [3]float32 // (X, alt, Z) 内存序
	Playtime uint32     // 秒
	Koroks   uint32
	// 玩家属性（PlayerInfo 指纹，内存定位用）
	MaxHart           uint32
	CurrentHart       uint32
	StaminaCurrentMax uint32
	StaminaMax        uint32
	MTime             time.Time
}

// VecString 便于日志输出。
func (a Anchor) VecString() string {
	return fmt.Sprintf("(%.1f, %.1f, %.1f)", a.Pos[0], a.Pos[1], a.Pos[2])
}

// AttrString 玩家属性摘要（日志用）。
func (a Anchor) AttrString() string {
	return fmt.Sprintf("心 %d/%d 体力 %.0f/%.0f",
		a.CurrentHart, a.MaxHart,
		math.Float32frombits(a.StaminaCurrentMax), math.Float32frombits(a.StaminaMax))
}

// Roots 返回 Eden 可能的存档根候选（照 xnavi platform_eden.go 的顺序）。
// exePath = 运行中 eden.exe 的完整路径，可为空。
func Roots(exePath string) []string {
	var roots []string
	if v := os.Getenv("BOTW_SAVE_DIR"); v != "" {
		roots = append(roots, v)
	}
	if exePath != "" {
		dir := filepath.Dir(exePath)
		roots = append(roots,
			filepath.Join(dir, "user", "nand", "user", "save"),
			filepath.Join(dir, "..", "user", "nand", "user", "save"),
			filepath.Join(dir, "..", "..", "user", "nand", "user", "save"),
		)
	}
	for _, d := range []string{"C:", "D:", "E:", "F:", "G:", "H:", "I:", "J:", "K:", "L:"} {
		roots = append(roots,
			filepath.Join(d+`\`, "YUZU", "eden", "user", "nand", "user", "save"),
			filepath.Join(d+`\`, "yuzu", "eden", "user", "nand", "user", "save"),
			filepath.Join(d+`\`, "Eden", "user", "nand", "user", "save"),
		)
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		roots = append(roots,
			filepath.Join(appdata, "Eden", "user", "nand", "user", "save"),
			filepath.Join(appdata, "eden", "user", "nand", "user", "save"),
		)
	}
	return roots
}

// Find 在候选根下找 BOTW 存档并解析，返回按「可信度」排序的结果。
//
// 目录层级实测（Eden，2026-10-20）：
//
//	<exe目录>/user/nand/user/save/<16位账户ID>/01007EF00011E000/<槽位>/game_data.sav
//
// 中间多两层 ID 目录，所以不能拼死路径，必须在存档根下按深度找标题目录。
func Find(exePath string) ([]Anchor, []string) {
	var anchors []Anchor
	var scanned []string
	seenRoot := map[string]bool{}

	for _, root := range Roots(exePath) {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() || seenRoot[root] {
			continue
		}
		seenRoot[root] = true
		scanned = append(scanned, root)

		// 在 root 下最多 6 层内找 BOTW 标题目录
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if rel, e := filepath.Rel(root, p); e == nil && rel != "." {
				if strings.Count(rel, string(os.PathSeparator)) > 5 {
					return filepath.SkipDir
				}
			}
			if !strings.EqualFold(d.Name(), botwTitleID) {
				return nil
			}
			_ = filepath.WalkDir(p, func(f string, fd os.DirEntry, e2 error) error {
				if e2 != nil || fd.IsDir() {
					return nil
				}
				if strings.EqualFold(fd.Name(), "game_data.sav") {
					if a, e3 := Parse(f); e3 == nil {
						anchors = append(anchors, *a)
					}
				}
				return nil
			})
			return filepath.SkipDir
		})
	}

	// 去重：Windows 大小写不敏感，G://YUZU 与 G://yuzu 是同一条
	uniq := anchors[:0]
	seenPath := map[string]bool{}
	for _, a := range anchors {
		k := strings.ToLower(filepath.Clean(a.Path))
		if seenPath[k] {
			continue
		}
		seenPath[k] = true
		uniq = append(uniq, a)
	}
	anchors = uniq

	sort.Slice(anchors, func(i, j int) bool {
		bi, bj := anchors[i].Playtime > 0, anchors[j].Playtime > 0
		if bi != bj {
			return bi
		}
		return anchors[i].MTime.After(anchors[j].MTime)
	})
	return anchors, scanned
}

// Best 从候选存档里挑出「最可能是玩家当前在玩的那一档」。
//
// ★ 为什么需要（2026-10-21 修正）：
//   Eden 的 BOTW 存档有 6 个槽（轮转写），其中可能混着：
//     · 空档 / 新档（刚开的新游戏，PlayTime=0、克洛格=0）—— 坐标是初始位置，
//       拿它当扫描锚点会扫到出厂点位，锁不到玩家；
//     · 玩家早已不玩的旧档 —— 坐标在几公里外。
//   旧逻辑（locate 直接取 anchors[0]）只按 (Playtime>0, mtime) 排，
//   遇到「新档 PlayTime 恰好>0 但进度为空」就会选错。
//
// 判据（从严到宽）：
//  1. 有进度（克洛格数 > 0 或 游玩时长 > 0）→ 合格档；
//  2. 合格档里取 mtime 最新的（BOTW 轮转写，最近写过的最接近当前状态）；
//  3. 一个合格档都没有 → 退回 mtime 最新的（总比没有强，但坐标可能不准）。
//
// 返回 nil 表示没有可用存档。
func Best(anchors []Anchor) *Anchor {
	if len(anchors) == 0 {
		return nil
	}
	// 合格档：有进度
	var qualified []Anchor
	for _, a := range anchors {
		if a.Koroks > 0 || a.Playtime > 0 {
			qualified = append(qualified, a)
		}
	}
	if len(qualified) == 0 {
		// 全是空档/新档 → 取最新的，并标记出来（调用方可据此降级）
		best := anchors[0]
		for _, a := range anchors[1:] {
			if a.MTime.After(best.MTime) {
				best = a
			}
		}
		return &best
	}
	best := qualified[0]
	for _, a := range qualified[1:] {
		if a.MTime.After(best.MTime) {
			best = a
		}
	}
	return &best
}

// Parse 解析单个 game_data.sav。
func Parse(path string) (*Anchor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 0x0C+24 {
		return nil, fmt.Errorf("文件太小（%d 字节）", len(data))
	}
	little := detectEndian(data)
	u32 := binary.BigEndian.Uint32
	if little {
		u32 = binary.LittleEndian.Uint32
	}

	// 扁平表：先建 hash → value（PlayTime / 克洛格数用）
	entries := make(map[uint32]uint32, len(data)/8)
	for o := 0x0C; o+8 <= len(data); o += 8 {
		entries[u32(data[o:])] = u32(data[o+4:])
	}

	// 玩家坐标：PLAYER_POSITION 连续三块，取值分别在 +4 / +12 / +20
	var pos [3]float32
	found := false
	for i := 0x0C; i+24 <= len(data); i += 8 {
		if u32(data[i:]) != hashPlayerPosition {
			continue
		}
		pos = [3]float32{
			math.Float32frombits(u32(data[i+4:])),
			math.Float32frombits(u32(data[i+12:])),
			math.Float32frombits(u32(data[i+20:])),
		}
		found = true
		break // 只取第一次命中：后面 i+8/i+16 会读到伪命中（xnavi 踩过的坑）
	}
	if !found || !validTriple(pos) {
		return nil, fmt.Errorf("没读到合法的 PLAYER_POSITION")
	}

	st, _ := os.Stat(path)
	var mt time.Time
	if st != nil {
		mt = st.ModTime()
	}
	return &Anchor{
		Path:              path,
		Pos:               pos,
		Playtime:          entries[hashPlaytime],
		Koroks:            entries[hashKorokCounter],
		MaxHart:           entries[hashMaxHart],
		CurrentHart:       entries[hashCurrentHart],
		StaminaCurrentMax: entries[hashStaminaCurrentMax],
		StaminaMax:        entries[hashStaminaMax],
		MTime:             mt,
	}, nil
}

func detectEndian(data []byte) bool {
	if len(data) >= 4 {
		le := binary.LittleEndian.Uint32(data[0:4])
		be := binary.BigEndian.Uint32(data[0:4])
		for _, v := range validVersions {
			if v == le {
				return true
			}
			if v == be {
				return false
			}
		}
	}
	return true // 兜底：Switch/Eden 是小端
}

// validTriple 世界坐标合法性（与 scan.Vec3.Valid 同口径）。
func validTriple(p [3]float32) bool {
	for _, v := range p {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	if math.Abs(float64(p[0])) > 7000 || math.Abs(float64(p[2])) > 7000 {
		return false
	}
	if float64(p[1]) < -600 || float64(p[1]) > 5000 {
		return false
	}
	return true
}
