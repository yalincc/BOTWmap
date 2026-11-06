// server.go — HTTP API 与本地网页托管。
//
// 契约与 xnavi 完全一致，网页 app/ 无需改动：
//   GET  /pos        玩家位置 + 目标 + 配置（600ms 轮询）
//   POST /target     设置 / 清除导航目标
//   POST /config     同步图层勾选
//   GET  /rescan     触发重新定位
//   GET  /progress   收集进度（points 口径）
//   GET  /save       直传 game_data.sav 二进制
//   GET  /status     内嵌极简状态页
//   /botw/*         本地地图静态托管（手机镜像）
//   /               302 到镜像入口，或返回缺文件引导页
//
// ★ 兼容铁律：/pos 的 app 字段必须是 "xnavi"。网页 live.js 的端口发现
//   （probePort）只认这个指纹，改名会导致「新核心 + 老网页」直接失联。

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed status.html mirror_fallback.html
var pageFS embed.FS

type targetT struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Type string  `json:"type"`
	Ts   float64 `json:"ts"`
}

type configT struct {
	Cats       []string `json:"cats"`
	DoneFilter string   `json:"doneFilter"`
	ShowAreas  bool     `json:"showAreas"`
}

var (
	targetMu  sync.Mutex
	curTarget *targetT
	configMu  sync.Mutex
	cfg       = configT{
		Cats:       []string{"神庙", "希卡塔", "克洛格果实"},
		DoneFilter: "all",
		ShowAreas:  true,
	}
)

var lanURL string

// ---- 静态地图托管（惰性解析：用户后放 app/ 刷新即生效） ----
var (
	botwFSMu sync.Mutex
	botwFS   http.Handler
)

var appCandidates = []string{
	`app`,
	`..\app`,
	`..\BOTWmap\app`,
	`E:\WorkSpace\BOTWmap\app`,
}

func exeDirApp() string {
	if exe, err := os.Executable(); err == nil {
		if real, err2 := filepath.EvalSymlinks(exe); err2 == nil {
			exe = real
		}
		return filepath.Join(filepath.Dir(exe), "app")
	}
	return ""
}

func resolveBotwFS() http.Handler {
	botwFSMu.Lock()
	defer botwFSMu.Unlock()
	if botwFS != nil {
		return botwFS
	}
	cands := []string{}
	if p := exeDirApp(); p != "" {
		cands = append(cands, p)
	}
	for _, c := range appCandidates {
		if abs, err := filepath.Abs(c); err == nil {
			c = abs
		}
		cands = append(cands, c)
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			botwFS = http.StripPrefix("/botw", http.FileServer(http.Dir(c)))
			fmt.Printf("  [asset] 本地地图已挂载: %s\n", c)
			return botwFS
		}
	}
	return nil
}

func corsHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Private-Network", "true")
	h.Set("Cache-Control", "no-store")
}

func writeJSON(w http.ResponseWriter, obj any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	corsHeaders(w)
	buf, _ := json.Marshal(obj)
	w.Write(buf)
}

func writeFallback(w http.ResponseWriter) {
	buf, _ := pageFS.ReadFile("mirror_fallback.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	corsHeaders(w)
	w.Write(buf)
}

type apiServer struct{}

func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	if r.Method == http.MethodOptions {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Private-Network", "true")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		h.Set("Access-Control-Expose-Headers", "X-Save-Path")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if path == "/" || path == "/botw" || strings.HasPrefix(path, "/botw/") {
		fs := resolveBotwFS()
		if fs == nil {
			writeFallback(w)
			return
		}
		if path == "/" {
			http.Redirect(w, r, "/botw/?follow=1&game=botw", http.StatusFound)
			return
		}
		fs.ServeHTTP(w, r)
		return
	}

	switch path {
	case "/pos":
		s.handlePos(w)
	case "/status", "/gui":
		buf, _ := pageFS.ReadFile("status.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		corsHeaders(w)
		w.Write(buf)
	case "/target":
		if r.Method == http.MethodPost {
			s.handleTargetPost(w, r)
		} else {
			writeJSON(w, map[string]any{"ok": false})
		}
	case "/config":
		if r.Method == http.MethodPost {
			s.handleConfigPost(w, r)
		} else {
			configMu.Lock()
			c := cfg
			configMu.Unlock()
			writeJSON(w, c)
		}
	case "/rescan":
		rescanReq.Store(true)
		writeJSON(w, map[string]any{"started": true})
	case "/progress":
		ok, body, _ := saves.snapshot()
		if !ok || len(body) == 0 {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			corsHeaders(w)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ok":false}`))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		corsHeaders(w)
		w.Write(body)
	case "/save":
		s.handleSave(w)
	case "/open-save":
		p := currentSavePath()
		if p == "" {
			writeJSON(w, map[string]any{"ok": false, "error": "暂无生效的存档"})
			return
		}
		if err := openSaveLocation(p); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "path": p})
	case "/save-override":
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]any{"ok": false})
			return
		}
		var req struct {
			Path  string `json:"path"`
			Clear bool   `json:"clear"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Clear || req.Path == "" {
			setSaveDirOverride("")
			writeJSON(w, map[string]any{"ok": true, "cleared": true})
		} else {
			setSaveDirOverride(req.Path)
			writeJSON(w, map[string]any{"ok": true, "path": req.Path})
		}
		go saves.refresh()
	default:
		writeJSON(w, map[string]any{"ok": false, "error": "unknown endpoint"})
	}
}

func (s *apiServer) handlePos(w http.ResponseWriter) {
	st := loc.snapshot()
	targetMu.Lock()
	t := curTarget
	targetMu.Unlock()
	configMu.Lock()
	c := cfg
	configMu.Unlock()

	payload := map[string]any{
		"ok":       st.OK,
		"app":      "xnavi", // ★ 网页端口发现指纹，勿改
		"ver":      CoreVersion,
		"game":     "botw",
		"gx":       st.GX,
		"gy":       st.GY,
		"gz":       st.GZ,
		"mx":       st.MX,
		"my":       st.MY,
		"layer":    0,
		"age":      st.Age,
		"verified": st.Verified,
		"copies":   1,
		"source":   st.Source,
		"target":   t,
		"config":   c,
		"lanUrl":   lanURL,
	}
	if ok, _, gen := saves.snapshot(); ok {
		payload["progressGen"] = gen
		sum := saves.summarize()
		payload["save"] = sum["save"]
		payload["saveSource"] = sum["saveSource"]
	}
	writeJSON(w, payload)
}

func (s *apiServer) handleSave(w http.ResponseWriter) {
	corsHeaders(w)
	p := currentSavePath()
	if p == "" {
		http.Error(w, "no active save found", http.StatusNotFound)
		return
	}
	data, err := readFileShared(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="game_data.sav"`)
	w.Header().Set("X-Save-Path", p)
	w.Header().Set("Access-Control-Expose-Headers", "X-Save-Path")
	w.Write(data)
}

func (s *apiServer) handleTargetPost(w http.ResponseWriter, r *http.Request) {
	var obj map[string]any
	if json.NewDecoder(r.Body).Decode(&obj) != nil {
		obj = map[string]any{}
	}
	if clear, _ := obj["clear"].(bool); clear {
		targetMu.Lock()
		curTarget = nil
		targetMu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "target": nil})
		return
	}
	str := func(v any) string {
		s, _ := v.(string)
		return s
	}
	flt := func(v any) float64 {
		f, _ := v.(float64)
		return f
	}
	t := &targetT{
		Name: str(obj["name"]),
		X:    flt(obj["x"]),
		Y:    flt(obj["y"]),
		Type: str(obj["type"]),
		Ts:   float64(time.Now().UnixNano()) / 1e9,
	}
	targetMu.Lock()
	curTarget = t
	targetMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "target": t})
}

func (s *apiServer) handleConfigPost(w http.ResponseWriter, r *http.Request) {
	var obj map[string]any
	if json.NewDecoder(r.Body).Decode(&obj) != nil {
		obj = map[string]any{}
	}
	configMu.Lock()
	if v, ok := obj["cats"].([]any); ok {
		var cats []string
		for _, c := range v {
			if str, ok := c.(string); ok {
				cats = append(cats, str)
			}
		}
		if cats != nil {
			cfg.Cats = cats
		}
	}
	if v, ok := obj["doneFilter"].(string); ok && v != "" {
		cfg.DoneFilter = v
	}
	if v, ok := obj["showAreas"].(bool); ok {
		cfg.ShowAreas = v
	}
	c := cfg
	configMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "config": c})
}
