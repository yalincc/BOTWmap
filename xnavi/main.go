// xnavi — BOTW 定位导航程序（单一状态机，双平台）。
//
// 用途：在本地为在线互动地图提供实时角色追踪：
//   BOTW https://botw.yalin.site/
// 原理：读模拟器进程内存定位玩家坐标 → HTTP API（127.0.0.1:8766）
// → 在线地图页跨域连接显示红点/轨迹/导航。
//
// 平台：auto（默认，Ryujinx 优先）| ryujinx | cemu —— 内存布局/字节序/存档
// 差异全部由平台驱动隔离；游戏：auto（默认）| botw —— 坐标语义/存档/
// 进度差异由游戏适配层隔离；定位核心（watch.go 单一状态机）不感知平台。
// （TOTK 已于 2026-10-02 拆分到 TOTKmap/live-go 独立工程，见交接文档。）
//
// 用法：xnavi.exe [port] [--no-open] [--no-save] [--emu=auto|ryujinx|cemu]
//                [--game=auto|botw] [--save-dir=...] [--dump]

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

func main() {
	enableLogTimestamps()
	port := 8766
	autoOpen := true
	useSave := true
	emu := "auto"
	gameName := "auto"
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
		case strings.HasPrefix(a, "--game="):
			gameName = strings.TrimPrefix(a, "--game=")
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

	// 平台选择：显式指定 or 自动探测（先全平台选，再按实际进程切换）
	var rp ryujinxPlatform
	var cp cemuPlatform
	switch emu {
	case "ryujinx":
		setPlatform(&rp)
	case "cemu":
		setPlatform(&cp)
	default: // auto
		// 游戏感知选择：优先"进程在跑且已加载游戏大块内存（≥256MB）"的平台，
		// 解决 Ryujinx 进程活着但游戏已关时启动永远选不中 Cemu 的问题；
		// 都没加载游戏则退回按进程存在选（Ryujinx 优先），后续状态机 0 块检测会再切。
		autoPlatform = true
		switch {
		case rp.Attach() && len(rp.Blocks(256.0)) > 0:
			setPlatform(&rp)
		case cp.Attach() && len(cp.Blocks(256.0)) > 0:
			setPlatform(&cp)
		case rp.Attach():
			setPlatform(&rp)
		case cp.Attach():
			setPlatform(&cp)
		default:
			setPlatform(&rp) // 都没在跑：默认 Ryujinx，状态机等它出现
		}
	}

	// 游戏选择：xnavi 只服务 BOTW（TOTK 已移交 live-go），仍接受显式 --game=botw。
	switch gameName {
	case "botw":
		fmt.Println("  [game] explicit: botw")
		currentGame = &gameBotw{}
	default:
		currentGame = detectGame(currentPlatform())
	}

	setConsoleTitle("xnavi · " + strings.ToUpper(currentGame.Name()) + " Live")

	fmt.Println("=" + strings.Repeat("=", 61))
	fmt.Printf(" xnavi · %s live - auto locating player position\n", strings.ToUpper(currentGame.Name()))
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
	// 进度 watcher：BOTW 只认 game_data.sav
	// （TOTK 的 progress.sav 口径已随 TOTK 一并移交 live-go）
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
	fmt.Printf("  live map -> %s\n", currentGame.MapURL())
	fmt.Println("  (Ctrl+C to stop)")

	if autoOpen {
		openBrowser(currentGame.MapURL())
	}

	select {}
}

// detectGame 自动识别当前游戏（--game=auto）。
// xnavi 只服务 BOTW，此函数用于确认当前运行的游戏确实是 BOTW：
//   1) 窗口标题判定（Q8 主判定）：模拟器自报的硬标识，零内存扫描；
//      若识别到 TOTK，明确提示用户改用 live-go（TOTKmap 项目）。
//   2) 内存感知（重试 3 次）：BOTW 任一已知偏移读出合法坐标 → BOTW。
//      读数有瞬时性（传送/加载瞬间全 invalid），一次失败不能下结论。
func detectGame(p Platform) Game {
	if p != nil && p.Attach() {
		h := p.Handle()
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Second)
			}
			// 0) 窗口标题判定（Q8 主判定）：模拟器自报的硬标识，零内存扫描
			if game := detectGameByTitle(p.PID()); game != "" {
				if game == "totk" {
					// xnavi 只服务 BOTW；检测到 TOTK 就明确提示，
					// 免得用户对着一个永远锁不上的界面找原因
					fmt.Println("  [game] 检测到 TOTK：本程序只服务 BOTW，TOTK 请改用 live-go（TOTKmap 项目）")
				} else {
					fmt.Printf("  [game] auto-detected: botw (window title TID)\n")
					return &gameBotw{}
				}
			}
			blks := p.Blocks(256.0)
			if len(blks) == 0 {
				continue
			}
			// BOTW：任一已知偏移读出合法坐标 → BOTW
			gb := &gameBotw{}
			botwOffs := []uintptr{0xA77FCBBC, 0x1055300C, 0xC1F8BF4}
			for _, blk := range blks {
				for _, off := range botwOffs {
					if d := gb.DecodeAt(h, blk.Base+off); d != nil {
						fmt.Printf("  [game] auto-detected: botw (fixed offset live @0x%X)\n", blk.Base+off)
						return gb
					}
				}
			}
		}
	}
	// xnavi 只服务 BOTW，不再做"存档兜底猜游戏"——那套逻辑当初是为了在
	// BOTW / TOTK 之间做选择，现在没有第二个游戏可选了。
	fmt.Println("  [game] auto-detected: botw (default)")
	return &gameBotw{}
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
