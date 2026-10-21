// Package known 缓存"已验证过的玩家坐标地址"，让下次启动能秒锁，不必每次重扫。
//
// 为什么要有它：扫描是 bug 的主要来源（窗口选错、锁到副本、频繁重扫）。
// 而坐标槽在「同一次 Eden 会话内」是稳定的 —— 只要缓存命中就直接读，
// 压根不进扫描逻辑，故障面立刻缩小到"读一个地址"。
//
// 缓存失效的安全阀：
//   - Eden 进程重启 → 地址必然失效（ASLR）→ 文件名里带 pid 与进程启动时间
//   - 场景切换（传送/进庙）→ 槽可能漂移 → 由 locate 的失效判定兜底重扫
package known

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry 一条缓存记录。
type Entry struct {
	Addr        uintptr `json:"addr"`        // 玩家坐标槽的宿主地址
	PID         uint32  `json:"pid"`         // Eden 进程 ID
	ProcStarted int64   `json:"procStarted"` // Eden 启动时间（Unix 秒），用于识别"同一个会话"
	Pos         [3]float32 `json:"pos"`      // 写入时的坐标
	Verified    bool    `json:"verified"`
	SavedAt     int64   `json:"savedAt"`
	EdenVersion string  `json:"edenVersion"`
	Note        string  `json:"note"`
}

// Store 缓存文件（最多保留几条，按 SavedAt 新的优先）。
type Store struct {
	Entries []Entry `json:"entries"`
}

const (
	fileName    = "known_player.json"
	maxEntries  = 6
	// 同会话有效期：超过就只当"参考"不当"真相"（防止会话内长时间后槽漂移）
	softTTL = 6 * time.Hour
)

// path 缓存文件放在 exe 同目录。
func path() string {
	// 优先用「当前工作目录」——
	// `go run` 时 exe 在临时目录里，缓存写到那里就丢了；
	// 而生产是 build/enavi.exe 从 enavi/ 目录启动，工作目录才是稳定位置。
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, fileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// 还没建过 → 记下工作目录，等会儿写的时候用同一个
		wdPath = p
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return fileName
	}
	return filepath.Join(filepath.Dir(exe), fileName)
}

// wdPath 工作目录下的缓存路径（首次调用 path() 时确定）。
var wdPath string

// Load 读缓存（文件不存在返回空 Store，不报错）。
func Load() Store {
	var s Store
	b, err := os.ReadFile(path())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

// Save 追加/更新一条并落盘。
func (s *Store) Save(e Entry) error {
	e.SavedAt = time.Now().Unix()
	out := []Entry{e}
	for _, old := range s.Entries {
		if old.Addr == e.Addr && old.PID == e.PID {
			continue // 同一条，替换
		}
		out = append(out, old)
	}
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	s.Entries = out
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), b, 0o644)
}

// Pick 挑出「与当前 Eden 会话匹配」的最新一条。
// procStarted = 0 表示无法判定会话，此时只按 pid 匹配。
func (s Store) Pick(pid uint32, procStarted int64) (Entry, bool) {
	for _, e := range s.Entries { // 已按 SavedAt 降序
		if e.PID != pid {
			continue
		}
		if procStarted != 0 && e.ProcStarted != 0 && e.ProcStarted != procStarted {
			continue // 不同会话（进程重启过），地址不可信
		}
		if !e.Verified {
			continue // 未经验证（走动确认）的地址不予复用，防止错误地址被秒锁固化
		}
		if time.Since(time.Unix(e.SavedAt, 0)) > softTTL {
			continue
		}
		return e, true
	}
	return Entry{}, false
}

// Clear 清空缓存（GUI/CLI 的"重新定位"用）。
func (s *Store) Clear() error {
	s.Entries = nil
	return os.WriteFile(path(), []byte(`{"entries":[]}`), 0o644)
}

// Path 暴露文件路径便于日志与排查。
func Path() string { return path() }
