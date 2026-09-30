// CemuNavi（navicemu）—— Cemu 模拟器上《旷野之息》实时定位 + 进度同步。
//
// v1.3.0 收尾版：以 GameTools/cmunavi 的简单 trackLoop 为底座，
// 去掉旧版 poll/verify/watchdog 三 goroutine 竞态，固定偏移多块试读，
// 读 5 次失败才重找，站定不重定位。
//
// 用法：先开 Cemu 进游戏，再运行 navicemu.exe（默认 :8766，自动开在线地图）。

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const buildVer = "v1.3.0"

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

	go trackLoop()
	startProgress()

	l, port := pickPort(8766)
	if l == nil {
		fmt.Println("[err] no free port in 8766..8780")
		time.Sleep(4 * time.Second)
		os.Exit(1)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	fmt.Printf("  API: %s/pos\n", url)
	if port != 8766 {
		fmt.Println("  note: 8766 taken, using spare port (online map expects 8766)")
	}
	fmt.Println("  Close this window to stop")
	fmt.Println("--------------------------------------------------")

	mux := newMux()
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()

	time.Sleep(800 * time.Millisecond)
	if port == 8766 {
		fmt.Printf("  [ui] opening %s\n", onlineMapURL)
		exec.Command("cmd", "/c", "start", "", onlineMapURL).Start()
	} else {
		fmt.Printf("  [ui] spare port, open %s manually\n", url)
	}

	select {}
}
