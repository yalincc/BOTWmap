// progress.go — 收集进度同步（存档热重建 → /progress）
//
// 与旧 Python 版「存档变更热重建进度」同机制，服务端实时计算：
//   - 参考表 data/progress_points.json（1068 条静态参考点：t/hash/done_hash/坐标/名，
//     由 live/build_progress.py 构建；不含个人进度，done 一律由本服务按当前存档现算）
//   - 每 2s 扫 Ryujinx 存档目录，主档（最高 playtime 槽）文件 mtime 变化 → 重新解析
//     [hash u32][value u32] 扁平表 → 逐条计算 done → 缓存 JSON
//   - GET /progress 返回与 Python progress_points.json 同构数据（points/counts/generated）
//   - /pos 附带 progressGen，前端检测到变化即重新拉取（游戏内保存后 ~1-2s 内更新）

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProgressPoint 与 build_progress.py 输出的单条参考点同构。
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
}

type progressWatcher struct {
	mu      sync.RWMutex
	ok      bool
	refs    []ProgressPoint
	body    []byte
	gen     string
	refPath string
	init    bool
	save    string
	modT    time.Time
}

var progress *progressWatcher

func progressDefaultRefPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data", "progress_points.json")
}

func newProgressWatcher(refPath string) *progressWatcher {
	w := &progressWatcher{refPath: refPath}
	w.loadRefs()
	w.refresh()
	return w
}

func (w *progressWatcher) loadRefs() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refs = nil
	path := w.refPath
	if path == "" {
		path = progressDefaultRefPath()
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if !w.init {
			fmt.Printf("  [progress] reference table not found: %s\n", path)
			fmt.Println("  (collect-progress sync disabled; put data/progress_points.json next to the exe to enable)")
		}
		w.ok = false
		return
	}
	var obj struct {
		Points []ProgressPoint `json:"points"`
	}
	if json.Unmarshal(buf, &obj) != nil || len(obj.Points) == 0 {
		if !w.init {
			fmt.Printf("  [progress] reference table unreadable: %s\n", path)
		}
		w.ok = false
		return
	}
	w.refs = obj.Points
	w.ok = true
	if !w.init {
		var n [5]int
		for _, p := range w.refs {
			n[progIdx(p.T)]++
		}
		fmt.Printf("  [progress] reference table: %d points (shrine %d, tower %d, korok %d, memory %d, beast %d)\n",
			len(w.refs), n[0], n[1], n[2], n[3], n[4])
	}
}

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

var progTypes = []string{"shrine", "tower", "korok", "memory", "beast"}

// refresh 解析当前主档（最高 playtime）→ 按参考表现算 done → 构建缓存 JSON。
func (w *progressWatcher) refresh() {
	w.mu.Lock()
	defer w.mu.Unlock()

	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		if !w.init {
			fmt.Println("  [progress] no usable save file yet - will watch for it")
		}
		w.ok = false
		w.init = true
		return
	}
	primary := anchors[0]
	entries, _ := parseSave(primary.Path)

	pts := make([]ProgressPoint, len(w.refs))
	copy(pts, w.refs)
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
	gen := time.Now().Format("2006-01-02 15:04:05")
	obj := map[string]any{
		"ok":        true,
		"points":    pts,
		"counts":    counts,
		"generated": gen,
		"source":    "live-go",
	}
	buf, _ := json.Marshal(obj)
	w.body = buf
	w.gen = gen
	w.ok = true
	if !w.init {
		fmt.Printf("  [progress] save=%s\n", primary.Path)
		fmt.Printf("  [progress] done: shrines %d/%d, towers %d/%d, koroks %d/%d, memories %d/%d, beasts %d/%d\n",
			doneN[0], totalN[0], doneN[1], totalN[1], doneN[2], totalN[2], doneN[3], totalN[3], doneN[4], totalN[4])
	}
	w.init = true
}

// loop 每 2s 检查存档目录是否有变化（路径或 mtime），变化则重算。
func (w *progressWatcher) loop() {
	for {
		time.Sleep(2 * time.Second)
		latest, mtime := "", time.Time{}
		for _, p := range findSaveFiles(saveRoot()) {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(mtime) {
				latest, mtime = p, fi.ModTime()
			}
		}
		w.mu.Lock()
		changed := !w.init || latest != w.save || !mtime.Equal(w.modT)
		w.save, w.modT = latest, mtime
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
