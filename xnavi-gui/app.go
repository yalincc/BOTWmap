package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// guiVersion 导航程序版本（三合一统一版本 v3.0.0，2026-10-07 拍板：
// GUI 壳与导航核心 xnavi 共享同一版本号，不再各自解耦）。
// 注意：必须与 xnavi/version.go 的 CoreVersion 保持一致。
const guiVersion = "v3.2.0"

// App Wails 后端：管理核心子进程 + 读 status.json / xnavi-gui-core.log + 网页端端口探测。
// 与核心的通信完全走文件（status.json、xnavi-gui-core.log），不依赖 8766 HTTP——
// 保证 GUI 与网页端两个入口相互独立（ds 双入口原则）：8766 被占时 GUI 照常工作。
type App struct {
	ctx       context.Context
	mu        sync.Mutex
	cmd       *exec.Cmd
	corePid   int // v3.2.1：本次拉起的 core 子进程号，用于状态快照会话签名
	workDir   string
	logBuf    []string
	logMarker string
	cfg       *Config
}

// Config GUI 持久化配置（xnavi-gui-config.json）。
// v3.0.1：三平台各自独立的 BOTW 存档路径（留空自动探测）。
type Config struct {
	CemuSaveDir    string `json:"cemuSaveDir"`    // Cemu 的 BOTW 存档目录（可选）
	RyujinxSaveDir string `json:"ryujinxSaveDir"` // Ryujinx 的 BOTW 存档目录（可选）
	EdenSaveDir    string `json:"edenSaveDir"`    // Eden 的 BOTW 存档目录（可选）
	Emulator       string `json:"emulator"`       // auto | cemu | ryujinx | eden
	Game           string `json:"game"`           // auto | botw | totk
	TLS            bool   `json:"tls"`             // v3.1.0：--tls 双协议，手机镜像页防息屏
	AutoOpenMap    *bool  `json:"autoOpenMap"`    // 定位成功后自动打开地图网页（默认 true）
	CustomMapURL   string `json:"customMapURL"`   // 自定义地图地址（留空=localhost:8766）
	AutoDetectSave *bool  `json:"autoDetectSave"` // 自动检测存档（默认 true）
}

func (a *App) configPath() string { return filepath.Join(a.workDir, "xnavi-gui-config.json") }

func (a *App) loadConfig() *Config {
	cfg := &Config{Emulator: "auto", Game: "auto"}
	buf, err := os.ReadFile(a.configPath())
	if err == nil {
		json.Unmarshal(buf, cfg)
	}
	// 为指针类型设默认值（JSON 未写字段时保持 nil，这里补全）
	defaultTrue := true
	if cfg.AutoOpenMap == nil    { cfg.AutoOpenMap = &defaultTrue }
	if cfg.AutoDetectSave == nil { cfg.AutoDetectSave = &defaultTrue }
	return cfg
}

func (a *App) SaveConfig(c Config) string {
	a.cfg = &c
	buf, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(a.configPath(), buf, 0644)
	return "已保存"
}

func (a *App) LoadConfig() Config {
	if a.cfg == nil {
		a.cfg = a.loadConfig()
	}
	return *a.cfg
}

func NewApp() *App {
	exe, _ := os.Executable()
	return &App{workDir: filepath.Dir(exe)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.cfg = a.loadConfig()
	// 清理孤儿 core（2026-10-08 真机事故修复）：GUI 崩溃/被杀后其 core 子进程
	// 可能残留并独占 8766 端口 → 新 core 全部 HTTP 绑定失败 + 平台反复重新探测
	// （表现为"换来换去"）。startup 时按进程名清理一次（core 是 GUI 专属子进程，
	// 用户不会单独运行；core 侧另有单实例锁兜底）。
	exec.Command("taskkill", "/IM", "xnavi-core.exe", "/F").Run()
	a.tailInit()
	go a.eventBridge(ctx)
	// 手动检查更新（不在启动时自动弹出）
	runtime.EventsOn(ctx, "update:check", func(data ...interface{}) {
		go a.checkUpdate()
	})
}

// isNewerVersion 简单的 semver 版本比较：remote 是否严格大于 current
func isNewerVersion(remote, current string) bool {
	r := strings.TrimPrefix(remote, "v")
	c := strings.TrimPrefix(current, "v")
	rParts := strings.Split(r, ".")
	cParts := strings.Split(c, ".")
	for i := 0; i < len(rParts) && i < len(cParts); i++ {
		var rn, cn int
		fmt.Sscanf(rParts[i], "%d", &rn)
		fmt.Sscanf(cParts[i], "%d", &cn)
		if rn > cn {
			return true
		}
		if rn < cn {
			return false
		}
	}
	return len(rParts) > len(cParts)
}

// checkUpdate 手动检查更新：查 GitHub tags 列表，若存在更高版本则通知前端。
func (a *App) checkUpdate() {
	defer func() { recover() }()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/yalincc/BOTWmap/tags?per_page=30")
	if err != nil { return }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tags []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &tags) != nil { return }
	found := false
	for _, t := range tags {
		if !strings.HasPrefix(t.Name, "xnavi-") { continue }
		latest := strings.TrimPrefix(t.Name, "xnavi-")
		if isNewerVersion(latest, guiVersion) {
			runtime.EventsEmit(a.ctx, "update:available", map[string]string{
				"latest": latest,
				"url":    "https://github.com/yalincc/BOTWmap/releases/tag/" + t.Name,
			})
		} else {
			runtime.EventsEmit(a.ctx, "update:latest", nil)
		}
		found = true
		break
	}
	if !found {
		runtime.EventsEmit(a.ctx, "update:latest", nil)
	}
}

func (a *App) PickDir(title string) string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil { return "" }
	return dir
}

// PickSaveFile 通过文件选择对话框挑选 game_data.sav 或任意 .sav 存档文件。
func (a *App) PickSaveFile() string {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 BOTW 存档文件 (game_data.sav)",
		Filters: []runtime.FileFilter{
			{DisplayName: "BOTW 存档文件 (*.sav)", Pattern: "*.sav;game_data.sav"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil || file == "" {
		return ""
	}
	return file
}

// OpenSaveDir 在资源管理器中直接高亮当前正在生效的存档文件；若文件不存在则打开目录。
func (a *App) OpenSaveDir() string {
	st := a.CoreStatus()
	savePath := ""
	if prog, ok := st["progress"].(map[string]any); ok {
		if s, ok := prog["save"].(string); ok {
			savePath = s
		}
	}
	if savePath == "" {
		if s, ok := st["save"].(string); ok {
			savePath = s
		}
	}
	if savePath == "" && a.cfg != nil {
		for _, d := range []string{a.cfg.CemuSaveDir, a.cfg.RyujinxSaveDir, a.cfg.EdenSaveDir} {
			if d != "" && pathExists(d) {
				savePath = d
				break
			}
		}
	}
	if savePath == "" {
		return "暂无正在生效的存档路径"
	}
	if err := openSaveLocation(savePath); err != nil {
		return "打开失败: " + err.Error()
	}
	return "已在资源管理器中定位存档"
}

// ApplySavePath 手动指定存档路径（支持 .sav 文件或目录），即时生效并持久化。
func (a *App) ApplySavePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return a.ResetSaveDetection()
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "路径不存在: " + path
	}
	cleanPath := filepath.Clean(path)
	if !fi.IsDir() {
		base := strings.ToLower(filepath.Base(cleanPath))
		if !strings.HasSuffix(base, ".sav") {
			return "请选择以 .sav 结尾的存档文件（如 game_data.sav）"
		}
	}

	if a.cfg == nil {
		a.cfg = a.loadConfig()
	}

	lower := strings.ToLower(cleanPath)
	emu := a.cfg.Emulator
	if emu == "cemu" || strings.Contains(lower, "00050000") || strings.Contains(lower, "cemu") {
		a.cfg.CemuSaveDir = cleanPath
	} else if emu == "ryujinx" || strings.Contains(lower, "bis") || strings.Contains(lower, "ryujinx") {
		a.cfg.RyujinxSaveDir = cleanPath
	} else if emu == "eden" || strings.Contains(lower, "nand") || strings.Contains(lower, "eden") || strings.Contains(lower, "yuzu") {
		a.cfg.EdenSaveDir = cleanPath
	} else {
		a.cfg.CemuSaveDir = cleanPath
		a.cfg.RyujinxSaveDir = cleanPath
		a.cfg.EdenSaveDir = cleanPath
	}
	a.SaveConfig(*a.cfg)

	// 若 core 正在运行，通过 HTTP /save-override 实时通知生效
	if a.CoreRunning() {
		body, _ := json.Marshal(map[string]any{"path": cleanPath})
		req, err := http.NewRequest("POST", "http://127.0.0.1:8766/save-override", strings.NewReader(string(body)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			client := http.Client{Timeout: 1 * time.Second}
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}
	return "已载入存档: " + cleanPath
}

// ResetSaveDetection 清除手动指定存档路径，恢复全自动探测。
func (a *App) ResetSaveDetection() string {
	if a.cfg == nil {
		a.cfg = a.loadConfig()
	}
	a.cfg.CemuSaveDir = ""
	a.cfg.RyujinxSaveDir = ""
	a.cfg.EdenSaveDir = ""
	a.SaveConfig(*a.cfg)

	if a.CoreRunning() {
		body, _ := json.Marshal(map[string]any{"clear": true})
		req, err := http.NewRequest("POST", "http://127.0.0.1:8766/save-override", strings.NewReader(string(body)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			client := http.Client{Timeout: 1 * time.Second}
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}
	return "已恢复自动探测"
}

func (a *App) onShutdown(ctx context.Context) {
	a.stopCoreLocked()
}

// eventBridge 每 800ms 推一次状态与日志增量（Wails Runtime 事件流）。
func (a *App) eventBridge(ctx context.Context) {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st := a.CoreStatus()
			runtime.EventsEmit(ctx, "status:update", st)
			if lines := a.PollLogs(); len(lines) > 0 {
				runtime.EventsEmit(ctx, "log:append", lines)
			}
		}
	}
}

// Version 返回 GUI 版本号（前端显示用）。
func (a *App) Version() string { return guiVersion }

// Calibrate 基准校准（S3）：把用户从游戏地图读到的坐标写入 calibrate.json，
// core 轮询消费（文件通信，遵守双入口原则）。返回提示文案。
func (a *App) Calibrate(x, y, z float64) string {
	req := map[string]any{"x": x, "y": y, "z": z, "ts": time.Now().UnixNano()}
	buf, _ := json.Marshal(req)
	if err := os.WriteFile(filepath.Join(a.workDir, "calibrate.json"), buf, 0644); err != nil {
		return "写入校准请求失败: " + err.Error()
	}
	return "校准请求已提交（约 5-10 秒出结果，看日志面板 [calib] 行）"
}

// corePath 核心子进程 exe 路径：与 GUI 同目录的 xnavi-core.exe。
func (a *App) corePath() string {
	return filepath.Join(a.workDir, "xnavi-core.exe")
}

// StartCore 启动核心子进程（xnavi-core.exe --emu=<emu> [--game=<game>] --no-open）。
// emu: auto | ryujinx | cemu | eden；game 从已保存配置读取（auto | botw | totk，auto 时不传让 core 自动识别）。
// 重复调用会先停掉旧进程。
func (a *App) StartCore(emu string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopCoreLocked()
	exe := a.corePath()
	if _, err := os.Stat(exe); err != nil {
		return "找不到核心程序 xnavi-core.exe（应放在本程序同目录）"
	}
	args := []string{"--emu=" + emu, "--no-open"}
	game := ""
	if a.cfg != nil {
		game = a.cfg.Game
	}
	if game != "" && game != "auto" {
		args = append(args, "--game="+game)
	}
	if a.cfg.CemuSaveDir != "" {
		args = append(args, "--cemu-save-dir="+a.cfg.CemuSaveDir)
	}
	if a.cfg.RyujinxSaveDir != "" {
		args = append(args, "--ryujinx-save-dir="+a.cfg.RyujinxSaveDir)
	}
	if a.cfg.EdenSaveDir != "" {
		args = append(args, "--eden-save-dir="+a.cfg.EdenSaveDir)
	}
	if a.cfg != nil && a.cfg.TLS {
		args = append(args, "--tls")
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = a.workDir
	// core 是控制台程序，但 GUI 已经在日志面板实时显示它的输出，
	// 这里把它的控制台窗口隐藏，避免弹一个黑窗且里面空白（stdout 重定向到了文件）。
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	f, err := os.OpenFile(filepath.Join(a.workDir, "xnavi-gui-core.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}
	if err := cmd.Start(); err != nil {
		return "启动失败: " + err.Error()
	}
	a.cmd = cmd
	a.corePid = cmd.Process.Pid
	go func() {
		cmd.Wait()
		a.mu.Lock()
		if a.cmd == cmd {
			a.cmd = nil
			a.corePid = 0
		}
		a.mu.Unlock()
	}()
	msg := "核心已启动（" + emu + "）"
	if game != "" && game != "auto" {
		msg += " · 游戏 " + game
	}
	return msg
}

// StopCore 停止核心子进程。
func (a *App) StopCore() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopCoreLocked()
	return "已停止"
}

func (a *App) stopCoreLocked() {
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
		a.cmd = nil
	}
	a.corePid = 0
}

// CoreRunning 核心子进程是否在运行。
func (a *App) CoreRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil
}

// statusPath 核心每 2s 写的会话快照。
func (a *App) statusPath() string { return filepath.Join(a.workDir, "status.json") }

// snapshotSignedBy 校验快照里的 corePid 是否就是本次拉起的子进程号。
// v3.2.1：status.json 会残留在磁盘上，若不做会话签名校验，上次会话的旧坐标
// 与 verified=true 会被当成本次状态，导致地图带着旧位置自动弹出。
func snapshotSignedBy(obj map[string]any, corePid int) bool {
	if corePid <= 0 {
		return false
	}
	v, ok := obj["corePid"].(float64)
	return ok && int(v) == corePid
}

// CoreStatus 读 status.json 并附加 GUI 侧状态（running / webOK / webNote）。
func (a *App) CoreStatus() map[string]any {
	a.mu.Lock()
	running := a.cmd != nil && a.cmd.Process != nil
	corePid := a.corePid
	a.mu.Unlock()
	obj := map[string]any{"running": running}
	buf, err := os.ReadFile(a.statusPath())
	if err == nil && json.Unmarshal(buf, &obj) == nil {
	} else {
		obj["ok"] = false
		obj["note"] = "核心未写入状态（未运行或启动中）"
	}
	// 会话签名门禁：快照必须由本次启动的 core 写出，否则整条丢弃。
	if !running || !snapshotSignedBy(obj, corePid) {
		obj = map[string]any{"running": running, "ok": false, "verified": false}
		if running {
			obj["note"] = "核心启动中，等待本次会话首份状态"
		} else {
			obj["note"] = "核心未运行"
		}
	}
	// 网页端可用性：核心运行且 8766 未被外部占用 → 可用。
	// 核心日志里出现 "HTTP 端口被占用" 说明核心启动时 listen 失败。
	obj["webOK"] = !portInUse(8766) || running
	obj["webNote"] = ""
	if !running {
		if portInUse(8766) {
			obj["webOK"] = false
			obj["webNote"] = "端口 8766 被其他程序占用：网页端不可用（GUI 定位不受影响）"
		} else {
			obj["webNote"] = "网页端待核心启动后可用"
		}
	} else if strings.Contains(a.lastLog(), "HTTP 端口被占用") {
		obj["webOK"] = false
		obj["webNote"] = "HTTP 端口被占用：网页端不可用，GUI 定位正常（双入口独立）"
	}
	return obj
}

// TailLog 返回日志缓冲（环形，最多 2000 行）。
func (a *App) TailLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.logBuf))
	copy(out, a.logBuf)
	return out
}

// lastLog 最近 N 行拼接（供关键字扫描）。
func (a *App) lastLog() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.logBuf)
	if n > 50 {
		n = 50
	}
	return strings.Join(a.logBuf[len(a.logBuf)-n:], "\n")
}

// tailInit 首次加载 xnavi-gui-core.log 末尾（最多 2000 行）。
func (a *App) tailInit() {
	os.WriteFile(filepath.Join(a.workDir, "xnavi-gui-core.log"), nil, 0644)
	a.logMarker = ""
}

// appendLines 追加日志到环形缓冲（2000 行上限，ds 提醒 2）。
func (a *App) appendLines(lines []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, l := range lines {
		if l == "" {
			continue
		}
		a.logBuf = append(a.logBuf, l)
	}
	const max = 2000
	if len(a.logBuf) > max {
		a.logBuf = a.logBuf[len(a.logBuf)-max:]
	}
}

// PollLogs 前端轮询：返回 xnavi-gui-core.log 新增行（增量读）。
func (a *App) PollLogs() []string {
	path := filepath.Join(a.workDir, "xnavi-gui-core.log")
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(buf)
	// 记录已读字节偏移，避免重发
	if content == a.logMarker {
		return nil
	}
	if strings.HasPrefix(content, a.logMarker) {
		newPart := strings.TrimPrefix(content, a.logMarker)
		lines := strings.Split(strings.ReplaceAll(newPart, "\r\n", "\n"), "\n")
		a.appendLines(lines)
		a.logMarker = content
		return a.lastNLines(lines)
	}
	// 文件被轮转/替换：全量重读
	a.logMarker = content
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	a.appendLines(lines)
	return a.lastNLines(lines)
}

var _ = fmt.Sprintf

func (a *App) lastNLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// OpenMapURL 在默认浏览器打开地图网页（支持 http/https/file:/// 自定义地址）。
func (a *App) OpenMapURL() string {
	url := "http://127.0.0.1:8766"
	if a.cfg != nil && a.cfg.CustomMapURL != "" {
		url = a.cfg.CustomMapURL
	}
	if err := exec.Command("cmd", "/c", "start", "", url).Start(); err != nil {
		return "打开失败: " + err.Error()
	}
	return "已打开: " + url
}

// OpenLogDir 打开日志/数据目录（方便用户反馈时复制日志）。
func (a *App) OpenLogDir() string {
	exec.Command("explorer", a.workDir).Start()
	return "已打开日志目录"
}

// ManualCalibrate 手工坐标校准（向核心发送 /rescan?x=...&z=...）
func (a *App) ManualCalibrate(x, z float64) string {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:8766/rescan?x=%.1f&z=%.1f", x, z))
	if err != nil {
		return "发送校准请求失败（核心可能未启动）"
	}
	defer resp.Body.Close()
	return fmt.Sprintf("已发送手工校准坐标 (%.1f, %.1f)，开启定向快速找人", x, z)
}

// Rescan 触发一次全局重新扫描
func (a *App) Rescan() string {
	resp, err := http.Get("http://127.0.0.1:8766/rescan")
	if err != nil {
		return "发送重扫请求失败"
	}
	defer resp.Body.Close()
	return "已发送重扫请求"
}

// hostname returns machine name (best effort).
func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// ExportDiagnostics 一键导出诊断包（zip）：系统信息 + status.json + core 日志 + 事件日志。
// 用户在反馈问题前点这个，选保存位置，把 zip 发回来即可。
func (a *App) ExportDiagnostics() string {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出诊断包",
		DefaultFilename: fmt.Sprintf("xnavi-diag-%s.zip", time.Now().Format("20060102-150405")),
		Filters:         []runtime.FileFilter{{DisplayName: "ZIP 压缩包", Pattern: "*.zip"}},
	})
	if err != nil || path == "" {
		return "已取消"
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		path += ".zip"
	}
	zipFile, err := os.Create(path)
	if err != nil {
		return "创建失败: " + err.Error()
	}
	defer zipFile.Close()
	w := zip.NewWriter(zipFile)
	defer w.Close()

	// 1) 系统信息
	info := fmt.Sprintf(
		"xnavi diagnostics\n===================\nGUI version: %s\nOS: %s/%s\nArch: %s\nCPU cores: %d\nHostname: %s\nTime: %s\nEmulator setting: %s\nCemuSaveDir configured: %q\nRyujinxSaveDir configured: %q\nEdenSaveDir configured: %q\n\n",
		guiVersion, stdruntime.GOOS, stdruntime.GOARCH, stdruntime.GOARCH,
		stdruntime.NumCPU(), hostname(),
		time.Now().Format("2006-01-02 15:04:05"),
		func() string {
			if a.cfg != nil {
				return a.cfg.Emulator
			}
			return "auto"
		}(),
		func() string { if a.cfg != nil { return a.cfg.CemuSaveDir }; return "" }(),
		func() string { if a.cfg != nil { return a.cfg.RyujinxSaveDir }; return "" }(),
		func() string { if a.cfg != nil { return a.cfg.EdenSaveDir }; return "" }(),
	)
	if bw, e := w.Create("info.txt"); e == nil {
		bw.Write([]byte(info))
	}

	// 2) 工作目录下的日志/状态文件（core 写的）
	for _, name := range []string{"status.json", "xnavi-gui-core.log", "run-events.log"} {
		buf, err := os.ReadFile(filepath.Join(a.workDir, name))
		if err != nil {
			continue
		}
		if bw, e := w.Create(name); e == nil {
			bw.Write(buf)
		}
	}
	return "已导出: " + path
}

// portInUse 探测端口是否被占用。
func portInUse(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// EnvDetect 探测"已配置的"模拟器路径是否存在（仅给前端做提示，不挡开始按钮）。
// 真实"是否附加到运行中的模拟器进程"由 core 经 status.json 的 pid 上报，
// 这里不再硬编码开发机盘符——任何用户机器上都不该因为路径猜不到而点不了开始。
// Eden 无路径配置（进程名 + 存档自动探测），恒 false，前端提示免配置。
func (a *App) EnvDetect() map[string]any {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	res := map[string]any{"ryujinx": false, "cemu": false, "eden": false}
	if cfg != nil {
		if cfg.CemuSaveDir != "" && pathExists(cfg.CemuSaveDir) {
			res["cemu"] = true
		}
		if cfg.RyujinxSaveDir != "" && pathExists(cfg.RyujinxSaveDir) {
			res["ryujinx"] = true
		}
		if cfg.EdenSaveDir != "" && pathExists(cfg.EdenSaveDir) {
			res["eden"] = true
		}
	}
	return res
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func openSaveLocation(savePath string) error {
	if savePath == "" {
		return fmt.Errorf("无有效存档路径")
	}
	savePath = filepath.Clean(savePath)
	fi, err := os.Stat(savePath)
	if err == nil {
		if !fi.IsDir() {
			return exec.Command("explorer.exe", "/select,", savePath).Start()
		}
		return exec.Command("explorer.exe", savePath).Start()
	}
	dir := filepath.Dir(savePath)
	if _, err := os.Stat(dir); err == nil {
		return exec.Command("explorer.exe", dir).Start()
	}
	return fmt.Errorf("路径不存在: %s", savePath)
}
