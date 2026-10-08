// HTTP 层：定位 API + 自带地图页静态托管 + BOTWmap 资产代理。
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// BOTWmap 资产候选位置（存在即自动接管，缺失不影响核心导航）
var botwRoots = []string{
	`E:\WorkSpace\BOTWmap\app`,
	`..\BOTWmap\app`,
	`..\..\BOTWmap\app`,
}

// lanURL 移动端局域网镜像 URL（main 启动时计算；/pos 返回，网页二维码面板读取）
var lanURL string

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

	// BOTWmap 资产代理（存在才注册）
	var root string
	for _, c := range botwRoots {
		if abs, err := filepath.Abs(c); err == nil {
			c = abs
		}
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			root = c
			break
		}
	}
	if root != "" {
		fs := http.FileServer(http.Dir(root))
		mux.Handle("/botw/", http.StripPrefix("/botw/", fs))
		fmt.Printf("  [asset] BOTWmap assets mounted: %s\n", root)
	} else {
		mux.HandleFunc("/botw/", func(w http.ResponseWriter, r *http.Request) {
			cors(w)
			w.WriteHeader(404)
		})
		fmt.Println("  [asset] BOTWmap not found - using grid basemap (navigation unaffected)")
	}

	// 自研地图页
	webDir := locateWebDir()
	if webDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
		fmt.Printf("  [web]   map page dir: %s\n", webDir)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			cors(w)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<meta charset="utf-8"><h3>CemuNavi 运行中</h3>
<p>未找到地图页目录（web/）。请保证 CemuNavi.exe 与 web/ 同级。</p>
<p>API 可用：<a href="/pos">/pos</a> · <a href="/status">/status</a></p>`))
		})
	}
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
