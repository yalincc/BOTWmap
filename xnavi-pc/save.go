// save.go — Savage Wind 存档发现、解析与进度同步。
//
// 存档与现有 BOTW 存档逐字节同构（实测 game_data.sav 1027208B，头部 0000471B…），
// 因此 /save 直传二进制给网页 save_upload.js，/progress 沿用 progress_points.json
// 与 save_stats.json 的口径，网页端零改动。

package main

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed data/progress_points.json data/save_stats.json
var dataFS embed.FS

const (
	playerPositionHash = 0xA40BA103
	playtimeHash       = 0x73C29681
	korokCounterHash   = 0x8A94E07A
	hSpot              = 0x73F13FBC
)

var validSaveVersions = []uint32{
	0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46,
	0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E,
}

// progressPoint 与 progress_points.json 单条参考点同构（前端 live.js 直接消费）。
type progressPoint struct {
	T        string  `json:"t"`
	Hash     uint32  `json:"hash"`
	DoneHash uint32  `json:"done_hash,omitempty"`
	En       string  `json:"en"`
	Cn       string  `json:"cn"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Done     bool    `json:"done"`
	Entered  bool    `json:"entered,omitempty"`
	Dlc      bool    `json:"dlc,omitempty"`
	Tier     string  `json:"tier,omitempty"`
	PhotoNo  int     `json:"photoNo,omitempty"`
}

type saveStatsTable struct {
	Quest struct {
		Main   []uint32 `json:"main"`
		Side   []uint32 `json:"side"`
		Shrine []uint32 `json:"shrine"`
		DLC    []uint32 `json:"dlc"`
	} `json:"quest"`
	Chest   []uint32 `json:"chest"`
	Weapon  []uint32 `json:"weapon"`
	PBEquip []uint32 `json:"pbEquip"`
	PBMat   []uint32 `json:"pbMat"`
	PBEnemy []uint32 `json:"pbEnemy"`
	Boss    struct {
		Hinox   []uint32 `json:"hinox"`
		Talus   []uint32 `json:"talus"`
		Molduga []uint32 `json:"molduga"`
	} `json:"boss"`
}

var saveStatsKeys = []string{
	"quest_main", "quest_side", "quest_shrine", "quest_dlc",
	"chest", "weapon", "pb_equip", "pb_mat", "pb_enemy",
	"boss_hinox", "boss_talus", "boss_molduga",
}

var progTypes = []string{"shrine", "tower", "korok", "memory", "beast"}

func progIdx(t string) int {
	switch t {
	case "shrine":
		return 0
	case "tower":
		return 1
	case "korok":
		return 2
	case "memory":
		return 3
	case "beast":
		return 4
	}
	return 0
}

var (
	refsOnce      sync.Once
	refsPoints    []progressPoint
	shrineKeys    []uint32
	statsMasks    map[uint32]uint16
	statsTotal    [12]int
	refsOK        bool
	saveDirMu     sync.RWMutex
	saveDirManual string
)

func loadRefs() {
	refsOnce.Do(func() {
		buf, err := dataFS.ReadFile("data/progress_points.json")
		if err != nil {
			fmt.Printf("  [progress] 参考表读取失败: %v\n", err)
			return
		}
		var obj struct {
			Points []progressPoint `json:"points"`
		}
		if json.Unmarshal(buf, &obj) != nil || len(obj.Points) == 0 {
			fmt.Println("  [progress] 参考表格式无效")
			return
		}
		refsPoints = obj.Points
		for _, p := range refsPoints {
			if p.T != "shrine" {
				continue
			}
			key := p.Hash
			if p.DoneHash != 0 {
				key = p.DoneHash
			}
			shrineKeys = append(shrineKeys, key)
		}

		sb, err := dataFS.ReadFile("data/save_stats.json")
		if err == nil {
			var t saveStatsTable
			if json.Unmarshal(sb, &t) == nil {
				groups := [12][]uint32{
					t.Quest.Main, t.Quest.Side, t.Quest.Shrine, t.Quest.DLC,
					t.Chest, t.Weapon, t.PBEquip, t.PBMat, t.PBEnemy,
					t.Boss.Hinox, t.Boss.Talus, t.Boss.Molduga,
				}
				statsMasks = make(map[uint32]uint16, 2048)
				for idx, list := range groups {
					statsTotal[idx] = len(list)
					bit := uint16(1) << uint(idx)
					for _, h := range list {
						statsMasks[h] |= bit
					}
				}
			}
		}
		refsOK = true
		fmt.Printf("  [progress] 参考表 %d 点，扩展统计 12 类已载入\n", len(refsPoints))
	})
}

func countSaveStats(entries map[uint32]uint32) map[string][2]int {
	loadRefs()
	if !refsOK || statsMasks == nil {
		return nil
	}
	var done [12]int
	for h, v := range entries {
		if v == 0 {
			continue
		}
		mask, ok := statsMasks[h]
		if !ok {
			continue
		}
		for i := 0; i < 12; i++ {
			if mask&(1<<uint(i)) != 0 {
				done[i]++
			}
		}
	}
	out := make(map[string][2]int, 12)
	for i, name := range saveStatsKeys {
		out[name] = [2]int{done[i], statsTotal[i]}
	}
	return out
}

func shrineDoneCount(entries map[uint32]uint32) int {
	loadRefs()
	n := 0
	for _, h := range shrineKeys {
		if entries[h] != 0 {
			n++
		}
	}
	return n
}

// detectEndian 按头部版本字段判定字节序（true = Switch 小端）。
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
	return true
}

// parseSave 解析 game_data.sav → hash 表 + 玩家坐标（(X, alt, Z)）。
func parseSave(data []byte) (map[uint32]uint32, [3]float32, bool) {
	if len(data) < 0x0C+24 {
		return nil, [3]float32{}, false
	}
	little := detectEndian(data)
	u32 := binary.BigEndian.Uint32
	if little {
		u32 = binary.LittleEndian.Uint32
	}
	entries := make(map[uint32]uint32, 8192)
	for o := 0x0C; o+8 <= len(data); o += 8 {
		entries[u32(data[o:])] = u32(data[o+4:])
	}
	var pos [3]float32
	found := false
	for i := 0x0C; i+24 <= len(data); i += 8 {
		if u32(data[i:]) == playerPositionHash {
			pos = [3]float32{
				math.Float32frombits(u32(data[i+4:])),
				math.Float32frombits(u32(data[i+12:])),
				math.Float32frombits(u32(data[i+20:])),
			}
			found = true
			break
		}
	}
	return entries, pos, found
}

// saveRoots 存档候选根目录（按优先级）。
func saveRoots() []string {
	var out []string
	saveDirMu.RLock()
	manual := saveDirManual
	saveDirMu.RUnlock()
	if manual != "" {
		out = append(out, manual)
	}
	if v := os.Getenv("SAVAGEWIND_SAVE_DIR"); v != "" {
		out = append(out, v)
	}
	if v := os.Getenv("SAVAGEWIND_DATA_ROOT"); v != "" {
		out = append(out, filepath.Join(v, "save"))
	}
	if exe := getGameExePath(); exe != "" {
		out = append(out, filepath.Join(filepath.Dir(exe), "save"))
	}
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(cwd, "save"))
	}
	return out
}

func setSaveDirOverride(p string) {
	saveDirMu.Lock()
	saveDirManual = p
	saveDirMu.Unlock()
}

// findSaves 递归查找所有 game_data.sav（首个命中的根目录优先）。
func findSaves() []string {
	for _, root := range saveRoots() {
		fi, err := os.Stat(root)
		if err != nil {
			continue
		}
		var files []string
		if !fi.IsDir() {
			if strings.EqualFold(fi.Name(), "game_data.sav") {
				files = append(files, root)
			}
		} else {
			filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if strings.EqualFold(d.Name(), "game_data.sav") {
					files = append(files, p)
				}
				return nil
			})
		}
		if len(files) > 0 {
			return files
		}
	}
	return nil
}

// pickPrimary 主档策略：mtime 最新且排除空档（与 xnavi Cemu 口径一致）。
func pickPrimary(files []string) string {
	type cand struct {
		path string
		mt   time.Time
	}
	var cs []cand
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			cs = append(cs, cand{f, fi.ModTime()})
		}
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].mt.After(cs[j].mt) })
	for _, c := range cs {
		if data, err := readFileShared(c.path); err == nil {
			if entries, _, ok := parseSave(data); ok && shrineDoneCount(entries) > 0 {
				return c.path
			}
		}
	}
	if len(cs) > 0 {
		return cs[0].path
	}
	return ""
}

type saveState struct {
	mu         sync.RWMutex
	path       string
	modT       time.Time
	body       []byte
	gen        string
	ok         bool
	counts     map[string][2]int
	stats      map[string][2]int
	playtime   uint32
	koroks     uint32
	saveSource string
}

var saves = &saveState{}

func (s *saveState) snapshot() (bool, []byte, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ok, s.body, s.gen
}

func (s *saveState) summarize() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"save":       s.path,
		"saveSource": s.saveSource,
		"saveMtime":  s.modT.Unix(),
		"playtime":   s.playtime,
		"koroks":     s.koroks,
		"counts":     s.counts,
		"stats":      s.stats,
	}
}

func currentSavePath() string {
	saves.mu.RLock()
	defer saves.mu.RUnlock()
	return saves.path
}

// refresh 解析主档 → 现算 done → 缓存 /progress JSON。
func (s *saveState) refresh() {
	primary := pickPrimary(findSaves())
	if primary == "" {
		s.mu.Lock()
		s.ok = false
		s.mu.Unlock()
		return
	}
	data, err := readFileShared(primary)
	if err != nil {
		return
	}
	entries, _, ok := parseSave(data)
	if !ok {
		return
	}
	loadRefs()
	if !refsOK {
		return
	}

	pts := make([]progressPoint, len(refsPoints))
	copy(pts, refsPoints)
	var doneN, totalN [5]int
	for i := range pts {
		key := pts[i].Hash
		if pts[i].DoneHash != 0 {
			key = pts[i].DoneHash
		}
		pts[i].Done = entries[key] != 0
		totalN[progIdx(pts[i].T)]++
		if pts[i].Done {
			doneN[progIdx(pts[i].T)]++
		}
	}
	counts := map[string][2]int{}
	for i, name := range progTypes {
		counts[name] = [2]int{doneN[i], totalN[i]}
	}
	var mp, mf, ma, md [2]int
	for _, p := range pts {
		if p.T != "memory" {
			continue
		}
		var dst *[2]int
		switch {
		case p.Tier == "photo" && p.PhotoNo != 13:
			dst = &mp
		case p.Tier == "photo" && p.PhotoNo == 13:
			dst = &mf
		case p.Tier == "auto":
			dst = &ma
		case p.Tier == "dlc":
			dst = &md
		default:
			continue
		}
		dst[1]++
		if p.Done {
			dst[0]++
		}
	}
	counts["memory_photo"] = mp
	counts["memory_final"] = mf
	counts["memory_auto"] = ma
	counts["memory_dlc"] = md

	gen := time.Now().Format("2006-01-02 15:04:05")
	obj := map[string]any{
		"ok":        true,
		"points":    pts,
		"counts":    counts,
		"spot_int":  int(entries[hSpot]),
		"generated": gen,
		"source":    "xnavi",
	}
	body, _ := json.Marshal(obj)
	fi, _ := os.Stat(primary)
	mt := time.Time{}
	if fi != nil {
		mt = fi.ModTime()
	}

	s.mu.Lock()
	s.path = primary
	s.modT = mt
	s.body = body
	s.gen = gen
	s.ok = true
	s.counts = counts
	s.stats = countSaveStats(entries)
	s.playtime = entries[playtimeHash]
	s.koroks = entries[korokCounterHash]
	s.saveSource = "Savage Wind"
	s.mu.Unlock()
}

// saveLoop 每 2 秒检查存档 mtime，变化则重算（游戏内保存后 1~2 秒内更新）。
func (s *saveState) loop() {
	s.refresh()
	for {
		time.Sleep(2 * time.Second)
		latest, mt := "", time.Time{}
		for _, f := range findSaves() {
			if fi, err := os.Stat(f); err == nil && fi.ModTime().After(mt) {
				latest, mt = f, fi.ModTime()
			}
		}
		s.mu.RLock()
		changed := latest != "" && (latest != s.path || !mt.Equal(s.modT))
		s.mu.RUnlock()
		if changed {
			s.refresh()
		}
	}
}

func openSaveLocation(p string) error {
	fi, err := os.Stat(p)
	if err == nil && !fi.IsDir() {
		return exec.Command("explorer.exe", "/select,", filepath.Clean(p)).Start()
	}
	dir := filepath.Dir(filepath.Clean(p))
	if _, err := os.Stat(dir); err == nil {
		return exec.Command("explorer.exe", dir).Start()
	}
	return fmt.Errorf("路径不存在: %s", p)
}
