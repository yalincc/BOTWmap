// HTTP API：供在线地图页（https://botw.yalin.site/）跨域连接。
// 端点：GET /pos、GET/POST /target、POST /config、GET /rescan。
// 所有响应带 CORS 头 + Private Network Access 预检头（Chrome 对
// "公网 HTTPS 页面 → http://127.0.0.1" 的请求强制要求）。

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type targetT struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Type string  `json:"type"`
	Ts   float64 `json:"ts"`
}

var (
	targetMu sync.Mutex
	target   *targetT
)

type configT struct {
	Cats       []string `json:"cats"`
	DoneFilter string   `json:"doneFilter"`
	ShowAreas  bool     `json:"showAreas"`
}

var (
	configMu sync.Mutex
	cfg      = defaultConfig()
)

func defaultConfig() configT {
	return configT{Cats: []string{"神庙", "希卡塔", "克洛格果实"}, DoneFilter: "all", ShowAreas: true}
}

func configPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data", "map_config.json")
}

func loadConfig() {
	configMu.Lock()
	defer configMu.Unlock()
	cfg = defaultConfig()
	if buf, err := os.ReadFile(configPath()); err == nil {
		var c configT
		if json.Unmarshal(buf, &c) == nil {
			merged := defaultConfig()
			if c.Cats != nil {
				merged.Cats = c.Cats
			}
			if c.DoneFilter != "" {
				merged.DoneFilter = c.DoneFilter
			}
			merged.ShowAreas = c.ShowAreas
			cfg = merged
		}
	}
}

func saveConfig() {
	configMu.Lock()
	c := cfg
	configMu.Unlock()
	os.MkdirAll(filepath.Dir(configPath()), 0755)
	buf, _ := json.MarshalIndent(c, "", " ")
	os.WriteFile(configPath(), buf, 0644)
}

// corsHeaders 写入跨域 + PNA 头。
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

type server struct{}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	switch path {
	case "/pos":
		s.handlePos(w, r)
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
		if r.Method == http.MethodGet {
			go relocalize("/rescan requested")
			writeJSON(w, map[string]any{"started": true})
		} else {
			writeJSON(w, map[string]any{"ok": false})
		}
	default:
		writeJSON(w, map[string]any{"ok": false, "error": "unknown endpoint"})
	}
}

func (s *server) handlePos(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	payload := map[string]any{
		"ok":       state.ok,
		"gx":       state.gx,
		"gy":       state.gy,
		"gz":       state.gz,
		"mx":       state.mx,
		"my":       state.my,
		"layer":    state.layer,
		"age":      state.age,
		"verified": state.verified,
		"copies":   state.copies,
		"source":   state.source,
	}
	state.mu.RUnlock()
	targetMu.Lock()
	payload["target"] = target
	targetMu.Unlock()
	configMu.Lock()
	payload["config"] = cfg
	configMu.Unlock()
	writeJSON(w, payload)
}

func (s *server) handleTargetPost(w http.ResponseWriter, r *http.Request) {
	var obj map[string]any
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		obj = map[string]any{}
	}
	if clear, _ := obj["clear"].(bool); clear {
		clearTarget()
		writeJSON(w, map[string]any{"ok": true, "target": nil})
		return
	}
	t := &targetT{
		Name: str(obj["name"]),
		X:    flt(obj["x"]),
		Y:    flt(obj["y"]),
		Type: str(obj["type"]),
		Ts:   float64(time.Now().UnixNano()) / 1e9,
	}
	setTarget(t)
	writeJSON(w, map[string]any{"ok": true, "target": t})
}

func (s *server) handleConfigPost(w http.ResponseWriter, r *http.Request) {
	var obj map[string]any
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		obj = map[string]any{}
	}
	configMu.Lock()
	if v, ok := obj["cats"].([]any); ok {
		var cats []string
		for _, c := range v {
			if s, ok := c.(string); ok {
				cats = append(cats, s)
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
	saveConfig()
	fmt.Printf("  [config] cats=%d doneFilter=%s showAreas=%v\n", len(c.Cats), c.DoneFilter, c.ShowAreas)
	writeJSON(w, map[string]any{"ok": true, "config": c})
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func flt(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}
