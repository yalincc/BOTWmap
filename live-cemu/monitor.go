// 运行监督：事件日志（run-events.log）+ 计数器 + 状态文件（status.json）。
// 与 BotwNavi(live-go) monitor.go 同构。

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	cntLocks     atomic.Int64
	cntRelocates atomic.Int64
	cntJumps     atomic.Int64
	cntScans     atomic.Int64
)

var (
	evMu   sync.Mutex
	evFile *os.File
)

const evLogMax = 2 << 20

func evLogPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "run-events.log")
}

func openEvLog() {
	if evFile != nil {
		return
	}
	f, err := os.OpenFile(evLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	if fi, err := f.Stat(); err == nil && fi.Size() > evLogMax {
		f.Close()
		os.Rename(evLogPath(), evLogPath()+".1")
		if f2, err := os.OpenFile(evLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			f = f2
		} else {
			return
		}
	}
	evFile = f
}

// eLog 事件行：控制台原样输出，文件带时间戳。
func eLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Print(msg)
	evMu.Lock()
	defer evMu.Unlock()
	openEvLog()
	if evFile != nil {
		evFile.WriteString(time.Now().Format("2006-01-02 15:04:05.000") + " " + msg)
	}
}

func incCounter(name string) {
	switch name {
	case "locks":
		cntLocks.Add(1)
	case "relocates":
		cntRelocates.Add(1)
	case "jumps":
		cntJumps.Add(1)
	case "scans":
		cntScans.Add(1)
	}
}

func statusPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "status.json")
}

// statusLoop 每 2s 写一次会话健康快照（原子替换）。
func statusLoop() {
	for {
		time.Sleep(2 * time.Second)
		lock.mu.RLock()
		addr := lock.addr
		src := lock.source
		verified := lock.verified
		copies := lock.copies
		lock.mu.RUnlock()

		state.mu.RLock()
		ok := state.ok
		gx, gy, gz := state.gx, state.gy, state.gz
		mx, my := state.mx, state.my
		age := state.age
		state.mu.RUnlock()

		ageSec := 0.0
		if age > 0 {
			ageSec = float64(time.Now().UnixNano())/1e9 - age
		}
		obj := map[string]any{
			"time":     time.Now().Format(time.RFC3339),
			"pid":      procPID,
			"ok":       ok,
			"pos":      map[string]any{"gx": gx, "gy": gy, "gz": gz, "mx": mx, "my": my},
			"source":   src,
			"verified": verified,
			"copies":   copies,
			"lockAddr": fmt.Sprintf("0x%X", addr),
			"ageSec":   ageSec,
			"counters": map[string]any{
				"locks":     cntLocks.Load(),
				"relocates": cntRelocates.Load(),
				"jumps":     cntJumps.Load(),
				"scans":     cntScans.Load(),
			},
		}
		buf, err := json.MarshalIndent(obj, "", " ")
		if err != nil {
			continue
		}
		tmp := statusPath() + ".tmp"
		if os.WriteFile(tmp, buf, 0644) == nil {
			os.Rename(tmp, statusPath())
		}
	}
}
