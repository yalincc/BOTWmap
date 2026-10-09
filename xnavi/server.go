// HTTP API：供在线地图页（https://botw.yalin.site/）跨域连接。
// 端点：GET /pos、GET/POST /target、POST /config、GET /rescan、GET /progress。
// 所有响应带 CORS 头 + Private Network Access 预检头（Chrome 对
// "公网 HTTPS 页面 → http://127.0.0.1" 的请求强制要求）。
// /rescan 只向状态机发信号（ds 文档 4.8），不自行开 goroutine 扫描。

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// lanURL 移动端局域网镜像 URL（main 启动时计算；/pos 返回，网页二维码面板读取）
var lanURL string

type server struct{}

// BOTWmap 资产候选位置（v3.0.2 局域网镜像：存在即接管 /botw/，缺失不影响核心导航）
// v3.1.0 排序（重要）：
//   1. exe 同目录 app/ —— 发布形态，引导页就是这么教用户的，必须最优先
//   2. cwd 相对 app/   —— 开发/命令行形态兜底
//   3~4. 上级目录 / 开发机绝对路径 —— 历史遗留，保留兼容
//
// 注意：候选 1 必须基于 os.Executable() 而不是进程 cwd——从快捷方式或任意目录启动时
// cwd ≠ exe 目录，只看 cwd 会出现"用户明明解压到 exe 同目录却仍找不到"的翻车。
var botwRoots = []string{
	`app`,
	`E:\WorkSpace\BOTWmap\app`,
	`..\BOTWmap\app`,
	`..\..\BOTWmap\app`,
}

// botwFS BOTWmap 静态文件处理器（StripPrefix "/botw"：/botw → 首页，相对路径资源照常）
// 惰性解析：见 resolveBotwFS（用户解压完 app/ 后刷新即生效，无需重启 xnavi）
var (
	botwFS   http.Handler
	botwFSMu sync.Mutex
)

//go:embed mirror_fallback.html
var mirrorFallbackHTML string

// 内置手机镜像引导页：exe 旁无 app/ 时 / 与 /botw/ 返回此页（v3.1.0），
// 替代兜底 unknown endpoint 黑 JSON（终端用户手机扫码后至少知道缺什么、怎么补）。
// 编译进 exe，不依赖任何外部文件。
func writeMirrorFallback(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	corsHeaders(w)
	w.Write([]byte(mirrorFallbackHTML))
}

// exeDirApp 返回 exe 所在目录下的 app/ 路径（引导页承诺的位置）。
func exeDirApp() string {
	if exe, err := os.Executable(); err == nil {
		if real, err2 := filepath.EvalSymlinks(exe); err2 == nil {
			exe = real
		}
		return filepath.Join(filepath.Dir(exe), "app")
	}
	return ""
}

// resolveBotwFS 返回本地地图静态处理器；没有就返回 nil（调用方回落到引导页）。
// 惰性解析 + mutex：用户按引导页提示解压 app/ 后，刷新页面即可生效，不必重启 xnavi。
// 已挂载成功后不再重复 stat（botwFS != nil 直接返回）。
func resolveBotwFS() http.Handler {
	botwFSMu.Lock()
	defer botwFSMu.Unlock()
	if botwFS != nil {
		return botwFS
	}
	cands := make([]string, 0, len(botwRoots)+1)
	if p := exeDirApp(); p != "" {
		cands = append(cands, p) // 最高优先：exe 同目录
	}
	cands = append(cands, botwRoots...)
	for _, c := range cands {
		if abs, err := filepath.Abs(c); err == nil {
			c = abs
		}
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			botwFS = http.StripPrefix("/botw", http.FileServer(http.Dir(c)))
			fmt.Printf("  [asset] BOTWmap assets mounted: %s\n", c)
			return botwFS
		}
	}
	return nil
}

func init() {
	// 预热：启动时挂载则打印日志；未挂载不打印（每次请求都会重试，不吵启动日志）
	resolveBotwFS()
}

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

	// 根路径（手机用户最自然的动作是只输 IP:端口）→ 有 app/ 302 跳镜像入口；
	// 无 app/ 直接返回内置引导页（v3.1.0，替代 unknown endpoint）
	isBotwPath := path == "/botw" || strings.HasPrefix(path, "/botw/")
	if path == "/" || isBotwPath {
		fs := resolveBotwFS()
		if fs == nil {
			writeMirrorFallback(w)
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
			requestRescan() // 只发信号，状态机下一个 tick 处理
			writeJSON(w, map[string]any{"started": true})
		} else {
			writeJSON(w, map[string]any{"ok": false})
		}
	case "/progress":
		s.handleProgress(w, r)
	default:
		writeJSON(w, map[string]any{"ok": false, "error": "unknown endpoint"})
	}
}

func (s *server) handlePos(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	payload := map[string]any{
		"ok":       state.ok,
		"game":     currentGame.Name(),
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
	if progress != nil {
		_, _, gen := progressFor().snapshot()
		payload["progressGen"] = gen
	}
	payload["lanUrl"] = lanURL
	writeJSON(w, payload)
}

// handleProgress 返回当前游戏的收集进度：
//   - BOTW（Cemu/Ryujinx）：done_hash + 神庙坐标点表（progress_points.json 口径）
//
// 参考表缺失或尚无存档时返回 503，前端据此降级到本地/静态路径。
// 存档隔离：watcher 只读自家存档文件名（game_data.sav）。
func (s *server) handleProgress(w http.ResponseWriter, r *http.Request) {
	ok, body, _ := progressFor().snapshot()
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
