// HTTP 层：定位 API + BOTWmap 本地地图托管（手机镜像）+ 缺图引导页。
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
	"sync/atomic"
	"time"
)

// BOTWmap 资产候选位置（开发机回退路径；终端用户优先 exe 同目录 app/，见 exeDirApp）
var botwRoots = []string{
	`E:\WorkSpace\BOTWmap\app`,
	`..\BOTWmap\app`,
	`..\..\BOTWmap\app`,
}

// lanURL 移动端局域网镜像 URL（main 启动时计算；/pos 返回，网页二维码面板读取）
var lanURL string

//go:embed mirror_fallback.html
var mirrorFallbackHTML string

// 内置手机镜像引导页：找不到 app/ 地图时 / 与 /botw/ 返回此页（v1.3.2，对齐 xnavi v3.1.0），
// 手机扫码后至少知道缺什么、怎么补，替代白屏/黑 JSON。
func writeMirrorFallback(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cors(w)
	_, _ = w.Write([]byte(mirrorFallbackHTML))
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

var (
	botwFS   http.Handler
	botwFSMu sync.Mutex
)

// resolveBotwFS 返回本地地图静态处理器；没有就返回 nil（调用方回落到引导页）。
// 惰性解析 + mutex：用户按引导页提示补齐 app/ 后，刷新页面即可生效，不必重启。
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
		if c == "" {
			continue // v1.3.2 加固：空路径经 filepath.Abs 会变成 cwd，误把当前目录挂成地图根
		}
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

// legacyWebDir 自带地图页目录（历史遗留 web/），启动时解析一次；仅作无 app/ 时的兜底。
var legacyWebDir string

// rootHandler v1.3.2：根路径与 /botw/ 统一入口。
//   - 有 app/ 地图：/ 302 跳镜像入口 /botw/?follow=1&game=botw（手机只输 IP:端口 即可达）；
//     /botw/... 直接服务地图静态文件（手机跟随红点 + 导航 + 进度，与电脑同一套页面）。
//   - 无 app/：返回内置引导页（告诉用户缺什么、怎么补）；/ 优先回落历史 web/ 页。
func rootHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	isBotw := path == "/botw" || strings.HasPrefix(path, "/botw/")
	if path == "/" || isBotw {
		fs := resolveBotwFS()
		if fs == nil {
			if path == "/" && legacyWebDir != "" {
				http.FileServer(http.Dir(legacyWebDir)).ServeHTTP(w, r)
				return
			}
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
	if legacyWebDir != "" {
		http.FileServer(http.Dir(legacyWebDir)).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func cors(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Access-Control-Allow-Private-Network", "true")
	h.Set("Cache-Control", "no-store")
}

func jsonOut(w http.ResponseWriter, v interface{}) {
	cors(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// jsonOutRaw 直接吐预序列化好的 JSON（进度数据大，避免二次编解码）
func jsonOutRaw(w http.ResponseWriter, body []byte) {
	cors(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/pos", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		st.mu.Lock()
		var t *Target
		if st.Target != nil {
			cp := *st.Target
			t = &cp
		}
		resp := map[string]interface{}{
			"ok":          st.OK,
			"verified":    st.Verified,
			"gx":          st.GX,
			"gy":          st.GY,
			"gz":          st.GZ,
			"mx":          st.MX,
			"my":          st.MY,
			"target":      t,
			"source":      st.Source,
			"progressGen": "",
			"lanUrl":      lanURL,
		}
		st.mu.Unlock()
		jsonOut(w, resp)
	})

	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		var in struct {
			Clear bool    `json:"clear"`
			Name  string  `json:"name"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Type  string  `json:"type"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Clear {
			st.Target = nil
			fmt.Println("  [target] 已清除目标")
		} else {
			st.Target = &Target{Name: in.Name, X: in.X, Y: in.Y, Type: in.Type}
			name := in.Name
			if name == "" {
				name = "(未命名)"
			}
			fmt.Printf("  [target] %s (%s) map(%.0f,%.0f)\n", name, in.Type, in.X, in.Y)
		}
		jsonOut(w, map[string]interface{}{"ok": true})
	})

	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		jsonOut(w, map[string]interface{}{"ok": true})
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		st.mu.Lock()
		d := st.Diag
		d.Version = buildVer
		d.UptimeSec = int64(time.Since(bootAt).Seconds())
		st.mu.Unlock()
		jsonOut(w, map[string]interface{}{
			"ok": st.OK, "source": st.Source,
			"diag": d,
		})
	})

	mux.HandleFunc("/rescan", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		atomic.StoreInt32(&rescanReq, 1)
		jsonOut(w, map[string]interface{}{"ok": true, "msg": "rescan scheduled"})
	})

	// 收集进度：Cemu 存档解析（无存档时返回空集合，前端静默降级）
	mux.HandleFunc("/progress", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		ok, body, _ := progress.snapshot()
		if !ok || len(body) == 0 {
			jsonOut(w, map[string]interface{}{
				"ok":        false,
				"points":    []interface{}{},
				"counts":    nil,
				"generated": "",
			})
			return
		}
		jsonOutRaw(w, body)
	})

	// v1.3.2：地图资产 + 根路径统一入口（有 app/ → 302 跳镜像；无 → 引导页）
	legacyWebDir = locateWebDir()
	if legacyWebDir != "" {
		fmt.Printf("  [web]   legacy map page dir: %s\n", legacyWebDir)
	}
	resolveBotwFS() // 预热：启动即挂载则打印日志；未挂载每次请求都会重试
	mux.HandleFunc("/", rootHandler)
	return mux
}

// locateWebDir 定位自带地图页：优先 exe 同级 web/，再源码目录
func locateWebDir() string {
	cands := []string{}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "web"))
	}
	cands = append(cands, "web", "cemunavi/web", `E:\WorkSpace\GameTools\live-cemu\cemunavi\web`)
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}
