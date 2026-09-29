// progress_cemu.go — 收集进度同步（Cemu 存档 → /progress）
//
// 移植自 BotwNavi live-go/progress.go，差异：
//   - 存档源：Cemu（Wii U）game_data.sav，字节序自适应，主档按 mtime 最新
//   - 参考表 progress_points.json 通过 go:embed 内嵌 → exe 仍单文件分发
//   - 完成判定：done_hash 优先（与 BotwNavi 一致；shrine 通关看 done_hash）

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

//go:embed data/progress_points.json
var progressRefsFS embed.FS

// ProgressPoint 与 progress_points.json 单条参考点同构（前端 live.js 直接消费）。
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

var progress *progressWatcher

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

func newProgressWatcher() *progressWatcher {
	w := &progressWatcher{}
	w.loadRefs()
	w.refresh()
	return w
}

func (w *progressWatcher) loadRefs() {
	w.mu.Lock()
	defer w.mu.Unlock()
	buf, err := progressRefsFS.ReadFile("data/progress_points.json")
	if err != nil {
		fmt.Printf("  [progress] embedded reference table unreadable: %v\n", err)
		w.ok = false
		return
	}
	var obj struct {
		Points []ProgressPoint `json:"points"`
	}
	if json.Unmarshal(buf, &obj) != nil || len(obj.Points) == 0 {
		fmt.Println("  [progress] embedded reference table invalid")
		w.ok = false
		return
	}
	w.refs = obj.Points
	w.ok = true
	var n [5]int
	for _, p := range w.refs {
		n[progIdx(p.T)]++
	}
	fmt.Printf("  [progress] reference table: %d points (shrine %d, tower %d, korok %d, memory %d, beast %d)\n",
		len(w.refs), n[0], n[1], n[2], n[3], n[4])
}

// refresh 解析最新存档（按 mtime）→ 按参考表 done_hash 现算 done → 缓存 JSON。
func (w *progressWatcher) refresh() {
	w.mu.Lock()
	defer w.mu.Unlock()

	files := findCemuSaveFiles()
	best, err := latestCemuSave(files)
	if err != nil {
		if !w.init {
			fmt.Println("  [progress] no Cemu save yet - will watch for it")
		}
		w.ok = false
		w.init = true
		return
	}
	entries, perr := parseCemuSave(best)
	if perr != nil {
		if !w.init {
			fmt.Printf("  [progress] parse save failed: %v\n", perr)
		}
		w.ok = false
		w.init = true
		return
	}

	pts := make([]ProgressPoint, len(w.refs))
	copy(pts, w.refs)
	var doneN, totalN [5]int
	for i := range pts {
		// 参考表若带历史状态（旧快照的 done/entered），一律以当前存档为准：
		// done 现算，entered 显式清零（存档无进入判定字段，不再外传旧快照残留）
		pts[i].Entered = false
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
	// 回忆拆档：照片12（不含最终）/ 最终1（照片13）/ 主线自动5 / DLC EX5
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
		case p.Tier == "photo" && p.PhotoNo == 13:
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
	counts["memory_photo"] = mp
	counts["memory_final"] = mf
	counts["memory_auto"] = ma
	counts["memory_dlc"] = md

	gen := time.Now().Format("2006-01-02 15:04:05")
	obj := map[string]any{
		"ok":        true,
		"points":    pts,
		"counts":    counts,
		"generated": gen,
		"source":    "live-cemu",
	}
	buf, _ := json.Marshal(obj)
	w.body = buf
	w.gen = gen
	w.save = best
	if fi, err := os.Stat(best); err == nil {
		w.modT = fi.ModTime()
	}
	w.ok = true
	if !w.init {
		fmt.Printf("  [progress] save=%s\n", saveDirLabel(best))
		fmt.Printf("  [progress] done: shrines %d/%d, towers %d/%d, koroks %d/%d, memories %d/%d, beasts %d/%d\n",
			doneN[0], totalN[0], doneN[1], totalN[1], doneN[2], totalN[2], doneN[3], totalN[3], doneN[4], totalN[4])
	}
	w.init = true
}

// loop 每 2s 检查最新存档 mtime 变化 → 变化则重算（游戏内保存后 ~1-2s 内 /progress 更新）。
func (w *progressWatcher) loop() {
	for {
		time.Sleep(2 * time.Second)
		latest, mtime := "", time.Time{}
		for _, p := range findCemuSaveFiles() {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(mtime) {
				latest, mtime = p, fi.ModTime()
			}
		}
		w.mu.Lock()
		changed := !w.init || latest != w.save || !mtime.Equal(w.modT)
		if latest != "" {
			w.save, w.modT = latest, mtime
		}
		w.mu.Unlock()
		if changed {
			w.refresh()
		}
	}
}

// snapshot 返回 (可用, 最新 JSON body, 进度代次)。
func (w *progressWatcher) snapshot() (bool, []byte, string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ok, w.body, w.gen
}
