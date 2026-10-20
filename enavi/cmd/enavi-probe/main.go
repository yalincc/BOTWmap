// enavi-probe —— Enavi M0 探针（一次性实测工具，不进交付物）。
//
// 目的：在写任何定位逻辑之前，把 Eden 的宿主内存形态、guest 映射、
// 身份锚点可行性这三件"只能实测"的事实钉死。用法见 enavi/PLAN.md §5。
//
//	go run ./cmd/enavi-probe -cmd all
//	go run ./cmd/enavi-probe -cmd strings -scope dram -pat Player,GameROMPlayer
//	go run ./cmd/enavi-probe -cmd coords -x 1759.8 -y 116.1 -z 1928.1 -r 60
//	go run ./cmd/enavi-probe -cmd struct -addr 0x1234abcd
//	go run ./cmd/enavi-probe -cmd gdb
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"botwmap/enavi/internal/gdbstub"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/version"
	"botwmap/enavi/internal/winmem"
)

var (
	cmdFlag  = flag.String("cmd", "m0", "m0（实机向导，默认）|pids|dram|regions|strings|coords|moving|struct|dump|gdb|all")
	pidFlag  = flag.Int("pid", 0, "Eden 的 PID；0 = 自动查找")
	procName = flag.String("proc", "eden", "模拟器进程名（不含 .exe）")
	minMB    = flag.Float64("minmb", 1, "regions 只列 ≥ 该尺寸（MB）的块")
	patFlag  = flag.String("pat", "Player,GameROMPlayer,PlayerInfo,Enemy_Giant,1.6.0", "strings 搜索的 ASCII 串，逗号分隔")
	scope    = flag.String("scope", "dram", "扫描范围：dram（Switch DRAM 大块）|heap（User Heap）|all（全宿主 RW）")
	xFlag    = flag.Float64("x", 0, "坐标锚点 X")
	yFlag    = flag.Float64("y", 0, "坐标锚点 Y（高度）")
	zFlag    = flag.Float64("z", 0, "坐标锚点 Z")
	radius   = flag.Float64("r", 60, "coords 窗口半径（米）")
	addrFlag = flag.String("addr", "", "struct/dump 的目标地址（0x 前缀，宿主地址）")
	nFlag    = flag.Int("n", 128, "dump 字节数")
	gdbAddr  = flag.String("gdb", "127.0.0.1:6543", "GDB Stub 地址")
	gvaFlag  = flag.Int64("gva", 0, "gdb 模式试读的 guest 地址（可选）")
	workers  = flag.Int("w", 8, "扫描并发数")
	topFlag  = flag.Int("top", 20, "coords 输出条数")
	chunkMB  = flag.Int("chunk", 8, "分块大小（MB）")
	scanDelay = flag.Duration("delay", 8*time.Millisecond, "每次块读之间的间隔（限速，防拖死模拟器）")
	scanAll   = flag.Bool("all", false, "scanactors: 扫全进程（慢，慎用）")
	timeout  = flag.Duration("t", 3*time.Second, "gdb 单包超时")
)

func main() {
	flag.Parse()
	// 向导模式自己打横幅（要写进日志），这里不重复
	if *cmdFlag != "m0" {
		sayln(version.Banner())
		sayln()
	}

	switch *cmdFlag {
	case "m0":
		cmdWizard()
	case "pids":
		cmdPids()
	case "dram":
		withEden(cmdDram)
	case "regions":
		withEden(cmdRegions)
	case "strings":
		withEden(cmdStrings)
	case "coords":
		withEden(cmdCoords)
	case "moving":
		withEden(cmdMoving)
	case "tfslots":
		withEden(cmdTfSlots)
	case "save":
		cmdSave()
	case "ringscan":
		withEden(cmdRingScan)
	case "playerinfo":
		withEden(cmdPlayerInfo)
	case "probevalues":
		withEden(cmdProbeValues)
	case "layout":
		withEden(cmdLayout)
	case "ptrscan":
		withEden(cmdPtrScan)
	case "actorscan":
		withEden(cmdActorScan)
	case "backscan":
		withEden(cmdBackScan)
	case "ownerscan":
		withEden(cmdOwnerScan)
	case "struct":
		withEden(cmdStruct)
	case "dump":
		withEden(cmdDump)
	case "gdbinfo":
		cmdGdbInfo()
	case "gdb":
		cmdGdb()
	case "trackcells":
		withEden(cmdTrackCells)
	case "climb":
		withEden(cmdClimb)
	case "structcoord":
		withEden(cmdStructCoord)
	case "livecoord":
		withEden(cmdLiveCoord)
	case "findpi":
		withEden(cmdFindPI)
	case "findhistory":
		withEden(cmdFindHistory)
	case "trackcoord":
		withEden(cmdTrackCoord)
	case "hunt3f":
		withEden(cmdHunt3f)
	case "findcoord":
		withEden(cmdFindCoord)
	case "scanactors":
		withEden(cmdScanActors)
	case "watchpos":
		withEden(cmdWatchPos)
	case "playerpos":
		withEden(cmdLocatePlayer)
	case "locatemain":
		withEden(cmdLocateMain)
	case "findbytes":
		withEden(cmdFindBytes)
	case "codedump":
		withEden(cmdCodeDump)
	case "jitdump":
		withEden(cmdJitDump)
	case "all":
		cmdAll()
	default:
		printf("未知 -cmd %q\n", *cmdFlag)
		os.Exit(2)
	}
}

func hr(t string) {
	printf("\n──────── %s ────────\n", t)
}

// ---- 基础命令 ----

func cmdPids() {
	hr("进程")
	pids := winmem.FindPIDs(*procName)
	if len(pids) == 0 {
		printf("[pid] 未找到 %q 进程（Eden 是否在运行？进程名可能是 eden.exe）\n", *procName)
		return
	}
	printf("[pid] 命中 %d 个 %q 进程\n", len(pids), *procName)
	for _, pid := range pids {
		p, err := winmem.Open(pid)
		if err != nil {
			printf("  pid=%d 打开失败: %v\n", pid, err)
			continue
		}
		printf("  pid=%d exe=%s\n", pid, p.ExePath())
		p.Close()
	}
}

func withEden(fn func(*winmem.Process)) {
	pid := uint32(*pidFlag)
	if pid == 0 {
		pids := winmem.FindPIDs(*procName)
		if len(pids) == 0 {
			printf("[fatal] 未找到 %q 进程。先启动 Eden 并进入游戏。\n", *procName)
			os.Exit(1)
		}
		pid = pids[0]
		if len(pids) > 1 {
			printf("[warn] 发现 %d 个 %s 进程，本次使用 pid=%d（多实例选择在 M2 做）\n", len(pids), *procName, pid)
		}
	}
	p, err := winmem.Open(pid)
	if err != nil {
		printf("[fatal] %v\n", err)
		os.Exit(1)
	}
	defer p.Close()
	loaded, title := winmem.GameLikelyLoaded(pid)
	printf("[attach] pid=%d exe=%s\n", pid, p.ExePath())
	printf("[attach] 窗口标题=%q 载入游戏=%v\n", title, loaded)
	if !loaded {
		sayln("[attach] ⚠️ Eden 已启动但看起来没载入游戏：User Heap 会是空的，dram/strings/coords 都测不出东西")
	}
	fn(p)
}

func cmdDram(p *winmem.Process) {
	hr("Switch DRAM 大块（ASLR 反解）")
	d, cands, err := p.FindDram()
	if err != nil {
		printf("[dram] %v\n", err)
		return
	}
	printf("[dram] ≥1GB MEM_MAPPED 候选 %d 个：\n", len(cands))
	for _, c := range cands {
		printf("   %s\n", c)
	}
	printf("[dram] 选中: block=0x%X size=%.2fGB\n", d.BlockBase, float64(d.BlockSize)/1073741824.0)
	printf("[dram] arenaLow=0x%X   ⇒ guestVA→宿主地址 = arenaLow + gva\n", d.ArenaLow)
	printf("[dram] User Heap 宿主区间: 0x%X ~ 0x%X\n", d.HeapLo(), d.HeapHi())

	// 验证 heap 区间里是否真有已提交区域（有 ⇒ 映射规则成立）
	regs := scan.HeapRegions(p, d, 64<<10)
	var total uint64
	for _, r := range regs {
		total += uint64(r.Size)
	}
	printf("[dram] User Heap 可读写块 %d 个，合计 %.2fGB\n", len(regs), float64(total)/1073741824.0)
	if len(regs) == 0 {
		sayln("[dram] ⚠️ 该区间没有可读写块。两种可能：")
		sayln("      ① 游戏未载入（最常见）→ 进游戏后重跑本命令；")
		sayln("      ② 内存配置变了（6G/8G）或映射规则与 xnavi 记录不同 → 需要重新标定")
		return
	}
	// arena 之后的第一批块，打印出来给人看形态
	sayln("[dram] User Heap 前 10 个块：")
	for i, r := range regs {
		if i >= 10 {
			break
		}
		printf("   %s\n", r)
	}
}

func cmdRegions(p *winmem.Process) {
	hr(fmt.Sprintf("内存区域概览（≥%.2fMB）", *minMB))
	minSize := uintptr(*minMB * 1048576)
	var total, commit, rwMapped, rwPrivate uint64
	var big []winmem.Region
	p.Regions(0, 0, func(r winmem.Region) bool {
		total++
		if r.State == winmem.MemCommit {
			commit += uint64(r.Size)
			switch r.Type {
			case winmem.MemMapped:
				rwMapped += uint64(r.Size)
			case winmem.MemPrivate:
				rwPrivate += uint64(r.Size)
			}
			if r.Size >= minSize {
				big = append(big, r)
			}
		}
		return false
	})
	printf("[regions] 区域总数 %d，已提交合计 %s（MAPPED %s / PRIVATE %s）\n",
		total, gb(commit), gb(rwMapped), gb(rwPrivate))
	sort.Slice(big, func(i, j int) bool { return big[i].Size > big[j].Size })
	limit := 20
	if len(big) < limit {
		limit = len(big)
	}
	printf("[regions] 最大的 %d 块：\n", limit)
	for i := 0; i < limit; i++ {
		printf("   %s\n", big[i])
	}
}

func cmdStrings(p *winmem.Process) {
	hr("身份锚点字符串扫描")
	pats := splitPatterns(*patFlag)
	d, _, err := p.FindDram()
	if err != nil {
		printf("[strings] %v\n", err)
		return
	}
	regions := scopeRegions(p, d, *scope)
	var bytes uint64
	for _, r := range regions {
		bytes += uint64(r.Size)
	}
	printf("[strings] scope=%s，块 %d 个 / %s，并发 %d\n", *scope, len(regions), gb(bytes), *workers)

	counts := make(map[string]int)
	romfsCounts := make(map[string]int)
	type addrRec struct {
		at    uintptr
		romfs bool
	}
	addrs := make(map[string][]addrRec)
	var mu sync.Mutex
	t0 := time.Now()
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		type hitRec struct {
			pat   string
			at    uintptr
			romfs bool
		}
		var local []hitRec
		for _, pat := range pats {
			for _, a := range scan.FindASCII(buf, addr, pat) {
				off := int(a - addr)
				local = append(local, hitRec{pat: pat, at: a, romfs: looksLikeRomfs(buf, off, len(pat))})
			}
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		for _, h := range local {
			counts[h.pat]++
			if h.romfs {
				romfsCounts[h.pat]++
			}
			if len(addrs[h.pat]) < 40 {
				addrs[h.pat] = append(addrs[h.pat], addrRec{at: h.at, romfs: h.romfs})
			}
		}
		mu.Unlock()
	})
	printf("[strings] 扫描耗时 %v\n", time.Since(t0).Round(time.Millisecond))

	for _, pat := range pats {
		bare := counts[pat] - romfsCounts[pat]
		printf("\n  %-16s 命中 %d 次（romfs 资源串 %d / 裸串 %d）\n", pat, counts[pat], romfsCounts[pat], bare)
		if bare == 0 && counts[pat] > 0 {
			sayln("    → 全是 romfs 里的路径/文本串，不能当身份锚点")
		}
		for i, rec := range addrs[pat] {
			if i >= 6 {
				printf("    …（省略 %d 条）\n", len(addrs[pat])-6)
				break
			}
			a := rec.at
			tag := "bare"
			if rec.romfs {
				tag = "romfs"
			}
			printf("    [%s] %s\n", tag, fmtAddr(d, a))
			// 打印命中前后的可打印上下文，判断是不是 actor 名/资源名
			if b, err := p.Read(a-16, 64); err == nil {
				printf("      ctx: %q\n", printable(b))
			}
		}
	}
	sayln("\n[strings] 判读：只有「裸串 > 0 且成片出现」才可能当身份锚点；")
	sayln("          全是 romfs 资源串（.sbactorpack/.msbt/.pack 附近）= ROM 数据，与运行期 actor 无关。")
}

func cmdCoords(p *winmem.Process) {
	hr("坐标三元组窗口扫描")
	if *xFlag == 0 && *yFlag == 0 && *zFlag == 0 {
		sayln("[coords] 需要锚点：-x -y -z（游戏内当前位置；可用存档或网页地图坐标）")
		return
	}
	anchor := scan.Vec3{X: float32(*xFlag), Y: float32(*yFlag), Z: float32(*zFlag)}
	if !anchor.Valid() {
		printf("[coords] 锚点不合法：%+v（范围 |X|,|Z|≤7000、Y∈[-600,5000]）\n", anchor)
		return
	}
	d, _, err := p.FindDram()
	if err != nil {
		printf("[coords] %v\n", err)
		return
	}
	regions := scopeRegions(p, d, *scope)
	var bytes uint64
	for _, r := range regions {
		bytes += uint64(r.Size)
	}
	printf("[coords] 锚点=(%.1f, %.1f, %.1f) 半径=%.0fm scope=%s 块=%d %s\n",
		anchor.X, anchor.Y, anchor.Z, *radius, *scope, len(regions), gb(bytes))

	opt := scan.Options{Anchor: &anchor, Radius: float32(*radius)}
	var all []scan.Hit
	var mu sync.Mutex
	t0 := time.Now()
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		hits := scan.FindTriples(buf, addr, opt)
		if len(hits) == 0 {
			return
		}
		mu.Lock()
		if len(all) < 400000 {
			all = append(all, hits...)
		}
		mu.Unlock()
	})
	elapsed := time.Since(t0)
	groups := scan.Group(all)
	printf("[coords] 命中 %d 点 / 分组后 %d 组，耗时 %v\n", len(all), len(groups), elapsed.Round(time.Millisecond))

	limit := *topFlag
	if len(groups) < limit {
		limit = len(groups)
	}
	printf("[coords] 前 %d 组（rot=Actor 旋转矩阵特征）:\n", limit)
	printf("  %-4s %-14s %-26s %-6s %-4s %s\n", "#", "host", "pos(x, y, z)", "dist", "rot", "copies")
	for i := 0; i < limit; i++ {
		g := groups[i]
		printf("  %-4d %-14s (%8.1f,%7.1f,%8.1f) %-6.1f %-4v %d\n",
			i+1, fmt.Sprintf("0x%X", g.Addr), g.P.X, g.P.Y, g.P.Z, g.Dist, g.Rot, g.Copies)
	}
	// 对最优候选做四元数/朝向解算演示（M0-R3 要看真实布局）
	if len(groups) > 0 {
		sayln("\n[coords] 最优候选邻域（找单位四元数与朝向）：")
		analyzeNeighborhood(p, d, groups[0].Addr, 96)
	}
	sayln("\n[coords] 判读：真玩家组应 rot=true 且步行时位移连续；多组同距时由 M1 的移动一致性裁决。")
}

// cmdMoving 不需要锚点的身份扫描：连采两次，找出「地址不变但位置变了」的槽。
// 这是 M0 阶段在没有存档锚点、也不想先做存档解析时，指认玩家槽的最快手段。
// 用法：进游戏 → 让林克持续走动/跑动 → 跑本命令（期间别传送、别骑马）。
func cmdStruct(p *winmem.Process) {
	hr("结构体解析")
	addr, err := parseAddr(*addrFlag)
	if err != nil {
		printf("[struct] %v\n", err)
		return
	}
	d, _, _ := p.FindDram()
	buf, err := p.Read(addr, 128)
	if err != nil {
		printf("[struct] %v\n", err)
		return
	}
	printf("[struct] %s（%d 字节）\n", fmtAddr(d, addr), len(buf))
	printf("  as float32: %v\n", floats(buf, 0, 12))
	printf("  rot3x3 ok = %v\n", scan.Rot3OK(buf, 12))
	analyzeNeighborhood(p, d, addr, 96)
}

func cmdDump(p *winmem.Process) {
	hr("内存 dump")
	addr, err := parseAddr(*addrFlag)
	if err != nil {
		printf("[dump] %v\n", err)
		return
	}
	d, _, _ := p.FindDram()
	buf, err := p.Read(addr, *nFlag)
	if err != nil {
		printf("[dump] %v\n", err)
		return
	}
	printf("[dump] %s\n", fmtAddr(d, addr))
	for i := 0; i < len(buf); i += 16 {
		end := i + 16
		if end > len(buf) {
			end = len(buf)
		}
		printf("  %08X  %-47s  %s\n", i, hexBytes(buf[i:end]), printable(buf[i:end]))
	}
}

func cmdGdb() {
	hr("GDB Stub 通道验证（仅用于证明「不适合当实时通道」）")
	for _, line := range gdbstub.Probe(*gdbAddr, uintptr(*gvaFlag), *timeout) {
		sayln(line)
	}
	sayln("[gdb] 判读：若 m 包往返 >5ms、或读取期间游戏明显卡顿 ⇒ 确认只作 M0 校准工具，不进生产路径。")
}

func cmdAll() {
	sayln("M0 一键实测：pids → dram → regions → strings → gdb")
	cmdPids()

	pids := winmem.FindPIDs(*procName)
	if len(pids) == 0 {
		return
	}
	loaded, title := winmem.GameLikelyLoaded(pids[0])
	printf("\n[check] pid=%d 标题=%q 载入游戏=%v\n", pids[0], title, loaded)
	if !loaded {
		sayln("[check] ⛔ 游戏未载入，后续项测不出东西。请进游戏（已读档、人物能走动）后再跑一次：")
		sayln("         enavi-probe -cmd all")
		sayln("[check] 下面先输出空载快照，便于对比载入前后的内存形态差异：")
		withEden(cmdDram)
		return
	}

	withEden(cmdDram)
	withEden(cmdRegions)
	withEden(cmdStrings)
	cmdGdb()
	sayln("\n把以上完整输出贴回给我，即可填 enavi/PLAN.md §5.3 的 M0 台账。")
}

// ---- 工具 ----

// quatHit 邻域内找到的一个单位四元数。
type quatHit struct {
	Rel int
	Q   [4]float32
	Yaw float64
}

// analyzeNeighborhood 在 addr 附近扫单位四元数，打印偏移 + 解算朝向，并返回结果。
func analyzeNeighborhood(p *winmem.Process, d winmem.Dram, addr uintptr, span int) []quatHit {
	var hits []quatHit
	start := addr - 32
	buf, err := p.Read(start, span+32)
	if err != nil {
		printf("    读取邻域失败: %v\n", err)
		return nil
	}
	for off := 0; off+16 <= len(buf); off += 4 {
		q, ok := scan.Quat(buf, off)
		if !ok {
			continue
		}
		rel := int(start) - int(addr) + off
		yaw := scan.YawFromQuat(q[0], q[1], q[2], q[3])
		printf("    quat@%+4d  (x=%+.3f y=%+.3f z=%+.3f w=%+.3f) → yaw=%.1f°\n",
			rel, q[0], q[1], q[2], q[3], yaw)
		hits = append(hits, quatHit{Rel: rel, Q: q, Yaw: yaw})
		if len(hits) >= 6 {
			break
		}
	}
	if len(hits) == 0 {
		sayln("    邻域内未找到单位四元数（朝向可能不在坐标槽附近，需另找槽）")
	}
	return hits
}

func scopeRegions(p *winmem.Process, d winmem.Dram, scope string) []winmem.Region {
	switch scope {
	case "heap":
		return scan.HeapRegions(p, d, 64<<10)
	case "all":
		return scan.BigRWRegions(p, 64<<10)
	default: // dram：只扫 Switch DRAM 大块本身（模块 rodata / 物理视图）
		var out []winmem.Region
		p.Regions(d.BlockBase, d.BlockBase+d.BlockSize, func(r winmem.Region) bool {
			if r.Readable() {
				out = append(out, r)
			}
			return false
		})
		return out
	}
}

// runChunks 把区域切成定长块，用 worker 池并发读取并回调。
func runChunks(p *winmem.Process, regions []winmem.Region, workers int, fn func(addr uintptr, buf []byte)) {
	if workers < 1 {
		workers = 1
	}
	const overlap = 256
	chunk := *chunkMB << 20
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += uintptr(chunk) {
			n := chunk
			if remain := int(r.Size - off); remain < n {
				n = remain
			}
			if uintptr(n) < r.Size-off {
				n += overlap
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}
	if len(jobs) == 0 {
		return
	}
	ch := make(chan job, workers*2)
	var wg sync.WaitGroup
	// ★ 限速：ReadProcessMemory 全速读会跟模拟器抢内存带宽，
	// 实测会把 Eden 拖到"卡在加载画面"。所以每次读之间强制小睡，
	// 并把并发压到很低 —— 探针是离线工具，慢一点没关系，绝不能影响游戏。
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				// 限速：每块之间 sleep
				time.Sleep(*scanDelay)
				if err != nil || len(buf) < 12 {
					continue
				}
				fn(j.addr, buf)
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

// romfsMarkers romfs/资源包字符串的邻域特征。
// 之所以要区分：2026-10-20 实测，DRAM 大块里 Player 有 7174 次命中，
// 全部来自 TipsPlayer.msbt / GameROMPlayer.sbactorpack 这类 ROM 资源路径，
// 与运行期 actor 无关 —— 直接当身份锚点会全错。
var romfsMarkers = []string{
	".sbactorpack", ".bactorpack", ".msbt", ".sarc", ".pack", ".bfres", ".bntx",
	".bgparamlist", "Actor/Pack", "LayoutMsg", "EventFlow",
}

func looksLikeRomfs(buf []byte, off, n int) bool {
	// ① 两侧不是 NUL → 命中嵌在路径/文本里（如 Tips/TipsPlayer.msbt），不是独立 rodata 串
	if off > 0 && off+n < len(buf) {
		if buf[off-1] != 0 || buf[off+n] != 0 {
			return true
		}
	}
	// ② 邻域有资源扩展名
	lo, hi := off-160, off+160+n
	if lo < 0 {
		lo = 0
	}
	if hi > len(buf) {
		hi = len(buf)
	}
	win := buf[lo:hi]
	for _, m := range romfsMarkers {
		if len(scan.FindASCII(win, 0, m)) > 0 {
			return true
		}
	}
	return false
}

func fmtAddr(d winmem.Dram, host uintptr) string {
	s := fmt.Sprintf("host=0x%X", host)
	if host >= d.BlockBase && host < d.BlockBase+d.BlockSize {
		s += fmt.Sprintf(" phys=+0x%X", host-d.BlockBase)
	}
	if host >= d.ArenaLow && host-d.ArenaLow >= 0x2000000000 {
		s += fmt.Sprintf(" gva=0x%X", host-d.ArenaLow)
	}
	return s
}

func splitPatterns(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseAddr(s string) (uintptr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("需要 -addr 0x...")
	}
	var v uint64
	if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"), "%x", &v); err != nil {
		return 0, fmt.Errorf("地址解析失败 %q: %w", s, err)
	}
	if v == 0 {
		return 0, fmt.Errorf("地址不能为 0")
	}
	return uintptr(v), nil
}

func floats(buf []byte, off, n int) []float32 {
	var out []float32
	for i := 0; i < n; i++ {
		p := off + i*4
		if p+4 > len(buf) {
			break
		}
		out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(buf[p:])))
	}
	return out
}

func hexBytes(b []byte) string {
	const hexd = "0123456789ABCDEF"
	var sb strings.Builder
	for i, c := range b {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte(hexd[c>>4])
		sb.WriteByte(hexd[c&0xF])
	}
	return sb.String()
}

func printable(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('.')
		}
	}
	return sb.String()
}

func gb(n uint64) string { return fmt.Sprintf("%.2fGB", float64(n)/1073741824.0) }

// gameLoaded 判断「Eden 是否真的载入了游戏」。
// 判据 = User Heap 有没有可读写块（可靠）；窗口标题只作为附加信息。
// 2026-10-20 实测：游戏在跑时主窗口标题可能是 "Eden 设置"（设置窗口在前台），
// 按标题判会误报"未载入"。
func gameLoaded(pid uint32) (bool, string) {
	title := winmem.MainWindowTitle(pid)
	p, err := winmem.Open(pid)
	if err != nil {
		return false, title
	}
	defer p.Close()
	d, _, err := p.FindDram()
	if err != nil {
		return false, title
	}
	return len(scan.HeapRegions(p, d, 64<<10)) > 0, title
}
