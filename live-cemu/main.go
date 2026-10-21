// CemuNavi（navicemu）—— Cemu 模拟器上《旷野之息》实时定位 + 进度同步。
//
// v1.3.0 收尾版：以 GameTools/cmunavi 的简单 trackLoop 为底座，
// 去掉旧版 poll/verify/watchdog 三 goroutine 竞态，固定偏移多块试读，
// 读 5 次失败才重找，站定不重定位。
//
// v1.3.2 应急复用版：对齐 xnavi v3.1.0 手机镜像能力 ——
// certs/ 有证书自动启用 https 双协议（手机防息屏）、根路径 302 跳镜像、
// 缺 app/ 地图时返回内置引导页。
//
// 用法：先开 Cemu 进游戏，再运行 navicemu.exe（默认 :8766，自动开在线地图）。

package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const buildVer = "v1.3.2"

const onlineMapURL = "https://botw.yalin.site/"

// 地图像素：mx = 2*gx + 12000, my = 2*gz + 10000
func gameToMap(gx, gz float64) (float64, float64) {
	return 2*gx + 12000, 2*gz + 10000
}

const (
	mapW       = 24000.0
	mapH       = 20000.0
	minTileMB  = 256
	shrineExit = 300.0 // |gx| 或 |gz| 超过此值 = 世界尺度，小于 = 神庙内部
)

type Target struct {
	Name string  `json:"name,omitempty"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Type string  `json:"type,omitempty"`
}

type Diag struct {
	Pid       uint32 `json:"pid"`
	Base      string `json:"base"`
	Off       string `json:"off"`
	Since     int64  `json:"sinceSec"`
	Reads     int64  `json:"reads"`
	Fails     int64  `json:"fails"`
	Relocates int64  `json:"relocates"`
	LastError string `json:"lastError"`
	UptimeSec int64  `json:"uptimeSec"`
	Version   string `json:"version"`
}

type State struct {
	mu        sync.Mutex
	OK        bool
	Verified  bool
	GX, GY, GZ float64 // GX=东, GY=高, GZ=南
	MX, MY    float64
	Target    *Target
	Source    string
	Diag      Diag
}

var (
	st        State
	rescanReq int32
	bootAt    = time.Now()
)

func setOffline(err string) {
	st.mu.Lock()
	st.OK = false
	st.Verified = false
	st.Source = "offline"
	if err != "" {
		st.Diag.LastError = err
	}
	st.mu.Unlock()
}

func trackLoop() {
	var seat *Seat
	fails := 0
	st.mu.Lock()
	st.Diag.Version = buildVer
	st.mu.Unlock()

	// 神庙模式：进神庙内部时把红点固定在入口（最后一个世界坐标），不追内部坐标。
	var shrineMode bool
	var shrinePin [3]float64 // (gx, alt, gz)
	var prevX, prevY, prevZ float64
	hasPrev := false

	for {
		if atomic.SwapInt32(&rescanReq, 0) == 1 {
			if seat != nil {
				seat.Close()
				seat = nil
			}
			fmt.Println("  [rescan] manual request, dropping current connection")
		}

		if seat == nil {
			s, msg := acquireSeat()
			if s == nil {
				setOffline(msg)
				fmt.Printf("  [wait] %s\n", msg)
				time.Sleep(2 * time.Second)
				continue
			}
			seat = s
			fails = 0
			st.mu.Lock()
			st.Diag.Pid = s.Pid
			st.Diag.Base = fmt.Sprintf("0x%X", s.Base)
			st.Diag.Off = fmt.Sprintf("0x%X", s.Off)
			st.Diag.Since = 0
			st.Diag.Relocates++
			st.Diag.LastError = ""
			st.Source = "fixed-offset"
			st.mu.Unlock()
			fmt.Printf("  [lock] %s\n", msg)
		}

		// Cemu 进程还在？
		alive := false
		for _, p := range findAllPid("Cemu") {
			if p == seat.Pid {
				alive = true
				break
			}
		}
		if !alive {
			fmt.Println("  [detach] Cemu exited, waiting for a new instance")
			seat.Close()
			seat = nil
			setOffline("Cemu exited")
			continue
		}

		x, o1 := f32BE(seat.H, seat.Base+seat.Off)
		y, o2 := f32BE(seat.H, seat.Base+seat.Off+4)
		z, o3 := f32BE(seat.H, seat.Base+seat.Off+8)
		if !o1 || !o2 || !o3 || !plausible(x, y, z) {
			fails++
		} else {
			fails = 0

			// ---- 神庙内部识别：世界尺度(|x|,|z|>300) 单步跳到内部尺度(<300) ----
			if !shrineMode && hasPrev &&
				abs64(x) < shrineExit && abs64(z) < shrineExit &&
				(abs64(prevX) > shrineExit || abs64(prevZ) > shrineExit) {
				shrineMode = true
				shrinePin = [3]float64{prevX, prevY, prevZ}
				fmt.Printf("  [shrine] entered interior (%.0f, %.0f) -> pin dot to entrance\n", x, z)
			}
			// 神庙模式：红点固定入口，忽略内部坐标
			if shrineMode {
				if abs64(x) > shrineExit || abs64(z) > shrineExit {
					fmt.Printf("  [shrine] back to world (%.0f, %.0f) -> normal tracking\n", x, z)
					shrineMode = false
				} else {
					x, y, z = shrinePin[0], shrinePin[1], shrinePin[2]
				}
			}

			mx, my := gameToMap(x, z)
			st.mu.Lock()
			was := st.OK
			st.OK = true
			st.Verified = true
			st.GX, st.GY, st.GZ = x, y, z
			st.MX, st.MY = mx, my
			st.Diag.Reads++
			st.Diag.LastError = ""
			st.mu.Unlock()
			if !was {
				fmt.Printf("  [move] X=%.1f Y=%.1f Z=%.1f -> map(%.0f,%.0f)\n", x, y, z, mx, my)
			}
		}

		if fails > 0 {
			st.mu.Lock()
			st.Diag.Fails++
			st.mu.Unlock()
			if fails >= 5 {
				fmt.Println("  [relocate] reads failing x5 -> re-locking (cutscene/loading?)")
				seat.Close()
				seat = nil
				fails = 0
				setOffline("read failed x5")
				time.Sleep(400 * time.Millisecond)
				continue
			}
			setOffline("read failed")
		}

		st.mu.Lock()
		st.Diag.Since = int64(time.Since(seat.Since).Seconds())
		st.Diag.UptimeSec = int64(time.Since(bootAt).Seconds())
		st.mu.Unlock()

		prevX, prevY, prevZ = x, y, z
		hasPrev = true
		time.Sleep(200 * time.Millisecond)
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// lanIPv4s 列出本机非回环 IPv4 地址（移动端局域网镜像 URL 用）
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
		addrs, err := ifa.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}

// defaultRouteIPv4 取本机出公网的默认路由 IPv4（UDP dial 不发包，仅选路）；
// 手机与 PC 同 WiFi 时，这个 IP 就是手机能直达的局域网地址。
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

// makeLanURL 构造移动端镜像 URL：http(s)://<IP>:<port>/botw/?follow=1&game=<game>
// v1.3.2：tlsMode 时输出 https（手机镜像页 secure context，wakeLock 防息屏生效）。
func makeLanURL(port int, game string, tlsMode bool) string {
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
	if tlsMode {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d/botw/?follow=1&game=%s", scheme, ip, port, game)
}

func pickPort(start int) (net.Listener, int) {
	for p := start; p < start+15; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err == nil {
			return l, p
		}
	}
	return nil, 0
}

func main() {
	fmt.Println("==================================================")
	fmt.Printf("  CemuNavi %s - Breath of the Wild (Cemu) live tracker\n", buildVer)
	fmt.Println("==================================================")

	// 参数解析必须先于 trackLoop/startProgress，否则手动存档路径在首次进度读取时不生效。
	certDir := "certs"
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--tls-cert-dir=") {
			certDir = strings.TrimPrefix(a, "--tls-cert-dir=")
		}
		if strings.HasPrefix(a, "--cemu-save-dir=") {
			cemuSaveDirOverride = strings.TrimPrefix(a, "--cemu-save-dir=")
			fmt.Printf("  [save] manual save dir: %s (top priority, isolated from auto-scan)\n", cemuSaveDirOverride)
		}
	}

	go trackLoop()
	startProgress()

	l, port := pickPort(8766)
	if l == nil {
		fmt.Println("[err] no free port in 8766..8780")
		time.Sleep(4 * time.Second)
		os.Exit(1)
	}

	// v1.3.2：exe 旁 certs/ 有证书（server.pem + server.key）→ 自动启用 https 双协议，
	// 手机镜像页获得 secure context（wakeLock 防息屏生效）；缺证书降级 http，定位照常。
	tlsMode := false
	if abs, err := filepath.Abs(certDir); err == nil {
		certDir = abs
	}
	cert, certErr := tls.LoadX509KeyPair(filepath.Join(certDir, "server.pem"), filepath.Join(certDir, "server.key"))

	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	fmt.Printf("  API: %s/pos\n", url)
	if port != 8766 {
		fmt.Println("  note: 8766 taken, using spare port (online map expects 8766)")
	}
	// v1.3.2 移动端镜像入口：手机/iPad 同一 WiFi 打开下列任一地址即可跟随红点+导航
	// 证书齐全时输出 https（与 /pos.lanUrl 一致，避免误导）
	scheme := "http"
	if certErr == nil {
		scheme = "https"
	}
	for _, ip := range lanIPv4s() {
		fmt.Printf("  LAN: %s://%s:%d/botw/?follow=1&game=botw\n", scheme, ip, port)
	}
	if certErr != nil {
		fmt.Println("  hint: certs not found - phone mirror runs http (screen wake lock off).")
		fmt.Println("        run certs\\gen-cert.ps1 once to enable https wake lock.")
	}
	fmt.Println("  Close this window to stop")
	fmt.Println("--------------------------------------------------")

	mux := newMux()
	if certErr == nil {
		// v1.3.2 双协议共存：同端口 http + https（按首字节 0x16 分派，对齐 xnavi v3.1.0）
		tlsMode = true
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		dl := newDualListener(l, tlsCfg)
		go dl.run()
		plainSrv := &http.Server{Handler: mux, ErrorLog: log.New(&errFilter{w: os.Stderr}, "", log.LstdFlags)}
		secureSrv := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
		go func() { _ = plainSrv.Serve(dl.plain) }()
		go func() { _ = secureSrv.Serve(dl.secure) }()
		fmt.Printf("  [tls] certs loaded -> http + https dual mode on :%d (phone wake lock ON)\n", port)
	} else {
		srv := &http.Server{Handler: mux}
		go func() { _ = srv.Serve(l) }()
	}
	lanURL = makeLanURL(port, "botw", tlsMode) // 手机镜像 URL（/pos.lanUrl 供网页二维码面板读取）

	time.Sleep(800 * time.Millisecond)
	if port == 8766 {
		fmt.Printf("  [ui] opening %s\n", onlineMapURL)
		exec.Command("cmd", "/c", "start", "", onlineMapURL).Start()
	} else {
		fmt.Printf("  [ui] spare port, open %s manually\n", url)
	}

	select {}
}

// ---- 双协议监听（v1.3.2，对齐 xnavi v3.1.0）：同端口 http + https 共存 ----
// 原理：Accept 后预读首字节；0x16（TLS ClientHello 首字节）→ TLS 通道，否则明文 HTTP 通道。
// 连接被立即关闭（如端口探测）→ 首字节读取失败 → 静默丢弃，不产生 TLS 握手错误刷屏日志。

// bufConn 保存预读缓冲的 bufio.Reader，首字节已在其中。
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// chanListener 从 channel 接收连接（http.Server.Serve 的 listener 接口）。
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

// dualListener 分派器：底层 tcp 监听 → 按首字节分派到明文/TLS 通道。
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
		c.Close() // 连接立即关闭（探测类）→ 静默丢弃
		return
	}
	if first[0] == 0x16 { // TLS ClientHello 首字节
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

// errFilter 过滤预期内的 TLS 握手错误日志，其余原样输出。
type errFilter struct{ w io.Writer }

func (f *errFilter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("TLS handshake error")) {
		return len(p), nil
	}
	return f.w.Write(p)
}
