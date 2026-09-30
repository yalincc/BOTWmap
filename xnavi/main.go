// xnavi — 通用定位导航程序（单一状态机，双游戏 × 双平台）。
//
// 用途：在本地为在线互动地图提供实时角色追踪：
//   BOTW https://botw.yalin.site/ ｜ TOTK https://totk.yalin.site/
// 原理：读模拟器进程内存定位玩家坐标 → HTTP API（127.0.0.1:8766）
// → 在线地图页跨域连接显示红点/轨迹/导航。
//
// 平台：auto（默认，Ryujinx 优先）| ryujinx | cemu —— 内存布局/字节序/存档
// 差异全部由平台驱动隔离；游戏：auto（默认）| botw | totk —— 坐标语义/存档/
// 进度/位面差异由游戏适配层隔离；定位核心（watch.go 单一状态机）两者都不感知。
//
// 用法：xnavi.exe [port] [--no-open] [--no-save] [--emu=auto|ryujinx|cemu]
//                [--game=auto|botw|totk] [--save-dir=...] [--dump]

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

	// 平台选择：显式指定 or 自动探测（先全平台选，游戏识别后再做 TOTK 平台修正）
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

	// 游戏选择：显式指定 or 游戏感知自动识别（auto 时读当前平台内存试固定偏移，
	// BOTW/TOTK 偏移互斥——用户实测 BOTW 偏移在 TOTK 内存全 invalid）
	switch gameName {
	case "totk":
		fmt.Println("  [game] explicit: totk")
		currentGame = &gameTotk{}
	case "botw":
		fmt.Println("  [game] explicit: botw")
		currentGame = &gameBotw{}
	default:
		currentGame = detectGame(currentPlatform())
	}

	// TOTK 平台修正：TOTK 仅 Ryujinx（Wii U 无 TOTK）。
	// auto 检测到 TOTK 时平台必为 Ryujinx（TOTK 偏移只存在于 Ryujinx 内存），此分支
	// 覆盖显式 --game=totk 配 auto 平台选中 Cemu（如 Cemu 正跑 BOTW）的情况。
	if currentGame.Name() == "totk" {
		if p := currentPlatform(); p != nil && p.Name() == "Cemu" {
			fmt.Println("  !! TOTK 仅 Ryujinx：平台强制切换 Ryujinx")
			setPlatform(&rp)
		}
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
// 两步判定：
//   1) 游戏感知（当前平台内存）：试读固定偏移——BOTW 偏移有效 → BOTW；
//      TOTK 偏移多副本共识 ≥2 → TOTK。两游戏偏移互斥（实测 BOTW 偏移在 TOTK 内存全 invalid），
//      识别可靠，且玩家在两个存档并存时不会被存档目录误导。
//   2) 存档兜底（游戏未运行时）：存在 TOTK 存档且无 BOTW 存档 → TOTK，否则 BOTW。
func detectGame(p Platform) Game {
	if p != nil && p.Attach() {
		h := p.Handle()
		blks := p.Blocks(256.0)
		if len(blks) > 0 {
			// BOTW：Ryujinx 0xA77FCBBC / Cemu 0x1055300C（0xC1F8BF4 备用）
			botwOffs := []uintptr{0xA77FCBBC, 0x1055300C, 0xC1F8BF4}
			gb := &gameBotw{}
			for _, blk := range blks {
				for _, off := range botwOffs {
					if d := gb.DecodeAt(h, blk.Base+off); d != nil {
						fmt.Printf("  [game] auto-detected: botw (fixed offset live @0x%X)\n", blk.Base+off)
						return gb
					}
				}
			}
			// TOTK：10 偏移共识 ≥2（PickOffset 拒绝杂散）
			gt := &gameTotk{}
			var hits []OffsetCand
			for _, off := range gt.KnownOffsets(p) {
				for _, blk := range blks {
					addr := blk.Base + off
					if d := gt.DecodeAt(h, addr); d != nil {
						hits = append(hits, OffsetCand{addr, [3]float32{d[0], d[1], d[2]}})
					}
				}
			}
			if _, _, ok := gt.PickOffset(hits, p); ok {
				fmt.Printf("  [game] auto-detected: totk (fixed offset consensus)\n")
				return gt
			}
		}
	}
	// 存档兜底
	root := ""
	if app := os.Getenv("APPDATA"); app != "" {
		root = filepath.Join(app, "Ryujinx", "bis", "user", "save")
	}
	if root != "" {
		totk := len(findSaveFiles(root, []string{"progress.sav", "caption.sav"})) > 0
		botw := len(findSaveFiles(root, []string{"game_data.sav"})) > 0
		if totk && !botw {
			fmt.Println("  [game] auto-detected: totk (save dir)")
			return &gameTotk{}
		}
	}
	fmt.Println("  [game] auto-detected: botw")
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
