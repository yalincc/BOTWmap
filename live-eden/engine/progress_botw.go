// progress_botw.go — BOTW game_data.sav 解析 → /progress（points + counts）。
//
// 平移 live-cemu/progress.go（Wii U/Cemu 已验证）+ xnavi（Switch 小端已验证）：
//   - 文件 game_data.sav，12 字节头 + 从 0x0C 起的扁平表 [hash u32][value u32]
//   - 字节序按头部版本字段自动识别（Switch 小端 / Wii U 大端，版本表共用）
//   - 完成判定：优先用参考点的 done_hash，表中该项值 != 0 即完成
//   - /progress: {ok, points, counts, generated, save, source}
//   - 与 BOTWmap 前端 live.js 的 points 口径一致 → 前端零改动

package main

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed data/progress_points.json
var botwRefFS embed.FS

// 存档格式版本号（Switch / Wii U 共用，仅字节序相反）
var botwSaveVersions = []uint32{0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E}

const botwSaveName = "game_data.sav"

type ProgressPoint struct {
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

var botwProgTypes = []string{"shrine", "tower", "korok", "memory", "beast"}

type progressWatcher struct {
	mu   sync.RWMutex
	ok   bool
	refs []ProgressPoint
	body []byte
	gen  string
	save string
	modT time.Time
	init bool
}

var progress = &progressWatcher{}

/* ---------- 存档定位 ---------- */

// findBotwProgressFiles 在 Eden 存档根下递归找 BOTW（标题 ID 过滤）的 game_data.sav。
func findBotwProgressFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), botwSaveName) {
			if !strings.Contains(p, botwTitleID) {
				return nil
			}
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func latestBotwSave(files []string) (string, time.Time) {
	var best string
	var bestT time.Time
	for _, p := range files {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if best == "" || fi.ModTime().After(bestT) {
			best, bestT = p, fi.ModTime()
		}
	}
	return best, bestT
}

/* ---------- 解析 ---------- */

// detectBotwEndian 用头部版本字段判字节序：true=小端(Switch)，false=大端(Wii U/Cemu)
func detectBotwEndian(data []byte) bool {
	if len(data) >= 4 {
		vLE := binary.LittleEndian.Uint32(data[0:4])
		vBE := binary.BigEndian.Uint32(data[0:4])
		for _, v := range botwSaveVersions {
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

func parseBotwSave(path string) (map[uint32]uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	little := detectBotwEndian(data)
	entries := make(map[uint32]uint32, len(data)/8)
	for o := 0x0C; o+8 <= len(data); o += 8 {
		var h, v uint32
		if little {
			h = binary.LittleEndian.Uint32(data[o:])
			v = binary.LittleEndian.Uint32(data[o+4:])
		} else {
			h = binary.BigEndian.Uint32(data[o:])
			v = binary.BigEndian.Uint32(data[o+4:])
		}
		entries[h] = v
	}
	if len(entries) < 100 {
		return entries, fmt.Errorf("abnormal save (%d entries): %s", len(entries), path)
	}
	return entries, nil
}

/* ---------- 参考表与判定 ---------- */

func (w *progressWatcher) loadRefs() {
	buf, err := botwRefFS.ReadFile("data/progress_points.json")
	if err != nil {
		fmt.Printf("  [progress] reference table unreadable: %v\n", err)
		return
	}
	var obj struct {
		Points []ProgressPoint `json:"points"`
	}
	if json.Unmarshal(buf, &obj) != nil || len(obj.Points) == 0 {
		fmt.Println("  [progress] reference table invalid")
		return
	}
	w.refs = obj.Points
	w.ok = true
	n := map[string]int{}
	for _, p := range w.refs {
		n[p.T]++
	}
	fmt.Printf("  [progress] reference table: %d points (shrine %d, tower %d, korok %d, memory %d, beast %d)\n",
		len(w.refs), n["shrine"], n["tower"], n["korok"], n["memory"], n["beast"])
}

func (w *progressWatcher) refresh() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.refs) == 0 {
		return
	}
	best, mtime := latestBotwSave(findBotwProgressFiles(botwSaveRoot()))
	if best == "" {
		if !w.init {
			fmt.Println("  [progress] no BOTW save found yet - watching")
		}
		w.ok = false
		w.init = true
		return
	}
	entries, perr := parseBotwSave(best)
	if perr != nil {
		if !w.init {
			fmt.Printf("  [progress] parse failed: %v\n", perr)
		}
		w.ok = false
		w.init = true
		return
	}

	pts := make([]ProgressPoint, len(w.refs))
	copy(pts, w.refs)
	doneN := map[string]int{}
	totalN := map[string]int{}
	for i := range pts {
		key := pts[i].Hash
		if pts[i].DoneHash != 0 {
			key = pts[i].DoneHash
		}
		pts[i].Done = entries[key] != 0
		totalN[pts[i].T]++
		if pts[i].Done {
			doneN[pts[i].T]++
		}
	}
	counts := map[string][2]int{}
	for _, t := range botwProgTypes {
		counts[t] = [2]int{doneN[t], totalN[t]}
	}
	// 回忆细分：照片 12 / 最终 1 / 主线自动 / DLC
	var mp, mf, ma, md [2]int
	for _, p := range pts {
		if p.T != "memory" {
			continue
		}
		switch {
		case p.Tier == "photo" && p.PhotoNo != 13:
			mp[1]++
			if p.Done {
				mp[0]++
			}
		case p.Tier == "photo":
			mf[1]++
			if p.Done {
				mf[0]++
			}
		case p.Tier == "auto":
			ma[1]++
			if p.Done {
				ma[0]++
			}
		case p.Tier == "dlc":
			md[1]++
			if p.Done {
				md[0]++
			}
		}
	}
	counts["memory_photo"], counts["memory_final"] = mp, mf
	counts["memory_auto"], counts["memory_dlc"] = ma, md

	gen := time.Now().Format("2006-01-02 15:04:05")
	obj := map[string]any{
		"ok":        true,
		"points":    pts,
		"counts":    counts,
		"generated": gen,
		"save":      best,
		"source":    "botwnavi-eden",
	}
	buf, _ := json.Marshal(obj)
	w.body, w.gen, w.save, w.modT = buf, gen, best, mtime
	w.ok = true
	if !w.init {
		rel := best
		if r := botwSaveRoot(); r != "" && strings.HasPrefix(best, r) {
			rel = strings.TrimPrefix(best, r+string(filepath.Separator))
		}
		fmt.Printf("  [progress] save=%s\n", rel)
		fmt.Printf("  [progress] done: shrines %d/%d, towers %d/%d, koroks %d/%d, memories %d/%d, beasts %d/%d\n",
			doneN["shrine"], totalN["shrine"], doneN["tower"], totalN["tower"], doneN["korok"], totalN["korok"],
			doneN["memory"], totalN["memory"], doneN["beast"], totalN["beast"])
	}
	w.init = true
}

// loop 每 2s 查存档 mtime，变化即重算（游戏内保存后 ~1-2s 生效）
func (w *progressWatcher) loop() {
	for {
		time.Sleep(2 * time.Second)
		latest, mtime := latestBotwSave(findBotwProgressFiles(botwSaveRoot()))
		w.mu.RLock()
		changed := !w.init || latest != w.save || !mtime.Equal(w.modT)
		w.mu.RUnlock()
		if changed {
			w.refresh()
		}
	}
}

// newProgressWatcher 构造并初始化进度监视器（main.go 启动入口）。
func newProgressWatcher() *progressWatcher {
	w := &progressWatcher{}
	w.loadRefs()
	w.refresh()
	return w
}

func (w *progressWatcher) snapshot() (bool, []byte, string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ok, w.body, w.gen
}
