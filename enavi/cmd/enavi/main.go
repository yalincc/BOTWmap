// enavi —— 只读实时大地图伴侣（Eden / 旷野之息）
//
// M1 能力：存档锚点锁定 → 10Hz 跟随 → GET /pos 供 https://botw.yalin.site 出点与朝向箭头。
// 契约与 xnavi / live.js 完全一致（协议 v1），前端零改动：
//
//	GET /pos      → ok/gx/gy/gz/mx/my/heading/verified/source/...
//	GET /progress → 503（M1 未做存档进度，前端会自动降级到静态数据）
//	GET /rescan   → 手动重扫
//	POST /target  /config → 原样接收并回显（导航目标、图层同步）
//
// 红线：只申请 PROCESS_VM_READ，不写游戏内存、不改存档、不注入。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"botwmap/enavi/internal/locate"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/version"
	"botwmap/enavi/internal/winmem"
)

// ---------- 配置 ----------

// Config 只留两个真正需要改的项。
//
// ★ 刻意保持极小（2026-10-20 老大要求）：每一个可调参数都是后期 bug 的来源。
// 采样频率、窗口半径、各类阈值全部写成代码常量，不给外部配置的机会。
type Config struct {
	Port     int    `json:"port"`     // HTTP 端口（前端固定连 8766）
	ProcName string `json:"procName"` // 模拟器进程名
}

func defaultConfig() Config {
	return Config{Port: 8766, ProcName: "eden"}
}

func loadConfig(path string) (Config, string) {
	cfg := defaultConfig()
	if path == "" {
		if exe, err := os.Executable(); err == nil {
			path = filepath.Join(filepath.Dir(exe), "enavi.json")
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, path
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		fmt.Printf("[config] 解析 %s 失败，用默认值: %v\n", path, err)
		return defaultConfig(), path
	}
	if cfg.Port == 0 {
		cfg.Port = 8766
	}
	if cfg.ProcName == "" {
		cfg.ProcName = "eden"
	}
	return cfg, path
}

// ---------- 共享状态 ----------

type shared struct {
	mu      sync.RWMutex
	rescan  func()
	upd     locate.Update
	updated time.Time
	target  any
	cfg     map[string]any
	anchor  scan.Vec3
	addr    uintptr
}

func (s *shared) set(u locate.Update) {
	s.mu.Lock()
	s.upd = u
	s.updated = time.Now()
	s.mu.Unlock()
}

func (s *shared) snapshot() (locate.Update, time.Time, any, map[string]any, scan.Vec3, uintptr) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.upd, s.updated, s.target, s.cfg, s.anchor, s.addr
}

func main() {
	cfgPath := flag.String("config", "", "配置文件（默认 exe 同目录 enavi.json）")
	port := flag.Int("port", 0, "覆盖端口")
	flag.Parse()

	fmt.Println(version.Banner())
	cfg, used := loadConfig(*cfgPath)
	if *port > 0 {
		cfg.Port = *port
	}
	const sampleHz = 10 // 采样频率写死：10Hz 对网页 600ms 轮询足够，且不吃 CPU
	fmt.Printf("[config] %s → port=%d proc=%s（采样 %dHz 固定）\n", used, cfg.Port, cfg.ProcName, sampleHz)

	st := &shared{cfg: map[string]any{}}

	// 单实例锁：孤儿进程占 8766 会让前端连到死服务
	if !acquireSingleInstance("Global\\EnaviM1") {
		fmt.Println("[fatal] 已有一个 Enavi 在运行（或端口被占）。先关掉旧窗口。")
		os.Exit(1)
	}

	// HTTP 先起来：即使还没锁上，前端也能连（ok=false 时前端显示"定位中"）
	go serve(cfg, st)

	// 后台定位循环：Eden 没开/没进游戏就每 3 秒重试
	go func() {
		mux := &sync.Mutex{}
		var tr *locate.Tracker
		var proc *winmem.Process

		var lastPrint time.Time
		ticker := time.NewTicker(time.Second / time.Duration(sampleHz))
		defer ticker.Stop()
		for {
			if tr == nil {
				p := attach(cfg.ProcName)
				if p == nil {
					time.Sleep(3 * time.Second)
					continue
				}
				t, err := locate.New(p)
				if err != nil {
					fmt.Printf("[locate] 锁定失败：%v（3 秒后重试）\n", err)
					p.Close()
					time.Sleep(3 * time.Second)
					continue
				}
				mux.Lock()
				proc, tr = p, t
				mux.Unlock()
				st.mu.Lock()
				st.anchor = t.Anchor()
				tt := t
				st.rescan = func() { tt.Rescan() }
				st.mu.Unlock()
				a := t.Anchor()
				fmt.Printf("[locate] ✓ 已锁定：锚点=(%.1f, %.1f, %.1f)\n", a.X, a.Y, a.Z)
			}

			<-ticker.C
			if !proc.Alive() {
				fmt.Println("[locate] Eden 已退出，重新探测…")
				mux.Lock()
				proc.Close()
				proc, tr = nil, nil
				mux.Unlock()
				st.set(locate.Update{OK: false, Source: "lost"})
				continue
			}
			u := tr.Tick()
			sb := tr.Stats()
			st.mu.Lock()
			st.upd, st.updated, st.addr = u, time.Now(), sb.Addr
			st.mu.Unlock()
			// 每 2 秒打一行（不打 \r：重定向到文件时会连成一条巨行）
			if time.Since(lastPrint) >= 2*time.Second {
				lastPrint = time.Now()
				if u.OK {
					fmt.Printf("[pos] (%.1f, %.1f, %.1f) verified=%v src=%s addr=0x%X scans=%d\n",
						u.Pos.X, u.Pos.Y, u.Pos.Z, u.Verified, u.Source, sb.Addr, sb.Scans)
				} else {
					note := u.Source
					if u.Note != "" {
						note += ": " + u.Note
					}
					fmt.Printf("[pos] -- 定位中（%s）\n", note)
				}
			}
		}
	}()

	waitSignal()
	fmt.Println("\n[exit] 收到退出信号")
}

func attach(procName string) *winmem.Process {
	pids := winmem.FindPIDs(procName)
	if len(pids) == 0 {
		return nil
	}
	p, err := winmem.Open(pids[0])
	if err != nil {
		fmt.Printf("[attach] 打开 %s(pid=%d) 失败：%v\n", procName, pids[0], err)
		return nil
	}
	fmt.Printf("[attach] pid=%d %s\n", pids[0], p.ExePath())
	return p
}

func waitSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}

// ---------- HTTP ----------

func cors(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Cache-Control", "no-store")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	cors(w)
	_ = json.NewEncoder(w).Encode(v)
}

func serve(cfg Config, st *shared) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			cors(w)
			return
		}
		u, at, target, cf, _, _ := st.snapshot()
		age := 0.0
		if !at.IsZero() {
			age = time.Since(at).Seconds()
		}
		payload := map[string]any{
			"ok":       u.OK,
			"game":     "botw",
			"gx":       u.Pos.X,
			"gy":       u.Pos.Y,
			"gz":       u.Pos.Z,
			"mx":       2*float64(u.Pos.X) + 12000,
			"my":       2*float64(u.Pos.Z) + 10000,
			"verified": u.Verified,
			"source":   u.Source,
			"age":      age,
			"layer":    "",
			"target":   target,
			"config":   cf,
			"lanUrl":   nil,
		}
		// 不输出 heading：刻意留空，让前端用位移推算箭头方向。
		// 理由（2026-10-20 老大定）：配合网页地图不需要游戏内那种精度，
		// 前端 live.js 已有位移推算 + 最短弧平滑，实测已匹配好；
		// 硬塞一个内存解算的 yaw 反而可能和位移方向打架。
		if u.Note != "" {
			payload["note"] = u.Note
		}
		writeJSON(w, payload)
	})
	mux.HandleFunc("/progress", func(w http.ResponseWriter, r *http.Request) {
		// M1 未做存档进度：返回 503，前端会自动降级到静态 progress_points.json
		cors(w)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"ok":false}`)
	})
	mux.HandleFunc("/rescan", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		st.mu.RLock()
		fn := st.rescan
		st.mu.RUnlock()
		if fn != nil {
			fn()
		}
		writeJSON(w, map[string]any{"started": fn != nil})
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		var obj map[string]any
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			obj = map[string]any{}
		}
		st.mu.Lock()
		if c, _ := obj["clear"].(bool); c {
			st.target = nil
		} else {
			st.target = obj
		}
		st.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "target": obj})
	})
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodPost {
			var obj map[string]any
			if err := json.NewDecoder(r.Body).Decode(&obj); err == nil {
				st.mu.Lock()
				st.cfg = obj
				st.mu.Unlock()
			}
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "version": version.Version, "pid": os.Getpid()})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	fmt.Printf("[http] 监听 http://%s/pos   （地图里会自动连这个地址）\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("[fatal] HTTP 启动失败：%v（端口被占？）\n", err)
		os.Exit(1)
	}
}

// acquireSingleInstance 命名互斥体单实例锁。
func acquireSingleInstance(name string) bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	create := kernel32.NewProc("CreateMutexW")
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return true
	}
	h, _, e := create.Call(0, 1, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return true
	}
	return e != syscall.Errno(183) // ERROR_ALREADY_EXISTS
}
