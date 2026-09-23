// module: live_float
// depends_on: botwnavi(HTTP API 127.0.0.1:8766, 可自动拉起), app/(只读静态资源)
// depended_by: 无
// status: M4 交互完善

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- 常量 ----------
const (
	WinW    = 420
	WinH    = 520
	SrvPort = 8777
	PosFile = "data/overlay_pos.json"
)

// ---------- 位置记忆 ----------
type posInfo struct {
	X int `json:"x"`
	Y int `json:"y"`
}

var (
	posMu   sync.Mutex
	lastPos posInfo
)

func posPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), PosFile)
}

func loadPos() posInfo {
	var p posInfo
	buf, err := os.ReadFile(posPath())
	if err == nil {
		_ = json.Unmarshal(buf, &p)
	}
	return p
}

func savePos(p posInfo) {
	posMu.Lock()
	lastPos = p
	posMu.Unlock()
	os.MkdirAll(filepath.Dir(posPath()), 0755)
	buf, _ := json.Marshal(p)
	_ = os.WriteFile(posPath(), buf, 0644)
}

// ---------- HTTP 服务（后台 goroutine） ----------
func startServer(win *floatWindow) {
	exeDir, _ := os.Executable()
	webDir := filepath.Join(exeDir, "web")
	if _, err := os.Stat(webDir); err != nil {
		if cwd, e := os.Getwd(); e == nil {
			webDir = filepath.Join(cwd, "web")
		}
	}
	appDir := filepath.Clean(filepath.Join(webDir, "..", "..", "app"))
	// 可选覆盖：FLOATNAVI_APP_DIR 指定地图资源目录（默认 exe 上级 app/）。
	// 测试"无本地资源走线上"时置为空目录；正常使用无需设置。
	if env := os.Getenv("FLOATNAVI_APP_DIR"); env != "" {
		appDir = filepath.Clean(env)
	}

	mux := http.NewServeMux()
	mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir(appDir))))
	mux.Handle("/", http.FileServer(http.Dir(webDir)))

	mux.HandleFunc("/win/move", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Dx int `json:"dx"`
			Dy int `json:"dy"`
		}
		if json.NewDecoder(r.Body).Decode(&d) == nil {
			win.moveBy(d.Dx, d.Dy)
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("/win/save", func(w http.ResponseWriter, r *http.Request) {
		win.saveCurrentPos()
		w.WriteHeader(200)
	})
	mux.HandleFunc("/win/quit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		go win.quit()
	})
	mux.HandleFunc("/win/status", func(w http.ResponseWriter, r *http.Request) {
		rc := win.getRect()
		fmt.Fprintf(w, `{"x":%d,"y":%d,"w":%d,"h":%d}`, rc.Left, rc.Top, rc.Right-rc.Left, rc.Bottom-rc.Top)
	})

	// 透明度（页面级 CSS opacity）：经 WebView2 Eval 直接设置，可确定性验证；
	// 同时写 localStorage 持久化（重启后恢复）。滑块 UI 与 HTTP 端点等价。
	mux.HandleFunc("/win/alpha", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			V int `json:"v"`
		}
		if json.NewDecoder(r.Body).Decode(&d) == nil && d.V >= 30 && d.V <= 100 {
			// 经 UI 线程执行队列调用 WebView2 Eval（跨线程 COM 调用会崩溃）
			js := fmt.Sprintf("document.body.style.opacity=%v;localStorage.setItem('botwmap.float.alpha','%d');", float64(d.V)/100.0, d.V)
			win.evalOnUI(js)
		}
		w.WriteHeader(200)
	})

	// 同源代理：/target → BotwNavi 8766（页面与 BotwNavi 跨端口，JSON POST 触发 preflight 被 CORS 拦截，
	// 故经本代理转发；/pos GET 为简单请求可直接跨源，无需代理）
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		body, _ := io.ReadAll(r.Body)
		req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8766/target", bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(502)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(502)
			fmt.Fprintf(w, `{"ok":false,"err":%q}`, err.Error())
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	// 同源代理：/win/rescan → BotwNavi /rescan（冻结自检：坐标长时间不更新时强制重新定位）
	mux.HandleFunc("/win/rescan", func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get("http://127.0.0.1:8766/rescan")
		if err != nil {
			w.WriteHeader(502)
			fmt.Fprintf(w, `{"ok":false,"err":%q}`, err.Error())
			return
		}
		resp.Body.Close()
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	})

	// 自愈：重启 BotwNavi（数据异常/漂移时 FloatNavi 管理其依赖生命周期；BotwNavi 由本模块拉起或已是依赖）
	mux.HandleFunc("/win/restart-navi", func(w http.ResponseWriter, r *http.Request) {
		go restartBotwNavi()
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", SrvPort))
	if err != nil {
		log.Printf("[live-float] HTTP 服务启动失败: %v", err)
		return
	}
	log.Printf("[live-float] HTTP 服务 http://127.0.0.1:%d （web=%s, app=%s）", SrvPort, webDir, appDir)
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			log.Printf("[live-float] HTTP 服务退出: %v", err)
		}
	}()
}

// ensureBotwNavi 探测 127.0.0.1:8766，无服务则自动拉起 live-go/BotwNavi.exe（子进程）。
// 仅启动时探测一次；浮窗单独可删，BotwNavi 保持零依赖反向。
func ensureBotwNavi() {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:8766/pos?who=float-boot&t=" + fmt.Sprint(time.Now().UnixMilli()))
	if err == nil {
		resp.Body.Close()
		log.Printf("[live-float] BotwNavi 已在运行，无需拉起")
		return
	}
	startBotwNavi()
}

// startBotwNavi 启动 BotwNavi.exe（8766 --no-open）；返回是否成功。
func startBotwNavi() bool {
	exeDir, _ := os.Executable()
	navi := filepath.Join(filepath.Dir(exeDir), "..", "live-go", "BotwNavi.exe")
	if _, err := os.Stat(navi); err != nil {
		log.Printf("[live-float] 未找到 BotwNavi.exe（%s），跳过启动", navi)
		return false
	}
	cmd := exec.Command(navi, "8766", "--no-open")
	cmd.Dir = filepath.Dir(navi)
	if err := cmd.Start(); err != nil {
		log.Printf("[live-float] 启动 BotwNavi 失败: %v", err)
		return false
	}
	log.Printf("[live-float] 已启动 BotwNavi（PID %d）：%s", cmd.Process.Pid, navi)
	return true
}

// restartBotwNavi 杀旧 BotwNavi 进程并重启（数据异常/漂移自愈）。
// 注：BotwNavi 重启后 target 会清空（属预期）；浮窗页面会在重启后自动恢复轮询。
// 关键：重启前删除 known_addrs.json —— BotwNavi 的"记住偏移"快速路径会把上次锁的
// 错误槽也记住，重启后直接锁错（这是"重启了还卡"的根因）。清掉后强制走存档锚点
// 全新扫描，锁槽质量远好于记忆偏移。
func restartBotwNavi() {
	exeDir, _ := os.Executable()
	navi := filepath.Join(filepath.Dir(exeDir), "..", "live-go", "BotwNavi.exe")
	// 杀旧进程（tasklist 查找，兼容任意启动方式）
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq BotwNavi.exe", "/FO", "CSV", "/NH").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if !strings.Contains(line, "BotwNavi.exe") {
				continue
			}
			parts := strings.Split(line, "\",\"")
			if len(parts) >= 2 {
				pidStr := strings.Trim(parts[1], "\"")
				if pid, e := strconv.Atoi(pidStr); e == nil && pid > 0 {
					if p, e := os.FindProcess(pid); e == nil {
						_ = p.Kill()
						log.Printf("[live-float] 已停止旧 BotwNavi（PID %d）", pid)
					}
				}
			}
		}
	}
	time.Sleep(1200 * time.Millisecond)
	// 清掉记住的错误偏移，强制全新扫描
	if known := filepath.Join(filepath.Dir(navi), "known_addrs.json"); fileExists(known) {
		if err := os.Remove(known); err == nil {
			log.Printf("[live-float] 已清除 known_addrs.json（强制全新扫描）")
		}
	}
	if !startBotwNavi() {
		log.Printf("[live-float] 重启 BotwNavi 失败")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func main() {
	go ensureBotwNavi() // 异步探测 + 拉起，不阻塞窗口启动
	win := newFloatWindow(WinW, WinH)
	startServer(win)
	win.run() // 阻塞：创建窗口 + WebView2 + 消息循环
}
