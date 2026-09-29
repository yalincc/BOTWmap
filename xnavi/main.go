// xnavi — BOTW 通用定位导航程序（单一状态机，Ryujinx/Cemu 双平台）。
//
// 用途：在本地为在线互动地图 https://botw.yalin.site/ 提供实时角色追踪。
// 原理：读模拟器进程内存定位玩家坐标 → HTTP API（127.0.0.1:8766）
// → 在线地图页跨域连接显示红点/轨迹/导航。
//
// 平台：auto（默认，Ryujinx 优先）| ryujinx | cemu —— 内存布局/字节序/存档
// 差异全部由平台驱动隔离，定位核心（watch.go 单一状态机）不感知。
//
// 用法：xnavi.exe [port] [--no-open] [--no-save] [--emu=auto|ryujinx|cemu] [--dump]

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
	port := 8766
	autoOpen := true
	useSave := true
	emu := "auto"
	dump := false
	saveDirOverride := ""
	for _, a := range os.Args[1:] {
		switch {
		case a == "--no-open":
			autoOpen = false
		case a == "--no-save":
			useSave = false
		case a == "--dump":
			dump = true
		case strings.HasPrefix(a, "--emu="):
			emu = strings.TrimPrefix(a, "--emu=")
		case strings.HasPrefix(a, "--save-dir="):
			saveDirOverride = strings.TrimPrefix(a, "--save-dir=")
			setSaveDirOverride(saveDirOverride)
		case strings.HasPrefix(a, "--"):
			// ignore
		default:
			if n, err := strconv.Atoi(a); err == nil {
				port = n
			}
		}
	}

	// 平台选择：显式指定 or 自动探测（Ryujinx 优先，BOTW 主模拟器；其次 Cemu）
	var rp ryujinxPlatform
	var cp cemuPlatform
	switch emu {
	case "ryujinx":
		setPlatform(&rp)
	case "cemu":
		setPlatform(&cp)
	default: // auto
		if rp.Attach() {
			setPlatform(&rp)
		} else if cp.Attach() {
			setPlatform(&cp)
		} else {
			setPlatform(&rp) // 都没在跑：默认 Ryujinx，状态机等它出现
		}
	}

	setConsoleTitle("xnavi · BOTW Live")

	fmt.Println("=" + strings.Repeat("=", 61))
	fmt.Println(" xnavi · BOTW live - auto locating player position")
	fmt.Println("=" + strings.Repeat("=", 61))

	if !useSave {
		useSaveScan = false
	}

	// ---- --dump 诊断模式：只打印内存信息与定位结果，不启服务 ----
	if dump {
		dumpDiagnostics()
		return
	}

	loadConfig()
	progress = newProgressWatcher()
	go progress.loop()

	// 服务器先于定位启动（踩坑固化：网页端要能第一时间连上）
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// 端口占用/防火墙拦截：不阻塞定位（ds 双入口原则——GUI 与网页端独立）。
		// 定位与状态照常，仅网页端不可用；GUI/日志仍可经 status.json + run-events.log 工作。
		fmt.Printf("  !! HTTP %s failed: %v\n", addr, err)
		fmt.Println("  !! HTTP 端口被占用：网页端不可用，定位导航照常运行")
	} else {
		fmt.Printf("  API -> http://%s/pos\n", addr)
		go http.Serve(ln, &server{})
	}

	// 单一状态机：唯一写 lock.addr 的 goroutine
	go stateMachine()
	go statusLoop()

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

	select {}
}

// dumpDiagnostics 打印当前平台进程/内存/定位诊断信息（联调用，不开服务器）。
func dumpDiagnostics() {
	p := currentPlatform()
	if p == nil {
		fmt.Println("  [dump] no platform selected")
		return
	}
	if !p.Attach() {
		fmt.Println("  [dump] emulator not running - nothing to dump")
		return
	}
	h := p.Handle()
	fmt.Printf("  [dump] platform: %s pid=%d\n", p.Name(), p.PID())
	fmt.Println("  --- memory blocks (committed RW, >=16MB) ---")
	blocks := p.Blocks(16.0)
	for _, b := range blocks {
		fmt.Printf("    block 0x%012X  %8.0f MB\n", b.Base, float64(b.Size)/1048576.0)
	}
	anchor, anchorSize := p.LargestBlock(0)
	fmt.Printf("  [dump] anchor (largest block) = 0x%012X  (%8.0f MB)\n", anchor, float64(anchorSize)/1048576.0)

	// Cemu 特有：memory_base 反推 + 社区链探针
	if p.Name() == "Cemu" {
		base, mem2, ok := findCemuBase(h)
		if !ok {
			fmt.Println("  [dump] memory_base NOT found (chains skipped; structural scan still works)")
		} else {
			fmt.Printf("  [dump] memory_base = 0x%012X  MEM2 host = 0x%012X\n", base, mem2)
			for _, c := range cemuChains {
				va, rok := resolveChain(h, base, c)
				if rok {
					fmt.Printf("  [dump] chain %-8s resolve OK  actor=0x%012X (host 0x%012X)\n", c.Name, va, base+va)
				} else {
					fmt.Printf("  [dump] chain %-8s resolve failed\n", c.Name)
				}
			}
			if res := chainLocate(h, base); res != nil {
				fmt.Printf("  [dump] chainLocate -> slot 0x%012X mem=(%.1f, %.1f, %.1f)\n",
					res.Addr, res.Hud[0], res.Hud[1], res.Hud[2])
			} else {
				fmt.Println("  [dump] chainLocate: no slot candidate")
			}
		}
	}

	// 结构扫描 top 组
	fmt.Println("  [dump] structural scan (top groups) ...")
	big := p.Blocks(minBlockMB)
	hits := structuralScan(h, big, nil)
	if len(hits) > 0 {
		groups, _ := groupAndRank(h, hits)
		for i := 0; i < len(groups) && i < 10; i++ {
			g := groups[i]
			fmt.Printf("    copies=%d struct=%d  mem=(%.1f, %.1f, %.1f)  ex0x%012X\n",
				g.Copies, g.Struct, g.X, g.Y, g.Z, g.Addrs[0])
		}
	} else {
		fmt.Println("  [dump] no structural candidates")
	}

	// remembered offsets 当前是否可用（锚 = 最大块）
	if anchor != 0 {
		for _, o := range loadOffsets() {
			if d := decodeTripleAt(h, anchor+o); d != nil {
				fmt.Printf("  [dump] known offset 0x%012X -> mem=(%.1f, %.1f, %.1f)\n", o, d[0], d[1], d[2])
			} else {
				fmt.Printf("  [dump] known offset 0x%012X -> unreadable\n", o)
			}
		}
	}
	fmt.Println("  [dump] done")
}

func openBrowser(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	if err := cmd.Start(); err != nil {
		fmt.Printf("  (could not open browser: %v)\n", err)
	}
}
