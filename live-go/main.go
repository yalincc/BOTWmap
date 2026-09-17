// BotwNavi（BOTW live 追踪服务，Go 版）
//
// 用途：在本地为在线互动地图 https://botw.yalin.site/ 提供实时角色追踪。
// 原理：读 Ryujinx 模拟器进程内存定位玩家坐标 → HTTP API（127.0.0.1:8766）
// → 在线地图页跨域连接显示红点/轨迹/导航。
//
// 用法：botwlive.exe [port] [--no-open] [--no-save]

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const mapURL = "https://botw.yalin.site/"

func main() {
	setConsoleTitle("BotwNavi · BOTW Live")

	port := 8766
	autoOpen := true
	useSave := true
	for _, a := range os.Args[1:] {
		switch {
		case a == "--no-open":
			autoOpen = false
		case a == "--no-save":
			useSave = false
		case strings.HasPrefix(a, "--"):
			// ignore
		default:
			if n, err := strconv.Atoi(a); err == nil {
				port = n
			}
		}
	}

	fmt.Println("=" + strings.Repeat("=", 61))
	fmt.Println(" BotwNavi · BOTW live - auto locating player position")
	fmt.Println("=" + strings.Repeat("=", 61))

	pid := findPid("ryujinx")
	if pid != 0 {
		h, err := openProcess(pid)
		if err != nil || h == 0 {
			fmt.Printf("OpenProcess failed. Try running as administrator.\n")
			return
		}
		procHandle, procPID = h, pid
		fmt.Printf("  pid = %d\n", pid)
	} else {
		fmt.Println("  Ryujinx not running yet - the watchdog attaches as soon as it appears.")
	}

	loadConfig()
	progress = newProgressWatcher("")
	go progress.loop()

	fmt.Println("  stage 0: remembered offsets (no scan needed) ...")
	if a, v, ok := tryOffsets(); ok {
		lock.mu.Lock()
		lock.addr = a
		lock.verified = false
		lock.copies = 0
		lock.source = "known"
		lock.mu.Unlock()
		fmt.Printf("  address 0x%X -> mem=(%.1f, %.1f, %.1f)   [fast path, no scan]\n", a, v[0], v[1], v[2])
		fmt.Println("  it is marked verified as soon as the value moves.")
	} else if reopenProcess() {
		fmt.Println("  stage 1: save anchor + anchored scan ...")
		if !(useSave && relocalize("remembered addresses unusable")) {
			fmt.Println("  stage 2: motion scan (walk a few steps when prompted)")
			if a2 := locateByMotion(); a2 != 0 {
				lock.mu.Lock()
				lock.addr = a2
				lock.verified = true
				lock.copies = 0
				lock.source = "motion"
				lock.mu.Unlock()
				saveKnown(append([]uintptr{a2}, loadKnownAddrs()...), guestBase)
			} else {
				fmt.Println("  no luck yet - serving anyway, the watchdog keeps trying.")
			}
		}
	} else {
		fmt.Println("  Ryujinx is not running yet - serving anyway.")
		fmt.Println("  Map + collection progress work now; the live dot starts as")
		fmt.Println("  soon as the emulator appears (watchdog, no restart needed).")
	}

	go poll()
	go watchdog()
	if a0 := lockAddr(); a0 != 0 {
		go verifyKnown(a0)
	}

	time.Sleep(500 * time.Millisecond)

	state.mu.RLock()
	st, _ := json.Marshal(map[string]any{
		"ok": state.ok, "gx": state.gx, "gy": state.gy, "gz": state.gz,
		"mx": state.mx, "my": state.my, "layer": state.layer,
		"age": state.age, "verified": state.verified, "copies": state.copies,
		"source": state.source,
	})
	state.mu.RUnlock()
	fmt.Println("")
	fmt.Printf("  serving -> %s\n", st)
	fmt.Printf("  live map -> %s\n", mapURL)
	fmt.Println("  (Ctrl+C to stop)")

	if autoOpen {
		openBrowser(mapURL)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("  listen %s failed: %v\n", addr, err)
		os.Exit(1)
	}
	fmt.Printf("  API -> http://%s/pos\n", addr)
	http.Serve(ln, &server{})
}

func lockAddr() uintptr {
	lock.mu.RLock()
	defer lock.mu.RUnlock()
	return lock.addr
}

func openBrowser(url string) {
	// 用默认浏览器打开
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	if err := cmd.Start(); err != nil {
		fmt.Printf("  (could not open browser: %v)\n", err)
	}
}
