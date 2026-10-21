// enavi-gui 的 Wails 后端。
//
// ★ 双入口独立原则（沿用 xnavi-gui 的同一套契约）：
//
//	GUI ↔ 核心  只走文件：status.json（核心写）+ enavi.log（核心写）
//	              + rescan.req（GUI 写 → 核心消费）
//	  → **完全不依赖 8766 HTTP**。8766 被别的程序占了 → 网页端不可用，
//	    但 GUI 照常能定位、能看日志、能手动重扫。
//	核心 ↔ 网页  8766 HTTP /pos（协议 v1，app/js/live.js 零改动）。
package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net"
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

// guiVersion GUI 版本号（导航程序版本独立，不跟网页版）。
const guiVersion = "v0.1.0"

// corePort 核心的 HTTP 端口（与网页端共用）。
const corePort = 8766

// App Wails 后端：管理 enavi 核心子进程 + 读 status.json / enavi.log。
type App struct {
	ctx     context.Context
	mu      sync.Mutex
	cmd     *exec.Cmd
	workDir string
	logBuf  []string
	logMark string
	cfg     *Config
}

// Config GUI 持久化配置（enavi-gui-config.json）。
type Config struct {
	EdenSaveDir string `json:"edenSaveDir"` // Eden 的 BOTW 存档目录（可选，留空自动探测）
}

func (a *App) configPath() string { return filepath.Join(a.workDir, "enavi-gui-config.json") }

func (a *App) loadConfig() *Config {
	cfg := &Config{}
	buf, err := os.ReadFile(a.configPath())
	if err == nil {
		json.Unmarshal(buf, cfg)
	}
	return cfg
}

// SaveConfig 保存 GUI 配置。
func (a *App) SaveConfig(c Config) string {
	a.cfg = &c
	buf, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(a.configPath(), buf, 0o644); err != nil {
		return "保存失败: " + err.Error()
	}
	return "已保存"
}

// LoadConfig 读取 GUI 配置。
func (a *App) LoadConfig() Config {
	if a.cfg == nil {
		a.cfg = a.loadConfig()
	}
	return *a.cfg
}

// NewApp 建 App（工作目录 = exe 所在目录）。
func NewApp() *App {
	exe, _ := os.Executable()
	return &App{workDir: filepath.Dir(exe)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.cfg = a.loadConfig()
	// 清理孤儿核心进程（GUI 崩溃/被杀后其子进程可能残留并独占 8766 端口）。
	exec.Command("taskkill", "/IM", "enavi.exe", "/F").Run()
	// 上一次的日志不带到这一轮（面板从干净状态开始，便于复现问题）。
	_ = os.WriteFile(filepath.Join(a.workDir, "enavi.log"), nil, 0o644)
	a.tailInit()
	go a.eventBridge(ctx)
}

func (a *App) onShutdown(ctx context.Context) {
	a.stopCoreLocked()
}

// eventBridge 每 800ms 推一次状态与日志增量。
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

// Version 返回 GUI 版本号。
func (a *App) Version() string { return guiVersion }

// corePath 核心子进程 exe 路径：与 GUI 同目录的 enavi.exe。
func (a *App) corePath() string {
	return filepath.Join(a.workDir, "enavi.exe")
}

// StartCore 启动核心子进程（enavi.exe）。
func (a *App) StartCore() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopCoreLocked()
	exe := a.corePath()
	if _, err := os.Stat(exe); err != nil {
		return "找不到核心程序 enavi.exe（应放在本程序同目录）"
	}
	args := []string{"--status-file", "status.json", "--rescan-req", "rescan.req"}
	if a.cfg != nil && a.cfg.EdenSaveDir != "" {
		args = append(args, "--eden-save-dir="+a.cfg.EdenSaveDir)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = a.workDir
	// 核心是控制台程序，GUI 已在日志面板实时显示它的输出 → 隐藏它的黑窗。
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	// ★ stdout/stderr 重定向到独立文件，**不要**指向 enavi.log：
	//   核心的 logPrint 会同时写 stdout 和 enavi.log（两份），
	//   若 stdout 也重定向到 enavi.log → 日志面板每行出现两遍。
	//   enavi.log 留给面板读；core.log 只留崩溃栈等 stderr 内容。
	f, err := os.OpenFile(filepath.Join(a.workDir, "enavi-gui-core.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}
	if err := cmd.Start(); err != nil {
		return "启动失败: " + err.Error()
	}
	a.cmd = cmd
	go func() {
		cmd.Wait()
		a.mu.Lock()
		if a.cmd == cmd {
			a.cmd = nil
		}
		a.mu.Unlock()
	}()
	return "核心已启动（Eden / BOTW）"
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
}

// CoreRunning 核心子进程是否在运行。
func (a *App) CoreRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil
}

// Rescan 请求核心重新扫描（写请求文件，不依赖 HTTP）。
func (a *App) Rescan() string {
	path := filepath.Join(a.workDir, "rescan.req")
	if err := os.WriteFile(path, []byte(time.Now().Format(time.RFC3339Nano)), 0o644); err != nil {
		return "提交失败: " + err.Error()
	}
	return "已提交重新扫描"
}

// statusPath 核心每 2 秒写的状态快照。
func (a *App) statusPath() string { return filepath.Join(a.workDir, "status.json") }

// CoreStatus 读 status.json 并附加 GUI 侧状态（running / webOK / webNote）。
//
// 注意：status.json 里的 anchor 是 {X,Y,Z} 对象（核心写的是 scan.Vec3 结构体）。
func (a *App) CoreStatus() map[string]any {
	a.mu.Lock()
	running := a.cmd != nil && a.cmd.Process != nil
	a.mu.Unlock()
	obj := map[string]any{"running": running}
	buf, err := os.ReadFile(a.statusPath())
	if err == nil && json.Unmarshal(buf, &obj) == nil {
		// 核心写的 anchor 是对象，前端按 {X,Y,Z} 读。
	} else {
		obj["ok"] = false
		obj["note"] = "核心未写入状态（未运行或启动中）"
	}
	// 网页端可用性：核心自己起 8766（协议 v1 给 live.js 读）。
	// 端口被别的程序占了 → 核心会打 "[fatal] HTTP 启动失败" → 网页端不可用，
	// 但 GUI 定位不受影响（双入口独立）。
	obj["webOK"] = true
	obj["webNote"] = "网页端待核心启动后可用"
	if !running {
		if portInUse(corePort) {
			obj["webOK"] = false
			obj["webNote"] = "端口 8766 被其他程序占用：网页端不可用（GUI 定位不受影响）"
		}
	} else if strings.Contains(a.recentLog(), "HTTP 启动失败") {
		obj["webOK"] = false
		obj["webNote"] = "端口 8766 被占用：网页端不可用（GUI 定位不受影响）"
	} else {
		obj["webNote"] = "网页端可用：http://127.0.0.1:" + fmt.Sprint(corePort) + "/pos"
	}
	return obj
}

// recentLog 最近若干行日志（供关键字扫描，如端口占用）。
func (a *App) recentLog() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.logBuf)
	if n > 60 {
		n = 60
	}
	return strings.Join(a.logBuf[len(a.logBuf)-n:], "\n")
}

// PollLogs 返回 enavi.log 新增行（增量读）。
func (a *App) PollLogs() []string {
	path := filepath.Join(a.workDir, "enavi.log")
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(buf)
	if content == a.logMark {
		return nil
	}
	if strings.HasPrefix(content, a.logMark) {
		newPart := strings.TrimPrefix(content, a.logMark)
		lines := strings.Split(strings.ReplaceAll(newPart, "\r\n", "\n"), "\n")
		a.appendLines(lines)
		a.logMark = content
		return nonEmpty(lines)
	}
	// 文件被轮转/替换 → 全量重读
	a.logMark = content
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	a.appendLines(lines)
	return nonEmpty(lines)
}

// TailLog 返回日志缓冲（环形，最多 2000 行）—— 前端首屏用。
func (a *App) TailLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.logBuf))
	copy(out, a.logBuf)
	return out
}

func (a *App) tailInit() {
	a.logMark = ""
}

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

func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// OpenLogDir 打开日志/数据目录。
func (a *App) OpenLogDir() string {
	exec.Command("explorer", a.workDir).Start()
	return "已打开日志目录"
}

// OpenMap 打开网页地图。
func (a *App) OpenMap() string {
	exec.Command("cmd", "/c", "start", "", "https://botw.yalin.site/?follow=1&game=botw").Start()
	return "已打开网页地图"
}

// PickDir 选目录（前端「浏览」按钮）。
func (a *App) PickDir(title string) string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil {
		return ""
	}
	return dir
}

// ExportDiagnostics 一键导出诊断包（zip）。
func (a *App) ExportDiagnostics() string {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出诊断包",
		DefaultFilename: fmt.Sprintf("enavi-diag-%s.zip", time.Now().Format("20060102-150405")),
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

	info := fmt.Sprintf(
		"enavi diagnostics\n==================\nGUI version: %s\nOS: %s/%s\nArch: %s\nCPU cores: %d\nTime: %s\nEdenSaveDir configured: %q\n\n",
		guiVersion, stdruntime.GOOS, stdruntime.GOARCH, stdruntime.GOARCH,
		stdruntime.NumCPU(),
		time.Now().Format("2006-01-02 15:04:05"),
		func() string {
			if a.cfg != nil {
				return a.cfg.EdenSaveDir
			}
			return ""
		}(),
	)
	if bw, e := w.Create("info.txt"); e == nil {
		bw.Write([]byte(info))
	}

	for _, name := range []string{"status.json", "enavi.log", "known_player.json"} {
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
