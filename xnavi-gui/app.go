package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// guiVersion 导航程序版本（与地图网页版本解耦，见 BOTWmap 项目规则第 3 条）。
const guiVersion = "v2.0.0"

// App Wails 后端：管理核心子进程 + 读 status.json / run-events.log + 网页端端口探测。
// 与核心的通信完全走文件（status.json、run-events.log），不依赖 8766 HTTP——
// 保证 GUI 与网页端两个入口相互独立（ds 双入口原则）：8766 被占时 GUI 照常工作。
type App struct {
	ctx       context.Context
	mu        sync.Mutex
	cmd       *exec.Cmd
	workDir   string // 核心 exe 所在目录（status.json / run-events.log 同目录）
	logBuf    []string
	logMarker string // 日志增量读已消费偏移（文件内容前缀）
}

func NewApp() *App {
	exe, _ := os.Executable()
	return &App{workDir: filepath.Dir(exe)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.tailInit()
	// 事件桥：轮询 status.json / run-events.log 增量，EventsEmit 推给前端
	//（核心是子进程，事件源头是文件；体验与同进程实时推送一致）
	go a.eventBridge(ctx)
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

// corePath 核心子进程 exe 路径：与 GUI 同目录的 xnavi-core.exe。
func (a *App) corePath() string {
	return filepath.Join(a.workDir, "xnavi-core.exe")
}

// StartCore 启动核心子进程（xnavi-core.exe --emu=<emu> --no-open）。
// emu: auto | ryujinx | cemu。重复调用会先停掉旧进程。
func (a *App) StartCore(emu string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopCoreLocked()
	exe := a.corePath()
	if _, err := os.Stat(exe); err != nil {
		return "找不到核心程序 xnavi-core.exe（应放在本程序同目录）"
	}
	cmd := exec.Command(exe, "--emu="+emu, "--no-open")
	cmd.Dir = a.workDir
	f, err := os.OpenFile(filepath.Join(a.workDir, "xnavi-gui-core.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}
	if err := cmd.Start(); err != nil {
		return "启动失败: " + err.Error()
	}
	a.cmd = cmd
	go func() { cmd.Wait(); a.mu.Lock(); if a.cmd == cmd { a.cmd = nil }; a.mu.Unlock() }()
	return "核心已启动（" + emu + "）"
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

// statusPath 核心每 2s 写的会话快照。
func (a *App) statusPath() string { return filepath.Join(a.workDir, "status.json") }

// CoreStatus 读 status.json 并附加 GUI 侧状态（running / webOK / webNote）。
func (a *App) CoreStatus() map[string]any {
	a.mu.Lock()
	running := a.cmd != nil && a.cmd.Process != nil
	a.mu.Unlock()
	obj := map[string]any{"running": running}
	buf, err := os.ReadFile(a.statusPath())
	if err == nil && json.Unmarshal(buf, &obj) == nil {
	} else {
		obj["ok"] = false
		obj["note"] = "核心未写入状态（未运行或启动中）"
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

// tailInit 首次加载 run-events.log 末尾（最多 2000 行）。
func (a *App) tailInit() {
	buf, err := os.ReadFile(filepath.Join(a.workDir, "run-events.log"))
	if err != nil {
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n")
	a.appendLines(lines)
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

// PollLogs 前端轮询：返回 run-events.log 新增行（增量读）。
func (a *App) PollLogs() []string {
	path := filepath.Join(a.workDir, "run-events.log")
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

// OpenLogDir 打开日志/数据目录（方便用户反馈时复制日志）。
func (a *App) OpenLogDir() string {
	exec.Command("explorer", a.workDir).Start()
	return "已打开日志目录"
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

// EnvDetect 探测本机模拟器路径（给前端展示"已检测到"）。
func (a *App) EnvDetect() map[string]any {
	res := map[string]any{
		"ryujinx": pathExists("G:/YUZU/ryujinx-canary-1.3.351-win_x64"),
		"cemu":    pathExists("H:/Cemu"),
	}
	return res
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
