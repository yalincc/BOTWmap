// live-npcs：实体探测器（NPC / 怪物 / 动物跟踪）——独立模块。
//
// 用途：读 Ryujinx 内存，扫描"坐标+旋转矩阵"结构簇（实体槽），排除玩家槽，
// 输出实体列表（含坐标/moving），供浮窗/地图叠加小红点。
//
// 用法：live-npcs.exe [--probe[=N]] [port]
//   - 默认：HTTP API :8778，每 1s 刷新实体列表（M2）
//   - --probe: 只跑 M1 探测（默认 10 帧），输出探测报告后退出
//
// 模块化：独立于 live-go/（BotwNavi）与 app/（网页地图），可随时删除。

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort  = 8778
	refreshEvery = 3000 * time.Millisecond // M3'：NPC 多静止，3s 一轮够用且不卡
)

// Npc 一个实体（NPC/怪物/动物）。
type Npc struct {
	Name  string  `json:"name,omitempty"`
	Label string  `json:"label,omitempty"`  // 游戏内显示名（官方中文，如 Npc_HatenoVillage022 → 万作）
	LabelEn string `json:"label_en,omitempty"` // 英文显示名（社区表，对照用，如 → Manny）
	X, Y, Z float32 // 游戏坐标 (X east, alt, Z south+)
	MX, MY  int     // 地图像素 mx=(gx*2)+12000, my=(gz*2)+10000
	Moving  bool    `json:"moving"`
	Addr    uintptr `json:"-"`
}

type npcState struct {
	mu     sync.RWMutex
	list   []Npc
	player Npc // 玩家（用于排除，也在 API 里返回）
	ok     bool
	addr   uintptr
	updated time.Time
}

var state npcState

// trackNpcs 每 1s 扫描一次：窗口扫描（玩家 ±win）实体候选 → 排除玩家 → 更新状态。
func trackNpcs(h uintptr) {
	// 上一帧：坐标列表，用于 moving 判定（按坐标最近邻匹配，不用地址——
	// 实体组 RepAddr 每次扫描可能不同，地址匹配永远失败）。
	prev := [][3]float32{}
	var playerPos [3]float32
	var havePlayer bool
	for {
		t0 := time.Now()
		refs := [][3]float32{}
		if havePlayer {
			refs = append(refs, playerPos)
		} else if a := readSaveAnchors(); len(a) > 0 {
			refs = append(refs, a[0].Pos)
		}
		if len(refs) == 0 {
			fmt.Println("[live-npcs] no anchor; waiting for save file...")
			time.Sleep(3 * time.Second)
			continue
		}
		groups, hits, secs := findEntitiesNear(h, refs, 400)
		_ = secs
		anchors := readSaveAnchors()
		player := findPlayerGroup(groups, anchors)

		var list []Npc
		var cur [][3]float32
		for _, g := range groups {
			if player != nil && g == player {
				continue
			}
			addr := g.RepAddr
			if addr == 0 {
				addr = g.Addrs[0]
			}
			xyz := [3]float32{g.X, g.Y, g.Z}
			// moving：与上一帧最近邻（<3m 视为同一实体）比较，位移 >0.5m 算移动
			moving := false
			if _, d := nearestPrev(prev, xyz); d < 3 && d > 0.5 {
				moving = true
			}
			cur = append(cur, xyz)
			list = append(list, Npc{
				X: g.X, Y: g.Y, Z: g.Z,
				MX: int(g.X*2) + 12000, MY: int(g.Z*2) + 10000,
				Moving: moving, Addr: addr,
			})
		}
		prev = cur // 本轮坐标作为下一帧基准（实体卸载自然消失）

		state.mu.Lock()
		state.list = list
		state.ok = true
		state.updated = time.Now()
		if player != nil {
			state.player = Npc{
				X: player.X, Y: player.Y, Z: player.Z,
				MX: int(player.X*2) + 12000, MY: int(player.Z*2) + 10000,
				Addr: player.RepAddr,
			}
			state.addr = player.RepAddr
			playerPos = [3]float32{player.X, player.Y, player.Z}
			havePlayer = true
		}
		state.mu.Unlock()

		elapsed := time.Since(t0)
		fmt.Printf("[live-npcs] %d entities (%d player, %d hits) in %s\n", len(list), boolInt(player != nil), hits, elapsed.Round(time.Millisecond))
		time.Sleep(refreshEvery)
	}
}

// nearestPrev 找上一帧里距 xyz 最近的坐标，返回距离；无上一帧返回 1e18。
func nearestPrev(prev [][3]float32, xyz [3]float32) ([3]float32, float32) {
	best, bestD := [3]float32{}, float32(1e18)
	for _, p := range prev {
		dx, dy, dz := xyz[0]-p[0], xyz[1]-p[1], xyz[2]-p[2]
		d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
		if d < bestD {
			best, bestD = p, d
		}
	}
	return best, bestD
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// npcsHandler GET /npcs → 实体列表 + 玩家。
func npcsHandler(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if !state.ok {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "list": []Npc{}})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"updated": state.updated.Format("15:04:05"),
		"player":  state.player,
		"list":    state.list,
	})
}

func main() {
	setConsoleTitle("live-npcs · BOTW Entity Tracker")

	port := defaultPort
	probeFrames := 0
	traceMode := false
	scanStrMode := false
	linkNamesMode := false
	memMapMode := false
	ptrMapMode := false
	refScanMode := false
	probe71Mode := false
	dumpObjMode := false
	dumpObjArg := ""
	vtScanMode := false
	dumpVtMode := false
	dumpVtArg := ""
	objScanMode := false
	objScanArg := ""
	refSlotMode := false
	nameRegionsMode := false
	probe010Mode := false
	npcScopeMode := false
	probeAFMode := false
	probeAF2Mode := false
	probeAF3Mode := false
	probeAF4Mode := false
	probeAF5Mode := false
	probeAF6Mode := false
	probeAF7Mode := false
	probeAF8Mode := false
	probeAF9Mode := false
	probeAFAMode := false
	probeAFBMode := false
	probeAFCMode := false
	probeAFDMode := false
	probeAFEMode := false
	probeAFEArg := ""
	for _, a := range os.Args[1:] {
		switch {
		case a == "--probe71":
			probe71Mode = true
		case strings.HasPrefix(a, "--dumpobj="):
			dumpObjArg = a[len("--dumpobj="):]
			dumpObjMode = true
		case a == "--vtscan":
			vtScanMode = true
		case strings.HasPrefix(a, "--dumpvtable="):
			dumpVtArg = a[len("--dumpvtable="):]
			dumpVtMode = true
		case strings.HasPrefix(a, "--objscan="):
			objScanArg = a[len("--objscan="):]
			objScanMode = true
		case a == "--refslot":
			refSlotMode = true
		case a == "--nameregions":
			nameRegionsMode = true
		case a == "--probe010":
			probe010Mode = true
		case a == "--npcscope":
			npcScopeMode = true
		case a == "--probeaf":
			probeAFMode = true
		case a == "--probeaf2":
			probeAF2Mode = true
		case a == "--probeaf3":
			probeAF3Mode = true
		case a == "--probeaf4":
			probeAF4Mode = true
		case a == "--probeaf5":
			probeAF5Mode = true
		case a == "--probeaf6":
			probeAF6Mode = true
		case a == "--probeaf7":
			probeAF7Mode = true
		case a == "--probeaf8":
			probeAF8Mode = true
		case a == "--probeaf9":
			probeAF9Mode = true
		case a == "--probeafa":
			probeAFAMode = true
		case a == "--probeafb":
			probeAFBMode = true
		case a == "--probeafc":
			probeAFCMode = true
		case a == "--probeafd":
			probeAFDMode = true
		case strings.HasPrefix(a, "--probeafe="):
			probeAFEArg = a[len("--probeafe="):]
			probeAFEMode = true
		case strings.HasPrefix(a, "--probe"):
			probeFrames = 10
			if eq := strings.Index(a, "="); eq >= 0 {
				if n, err := strconv.Atoi(a[eq+1:]); err == nil && n > 0 {
					probeFrames = n
				}
			}
		case a == "--trace":
			traceMode = true
		case a == "--scanstr":
			scanStrMode = true
		case a == "--linknames":
			linkNamesMode = true
		case a == "--memmap":
			memMapMode = true
		case a == "--ptrmap":
			ptrMapMode = true
		case a == "--refscan":
			refScanMode = true
		case a == "--probe71":
			probe71Mode = true
		default:
			if n, err := strconv.Atoi(a); err == nil {
				port = n
			}
		}
	}

	fmt.Println("======================================================")
	fmt.Println(" live-npcs · BOTW entity tracker (NPC/monster/animal)")
	fmt.Println("======================================================")

	loadNpcNames()

	pid := findPid("ryujinx")
	if pid == 0 {
		fmt.Println("Ryujinx not running. Start the emulator and load a save first.")
		if probeFrames > 0 {
			os.Exit(1)
		}
		// 服务模式：等待 watchdog 附上（简化：直接退出，M2 后加 watchdog）
		fmt.Println("Exiting. (Watchdog for late-starting Ryujinx comes in a later milestone.)")
		os.Exit(1)
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		fmt.Println("OpenProcess failed. Try running as administrator.")
		os.Exit(1)
	}
	defer closeHandle(h)
	fmt.Printf("attached to ryujinx pid=%d\n", pid)

	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("No save anchor found (game_data.sav missing/unreadable).")
		os.Exit(1)
	}
	fmt.Printf("save anchor: mem=(%.1f, %.1f, %.1f)\n", anchors[0].Pos[0], anchors[0].Pos[1], anchors[0].Pos[2])

	if probeFrames > 0 {
		fmt.Printf("\n[M1 probe] scanning %d frames (1s apart)...\n", probeFrames)
		rep := runProbe(h, probeFrames)
		p := saveReport(rep)
		printReport(rep)
		fmt.Printf("\nreport saved: %s\n", p)
		return
	}

	if traceMode {
		fmt.Println("\n[trace] actor object layout exploration (M3' pointer-chain reverse)")
		runTrace(h)
		return
	}

	if scanStrMode {
		fmt.Println("\n[scanstr] scanning actor name strings (M3' reverse)")
		runScanStr(h)
		return
	}

	if linkNamesMode {
		fmt.Println("\n[linknames] linking entity slots to name strings (M3' reverse)")
		runLinkNames(h)
		return
	}

	if memMapMode {
		fmt.Println("\n[memmap] guest memory regions (M3' reverse)")
		runMemMap(h)
		return
	}

	if ptrMapMode {
		fmt.Println("\n[ptrmap] pointer targets near player slot (M3' reverse)")
		runPtrMap(h)
		return
	}

	if refScanMode {
		fmt.Println("\n[refscan] reverse-lookup: who points to name strings (M3' reverse)")
		runRefScan(h)
		return
	}

	if probe71Mode {
		fmt.Println("\n[probe71] probe 0x710... base region (M3' reverse)")
		runProbe71(h)
		return
	}

	if dumpObjMode {
		fmt.Println("\n[dumpobj] dump structure around name string (M3' reverse)")
		runDumpObj(h, dumpObjArg)
		return
	}

	if vtScanMode {
		fmt.Println("\n[vtscan] full-memory vtable pointer scan (M3' reverse)")
		runVtScan(h)
		return
	}

	if dumpVtMode {
		fmt.Println("\n[dumpvtable] dump vtable object structure (M3' reverse)")
		runDumpVtable(h, dumpVtArg)
		return
	}

	if objScanMode {
		fmt.Println("\n[objscan] scan object cluster around anchor (M3' reverse)")
		runObjScan(h, objScanArg)
		return
	}

	if refSlotMode {
		fmt.Println("\n[refslot] reverse-lookup: who references player slot (M3' reverse)")
		runRefSlot(h)
		return
	}

	if nameRegionsMode {
		fmt.Println("\n[nameregions] name string region distribution (M3' reverse)")
		runNameRegions(h)
		return
	}

	if probe010Mode {
		fmt.Println("\n[probe010] probe 0x0100... region (M3' reverse)")
		runProbe010(h)
		return
	}

	if npcScopeMode {
		fmt.Println("\n[npcscope] scan NPC name strings around player (M3' reverse)")
		runNpcScope(h)
		return
	}

	if probeAFMode {
		fmt.Println("\n[probeaf] probe 0x0AF7 region (M3' reverse)")
		runProbeAF(h)
		return
	}

	if probeAF2Mode {
		fmt.Println("\n[probeaf2] probe guest ptrs w/ mapping base (M3' reverse)")
		runProbeAF2(h)
		return
	}

	if probeAF3Mode {
		fmt.Println("\n[probeaf3] follow name-obj ptr chain (M3' reverse)")
		runProbeAF3(h)
		return
	}

	if probeAF4Mode {
		fmt.Println("\n[probeaf4] dump name-obj array entries (M3' reverse)")
		runProbeAF4(h)
		return
	}

	if probeAF5Mode {
		fmt.Println("\n[probeaf5] dump 0x0B11 region (M3' reverse)")
		runProbeAF5(h)
		return
	}

	if probeAF6Mode {
		fmt.Println("\n[probeaf6] find coords near Npc name objs (M3' reverse)")
		runProbeAF6(h)
		return
	}

	if probeAF7Mode {
		fmt.Println("\n[probeaf7] dump ActorInfo sub-ptrs & refs (M3' reverse)")
		runProbeAF7(h)
		return
	}

	if probeAF8Mode {
		fmt.Println("\n[probeaf8] reverse-lookup ActorInfo hash refs (M3' reverse)")
		runProbeAF8(h)
		return
	}

	if probeAF9Mode {
		fmt.Println("\n[probeaf9] dump hash ref sites (M3' reverse)")
		runProbeAF9(h)
		return
	}

	if probeAFAMode {
		fmt.Println("\n[probeafa] hash->instance reverse lookup (M3' reverse)")
		runProbeAFA(h)
		return
	}

	if probeAFBMode {
		fmt.Println("\n[probeafb] refine coord extraction near hash refs (M3' reverse)")
		runProbeAFB(h)
		return
	}

	if probeAFCMode {
		fmt.Println("\n[probeafc] formal instance locator (M3' reverse)")
		runProbeAFC(h)
		return
	}

	if probeAFDMode {
		fmt.Println("\n[probeafd] compare instance vs component ref structs (M3' reverse)")
		runProbeAFD(h)
		return
	}

	if probeAFEMode {
		fmt.Println("\n[probeafe] dump all hash refs per actor name (jitter diagnosis)")
		runProbeAFE(h, probeAFEArg)
		return
	}

	// M2 服务模式（M3'：NPC hash→实例定位，替代启发式扫描）
	go trackNpcsV2(h)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("listen %s failed: %v\n", addr, err)
		os.Exit(1)
	}
	fmt.Printf("API -> http://%s/npcs  (Ctrl+C to stop)\n", addr)
	mux := http.NewServeMux()
	mux.HandleFunc("/npcs", npcsHandler)
	http.Serve(ln, mux)
}
