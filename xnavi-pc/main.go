// xnavi-pc — Savage Wind（BOTW PC 重编译版）定位导航，单文件极简版。
//
// 定位：只读游戏进程内存，固定偏移直读 12 字节；零全内存扫描、零移动确认。
// 契约：与 xnavi 完全一致，网页 app/ 与存档进度体系无需改动。
// 边界：只读内存、只读存档，绝不写入游戏目录，不注入。

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CoreVersion 遵循发布版本号（与模拟器版一致）。
const CoreVersion = "v3.3.0"

var (
	loc         *locator
	primaryPort int
	tlsMode     bool
	watchMode   bool
	noOpen      bool
	portBase    = 8766
	manualPID   uint32
)

func main() {
	certDir := "certs"
	for _, a := range os.Args[1:] {
		switch {
		case a == "--tls":
			tlsMode = true
		case strings.HasPrefix(a, "--tls-cert-dir="):
			certDir = strings.TrimPrefix(a, "--tls-cert-dir=")
		case strings.HasPrefix(a, "--port="):
			if n, err := strconv.Atoi(strings.TrimPrefix(a, "--port=")); err == nil && n > 0 {
				portBase = n
			}
		case strings.HasPrefix(a, "--save-dir="):
			setSaveDirOverride(strings.TrimPrefix(a, "--save-dir="))
		// xnavi-gui 启动核心时会传下面这些参数。PC 版没有模拟器平台之分，
		// 全部接受并做合理映射，避免 GUI 报「参数不支持」或存档路径不生效。
		case strings.HasPrefix(a, "--cemu-save-dir="),
			strings.HasPrefix(a, "--ryujinx-save-dir="),
			strings.HasPrefix(a, "--eden-save-dir="):
			if v := a[strings.IndexByte(a, '=')+1:]; v != "" {
				setSaveDirOverride(v)
			}
		case strings.HasPrefix(a, "--emu="), strings.HasPrefix(a, "--game="):
			// PC 版固定 BOTW / Savage Wind，忽略平台与游戏选择
		case strings.HasPrefix(a, "--pid="):
			if n, err := strconv.ParseUint(strings.TrimPrefix(a, "--pid="), 10, 32); err == nil && n > 0 {
				manualPID = uint32(n)
			}
		case a == "--no-open":
			noOpen = true
		case a == "--watch":
			watchMode = true
		case a == "--debug":
			debugProbe.Store(true)
		case a == "-h" || a == "--help":
			printHelp()
			return
		}
	}

	fmt.Println("==============================================")
	fmt.Println("  xnavi-pc · 旷野之息 PC 移植版 定位导航")
	fmt.Println("  只读内存 / 不修改游戏 / 不改存档")
	fmt.Println("==============================================")

	loc = newLocator("savagewind.exe")
	go saves.loop()
	go statusLoop()
	go locateLoop()
	if watchMode {
		go watchLoop()
	}

	h := &apiServer{}
	tlsCfg := loadTLSCfgIfEnabled(tlsMode, certDir)
	if tlsCfg == nil {
		tlsMode = false
	}
	owned, skipped := listenOwnedPorts(portBase, tlsCfg, h)
	if len(owned) == 0 {
		fmt.Printf("  !! 端口 %d~%d 全部被占用，请先关闭其它导航程序后重试\n", portBase, portBase+4)
		fmt.Println("  按回车键退出…")
		fmt.Scanln()
		return
	}
	primaryPort = owned[0]
	lanURL = makeLanURL(primaryPort, "botw", tlsMode)

	scheme := "http"
	if tlsMode {
		scheme = "http/https"
	}
	fmt.Printf("  [http] 端口 %s（跳过被占: %v）· 协议 %s\n", joinInts(owned), skipped, scheme)
	if lanURL != "" {
		fmt.Printf("  [手机镜像] %s\n", lanURL)
	}
	if fs := resolveBotwFS(); fs == nil {
		fmt.Println("  [提示] 未找到 app/，手机镜像将显示引导页；把地图 app/ 放在本程序同目录即可")
	}
	fmt.Printf("  [状态页] http://127.0.0.1:%d/status\n", primaryPort)

	if !noOpen {
		url := fmt.Sprintf("http://127.0.0.1:%d/status", primaryPort)
		if resolveBotwFS() != nil {
			url = fmt.Sprintf("http://127.0.0.1:%d/botw/?follow=1&game=botw", primaryPort)
		}
		openBrowser(url)
	}

	select {} // 服务运行在后台 goroutine
}

func printHelp() {
	fmt.Println(`用法: xnavi-pc.exe [选项]

  --port=N          起始端口（默认 8766，自动顺延到 8770）
  --save-dir=DIR    手动指定 Savage Wind 存档目录
  --pid=N           手动指定游戏进程 PID（自动发现失败时使用）
  --tls             启用同端口 http+https（需 certs/server.pem 与 server.key）
  --tls-cert-dir=   证书目录（默认 certs）
  --no-open         启动时不自动打开浏览器
  --watch           控制台实时打印坐标
  -h / --help       显示帮助

默认自动探测：savagewind.exe 进程 → 同目录 save/ → 主档 game_data.sav`)
}

func locateLoop() {
	for {
		loc.tick()
		time.Sleep(200 * time.Millisecond)
	}
}

func watchLoop() {
	var last string
	for {
		st := loc.snapshot()
		line := fmt.Sprintf("X %.1f  Y %.1f  Z %.1f", st.GX, st.GY, st.GZ)
		if st.OK && line != last {
			fmt.Printf("  [pos] %s  (锁定偏移 0x%X)\n", line, st.Offset)
			last = line
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ---- status.json（运行时健康快照，GUI/诊断可读；发布包禁带） ----
func statusLoop() {
	for {
		writeStatusFile()
		time.Sleep(2 * time.Second)
	}
}

func writeStatusFile() {
	st := loc.snapshot()
	obj := map[string]any{
		"time":      time.Now().Format("2006-01-02 15:04:05"),
		// GUI 会话签名：xnavi-gui 用它确认快照来自本次拉起的核心，缺失会一直显示「启动中」。
		"corePid":   os.Getpid(),
		"pid":       st.PID,
		"ok":        st.OK,
		"game":      "botw",
		"emulator":  "Savage Wind",
		"mode":      "PC-direct",
		"lockOffset": fmt.Sprintf("0x%X", st.Offset),
		"verified":  st.Verified,
		"source":    st.Source,
		"layer":     0,
		"note":      st.Note,
		"rescans":   st.Rescans,
		"port":      primaryPort,
		"ver":       CoreVersion,
		"lanUrl":    lanURL,
		"pos": map[string]any{
			"gx": st.GX, "gy": st.GY, "gz": st.GZ,
			"mx": st.MX, "my": st.MY,
		},
		"progress": saves.summarize(),
	}
	buf, _ := json.MarshalIndent(obj, "", " ")
	dir := workDir()
	tmp := filepath.Join(dir, "status.json.tmp")
	final := filepath.Join(dir, "status.json")
	if os.WriteFile(tmp, buf, 0644) == nil {
		os.Rename(tmp, final)
	}
}

func workDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// ---- 浏览器 / 局域网 URL ----
func openBrowser(url string) {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		fmt.Printf("  (无法自动打开浏览器: %v)\n", err)
	}
}

func defaultRouteIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if la, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return la.IP.String()
	}
	return ""
}

func lanIPv4s() []string {
	var out []string
	ifas, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifa := range ifas {
		if ifa.Flags&net.FlagUp == 0 || ifa.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifa.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLoopback() && ip.To4() != nil {
				out = append(out, ip.String())
			}
		}
	}
	return out
}

func makeLanURL(port int, game string, secure bool) string {
	ip := defaultRouteIPv4()
	if ip == "" {
		if ips := lanIPv4s(); len(ips) > 0 {
			ip = ips[0]
		}
	}
	if ip == "" {
		return ""
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d/botw/?follow=1&game=%s", scheme, ip, port, game)
}

// ---- 双协议监听（同端口 http + https 共存） ----
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

type chanListener struct {
	addr net.Addr
	ch   chan net.Conn
	done chan struct{}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *chanListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *chanListener) Addr() net.Addr { return l.addr }

const soExclusiveAddrUse = -5

func exclusiveAddrControl(network, address string, c syscall.RawConn) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, soExclusiveAddrUse, 1)
	}); err != nil {
		return err
	}
	return serr
}

func loadTLSCfgIfEnabled(enabled bool, certDir string) *tls.Config {
	if !enabled {
		return nil
	}
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(certDir, "server.pem"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		fmt.Printf("  !! --tls 证书缺失/无效（%s），已降级 http 模式\n", certDir)
		return nil
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}

type errFilter struct{ w io.Writer }

func (f *errFilter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("TLS handshake error")) {
		return len(p), nil
	}
	return f.w.Write(p)
}

func listenOwnedPorts(base int, tlsCfg *tls.Config, h http.Handler) (owned []int, skipped []int) {
	lc := net.ListenConfig{Control: exclusiveAddrControl}
	for p := base; p < base+5; p++ {
		ln, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			skipped = append(skipped, p)
			continue
		}
		owned = append(owned, p)
		if tlsCfg != nil {
			dl := newDualListener(ln, tlsCfg)
			go dl.run()
			go (&http.Server{Handler: h, ErrorLog: log.New(io.Discard, "", 0)}).Serve(dl.secure)
			go (&http.Server{Handler: h, ErrorLog: log.New(&errFilter{w: os.Stderr}, "", log.LstdFlags)}).Serve(dl.plain)
		} else {
			go http.Serve(ln, h)
		}
	}
	return owned, skipped
}

type dualListener struct {
	ln     net.Listener
	tlsCfg *tls.Config
	plain  *chanListener
	secure *chanListener
}

func newDualListener(ln net.Listener, tlsCfg *tls.Config) *dualListener {
	return &dualListener{
		ln:     ln,
		tlsCfg: tlsCfg,
		plain:  &chanListener{addr: ln.Addr(), ch: make(chan net.Conn, 16), done: make(chan struct{})},
		secure: &chanListener{addr: ln.Addr(), ch: make(chan net.Conn, 16), done: make(chan struct{})},
	}
}

func (d *dualListener) run() {
	for {
		c, err := d.ln.Accept()
		if err != nil {
			d.plain.Close()
			d.secure.Close()
			return
		}
		go d.dispatch(c)
	}
}

func (d *dualListener) dispatch(c net.Conn) {
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		c.Close()
		return
	}
	if first[0] == 0x16 {
		select {
		case d.secure.ch <- tls.Server(&bufConn{Conn: c, r: br}, d.tlsCfg):
		default:
			c.Close()
		}
		return
	}
	select {
	case d.plain.ch <- &bufConn{Conn: c, r: br}:
	default:
		c.Close()
	}
}

func joinInts(ps []int) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, " ")
}
