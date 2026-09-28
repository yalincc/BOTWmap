// CemuNavi（live-cemu）：为《旷野之息》互动地图 https://botw.yalin.site/ 提供
// Cemu（Wii U 模拟器）版实时角色追踪。
//
// 原理：读 Cemu 进程内存（guest 地址 = memory_base + 偏移，PowerPC 大端），
// 用社区指针链/结构扫描定位玩家坐标 → HTTP API（127.0.0.1:8766）
// → 在线地图页跨域连接显示红点/轨迹/导航。
//
// 用法：navicemu.exe [port] [--no-open] [--no-scan] [--dump]

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
	setConsoleTitle("CemuNavi · BOTW Live (Cemu)")

	port := 8766
	autoOpen := true
	allowScan := true
	dump := false
	for _, a := range os.Args[1:] {
		switch {
		case a == "--no-open":
			autoOpen = false
		case a == "--no-scan":
			allowScan = false
		case a == "--dump":
			dump = true
		case strings.HasPrefix(a, "--"):
			// ignore
		default:
			if n, err := strconv.Atoi(a); err == nil {
				port = n
			}
		}
	}

	fmt.Println("=" + strings.Repeat("=", 61))
	fmt.Println(" CemuNavi · BOTW Live (Cemu emulator)")
	fmt.Println("=" + strings.Repeat("=", 61))

	pid, h := findLiveCemu()
	if pid != 0 && h != 0 {
		procHandle, procPID = h, pid
		fmt.Printf("  pid = %d\n", pid)
	} else {
		fmt.Println("  Cemu not running yet - the watchdog attaches as soon as it appears.")
	}

	// ---- --dump 诊断模式：只打印内存信息与定位结果，不启服务 ----
	if dump {
		dumpDiagnostics()
		return
	}

	loadConfig()
	progress = newProgressWatcher()
	go progress.loop()

		addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("  listen %s failed: %v\n", addr, err)
		os.Exit(1)
	}
	fmt.Printf("  API -> http://%s/pos\n", addr)
	go http.Serve(ln, &server{})

	fmt.Println("  stage 1: remembered offsets (no scan needed) ...")
	if a, v, ok := tryOffsets(); ok {
		lock.mu.Lock()
		lock.addr = a
		lock.verified = false
		lock.copies = 0
		lock.source = "known"
		lock.mu.Unlock()
		fmt.Printf("  address 0x%X -> mem=(%.1f, %.1f, %.1f)   [fast path, no scan]\n", a, v[0], v[1], v[2])
		fmt.Println("  it is verified as soon as the value reads sane.")
		go verifyKnown(a)
	} else if reopenProcess() && allowScan {
		fmt.Println("  stage 2: community chain -> motion scan fallback ...")
		if !relocalize("remembered addresses unusable") {
			fmt.Println("  no luck yet - serving anyway, the watchdog keeps trying.")
		}
	} else {
		fmt.Println("  Cemu is not running yet - serving anyway.")
		fmt.Println("  Map works now; the live dot starts as soon as the emulator")
		fmt.Println("  appears (watchdog, no restart needed).")
	}

	go poll()
	go watchdog()
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

// dumpDiagnostics 打印 Cemu 进程/内存/定位诊断信息（联调用，不开服务器）。
func dumpDiagnostics() {
	if procHandle == 0 {
		fmt.Println("  [dump] Cemu not running - nothing to dump")
		return
	}
	h := procHandle
	fmt.Println("  --- memory blocks (committed RW, >=16MB) ---")
	blocks := rwBlocks(h, 16.0)
	for _, b := range blocks {
		fmt.Printf("    block 0x%012X  %8.0f MB\n", b.Base, float64(b.Size)/1048576.0)
	}
	base, mem2, ok := findCemuBase(h)
	anchor, anchorSize := largestRWBlock(h)
	fmt.Printf("  [dump] anchor (largest RW block) = 0x%012X  (%8.0f MB)\n", anchor, float64(anchorSize)/1048576.0)
	if !ok {
		fmt.Println("  [dump] memory_base NOT found (chains will be skipped; motion scan still works)")
	} else {
		fmt.Printf("  [dump] memory_base = 0x%012X  MEM2 host = 0x%012X\n", base, mem2)
		guestBase.Store(base)
		for _, c := range cemuChains {
			va, rok := resolveChain(h, base, c)
			fmt.Printf("  [dump] chain %-8s root=0x%012X resolve=%v", c.Name, c.Root, rok)
			if rok {
				fmt.Printf("  actor=0x%012X (host 0x%012X)\n", va, base+va)
			} else {
				fmt.Println()
			}
		}
		if res := chainLocate(h, base); res != nil {
			fmt.Printf("  [dump] chainLocate -> slot 0x%012X mem=(%.1f, %.1f, %.1f)\n",
				res.Addr, res.Hud[0], res.Hud[1], res.Hud[2])
		} else {
			fmt.Println("  [dump] chainLocate: no slot candidate")
		}
	}

	// 链根探针：候选基址 × 社区根地址，校准本 build 的链（版本差异排查用）
	fmt.Println("  [dump] chain-root probe (candidate bases x roots) ...")
	probeBlocks := rwBlocks(h, 64.0)
	roots := []uintptr{0x02D1EA00, 0x02CA6D48, 0x02CC5DE8}
	for _, b := range probeBlocks {
		for _, delta := range []uintptr{0x02000000, 0x10000000, 0xF4000000, 0} {
			cand := b.Base - delta
			if cand == 0 || cand > b.Base {
				continue
			}
			line := fmt.Sprintf("    cand=0x%012X (blk 0x%012X -0x%X)", cand, b.Base, delta)
			any := false
			for _, r := range roots {
				if v, rok := readU64BE(h, cand+r); rok && (v >= 0x01000000 && v <= 0x4FFFFFFF) {
					line += fmt.Sprintf("  r0x%08X->0x%012X", r, v)
					any = true
				}
			}
			if !any {
				continue
			}
			for _, c := range cemuChains {
				if va, rok := resolveChain(h, cand, c); rok {
					n := len(slotScanAround(h, cand, va))
					line += fmt.Sprintf("  [%s ok, %d slots]", c.Name, n)
				}
			}
			fmt.Println(line)
		}
	}

	fmt.Println("  [dump] structural scan (top groups, NaN-filtered) ...")
	sb := rwBlocks(h, 64.0)
	hits := structuralScan(h, sb, nil)
	groups, _ := groupAndRank(h, hits)
	for i := 0; i < len(groups) && i < 10; i++ {
		g := groups[i]
		fmt.Printf("    copies=%d  mem=(%.1f, %.1f, %.1f)  ex0x%012X\n",
			g.Copies, g.X, g.Y, g.Z, g.Addrs[0])
	}

	// remembered offsets 当前是否可用（锚 = 最大块）
	off := loadOffsets()
	for _, o := range off {
		if v, rok := readPosBE(h, anchor+o); rok {
			fmt.Printf("  [dump] known offset 0x%012X -> mem=(%.1f, %.1f, %.1f)\n", o, v[0], v[1], v[2])
		} else {
			fmt.Printf("  [dump] known offset 0x%012X -> unreadable\n", o)
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
