// 本文件 = M0 实机体检向导（老大双击 M0.bat 就走这一套）。
//
// 设计原则：
//  1. 老大不需要敲任何命令 —— 每步只提示「在游戏里做什么」，回来按 Enter；
//  2. 身份判定走确定性路径：存档锚点（精确世界坐标）→ ±120m 窄窗扫描 → 走动差分，
//     不再靠"整堆猜谁是玩家"（2026-10-20 实测整堆猜法捞出 6 万候选，不可用）；
//  3. 全部输出同时写文件，结束自动打开文件夹。
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"botwmap/enavi/internal/gdbstub"
	knownpkg "botwmap/enavi/internal/known"
	"botwmap/enavi/internal/saveanchor"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/version"
	"botwmap/enavi/internal/winmem"
)

// out 是统一输出口：向导模式 = 屏幕 + 日志文件双写。
var out io.Writer = os.Stdout

func printf(format string, a ...any) { fmt.Fprintf(out, format, a...) }
func sayln(a ...any)                 { fmt.Fprintln(out, a...) }

var stdinReader = bufio.NewReader(os.Stdin)

// globalProc 打印函数复用向导已打开的句柄。
var globalProc *winmem.Process

// tfSlot 一个坐标槽（含邻近旋转矩阵信息）。
type tfSlot struct {
	Addr   uintptr
	P      scan.Vec3
	RotOff int // 旋转矩阵相对坐标的偏移（实测值，不写死）
	M      [9]float32
	Next   [3]float32 // 矩阵之后 12 字节，用于观察 scale 真实位置
}

// winHit 窗口扫描命中的坐标槽，附带走动差分结果。
type winHit struct {
	Addr  uintptr
	P     scan.Vec3
	Rot   bool
	Hist  bool    // 是否落在「位置历史副本链」上（32B 步长、彼此几十米内的同构坐标）
	Dist  float64 // 距锚点（米）
	Delta float64 // 两次采样间位移（米）
}

// ---- 内存访问小工具 ----

func heapReady(p *winmem.Process) (winmem.Dram, []winmem.Region, error) {
	d, _, err := p.FindDram()
	if err != nil {
		return d, nil, err
	}
	regions := scan.HeapRegions(p, d, 64<<10)
	if len(regions) == 0 {
		return d, nil, fmt.Errorf("User Heap 里没有可读写块 → 游戏还没载入（进游戏、读到存档后再来）")
	}
	return d, regions, nil
}

// scanWindow 在锚点半径内扫坐标三元组，返回 addr→Hit。
func scanWindow(p *winmem.Process, regions []winmem.Region, anchor scan.Vec3, radius float32) (map[uintptr]scan.Hit, int) {
	hits := make(map[uintptr]scan.Hit, 256)
	total := 0
	opt := scan.Options{Anchor: &anchor, Radius: radius}
	var mu sync.Mutex
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		hs := scan.FindTriples(buf, addr, opt)
		if len(hs) == 0 {
			return
		}
		mu.Lock()
		total += len(hs)
		for _, h := range hs {
			if len(hits) < 200000 {
				hits[h.Addr] = h
			}
		}
		mu.Unlock()
	})
	return hits, total
}

// findMovingInWindow 以锚点为中心做两次窗口扫描，找「地址不变、位置变了」的槽。
func findMovingInWindow(p *winmem.Process, regions []winmem.Region, anchor scan.Vec3, r1, r2 float32, gap time.Duration) ([]winHit, int, int) {
	a, _ := scanWindow(p, regions, anchor, r1)
	time.Sleep(gap)
	b, _ := scanWindow(p, regions, anchor, r2)

	var mv []winHit
	for addr, h2 := range b {
		h1, ok := a[addr]
		if !ok {
			continue
		}
		dd := h2.P.Dist3D(h1.P)
		if dd >= 0.5 && dd <= 20 {
			mv = append(mv, winHit{Addr: addr, P: h2.P, Rot: h2.Rot, Dist: h2.Dist, Delta: dd})
		}
	}
	// 标记"位置历史副本"：2026-10-20 实测，同一物体的一串历史坐标以 32B 步长堆在一起，
	// 它们也会"移动"，会把真身挤到列表后面。真身通常是孤立的那个。
	for i := range mv {
		mv[i].Hist = isHistoryCopy(p, mv[i].Addr, mv[i].P)
	}
	sort.Slice(mv, func(i, j int) bool {
		if mv[i].Rot != mv[j].Rot {
			return mv[i].Rot // 1) 有旋转矩阵的优先
		}
		if mv[i].Hist != mv[j].Hist {
			return !mv[i].Hist // 2) 非历史副本优先
		}
		return mv[i].Delta > mv[j].Delta // 3) 位移大的优先
	})
	return mv, len(a), len(b)
}

// readTf 在指定地址重读一次 Transform（朝向验证用）。
func readTf(p *winmem.Process, addr uintptr) (tfSlot, bool) {
	// 坐标前 64 字节也读进来：朝向矩阵可能排在坐标之前
	const pre = 64
	buf, err := p.Read(addr-pre, pre+128)
	if err != nil {
		return tfSlot{}, false
	}
	pos := scan.Vec3{
		X: math.Float32frombits(binary.LittleEndian.Uint32(buf[pre:])),
		Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[pre+4:])),
		Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[pre+8:])),
	}
	if !pos.Valid() {
		return tfSlot{}, false
	}
	k := scan.RotOffsetIn(buf, pre, -32, 96)
	if k < 0 {
		return tfSlot{Addr: addr, P: pos, RotOff: -1}, false
	}
	start := pre + k
	s := tfSlot{Addr: addr, P: pos, RotOff: k, M: scan.Matrix(buf, start)}
	if start+48 <= len(buf) {
		for i := 0; i < 3; i++ {
			s.Next[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[start+36+i*4:]))
		}
	}
	return s, true
}

// scanTransformSlots 全堆 Transform 统计（诊断用，不参与向导判定）。
func scanTransformSlots(p *winmem.Process, regions []winmem.Region) (map[uintptr]tfSlot, int) {
	slots := make(map[uintptr]tfSlot, 1<<14)
	loose := 0
	var mu sync.Mutex
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		var local []tfSlot
		nLoose := 0
		for off := 0; off+60 <= len(buf); off += 4 {
			pos, rotOff, ok := scan.TransformLoose(buf, off)
			if !ok {
				if pos.Valid() {
					nLoose++
				}
				continue
			}
			s := tfSlot{Addr: addr + uintptr(off), P: pos, RotOff: rotOff, M: scan.Matrix(buf, off+rotOff)}
			if off+rotOff+48 <= len(buf) {
				for i := 0; i < 3; i++ {
					s.Next[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[off+rotOff+36+i*4:]))
				}
			}
			local = append(local, s)
		}
		if len(local) == 0 && nLoose == 0 {
			return
		}
		mu.Lock()
		loose += nLoose
		for _, s := range local {
			slots[s.Addr] = s
		}
		mu.Unlock()
	})
	return slots, loose
}

// ---- 交互 ----

func waitEnter(prompt string) bool {
	sayln(prompt)
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(line)) != "q"
}

func cmdWizard() {
	if err := runWizard(); err != nil {
		printf("\n[中止] %v\n", err)
		sayln("（双击运行的窗口一闪而过时，请改用 enavi\\M0.bat 启动）")
	}
}

// ---- 向导主体 ----

func runWizard() error {
	if err := os.MkdirAll("report", 0o755); err != nil {
		return fmt.Errorf("创建 report 目录失败: %w", err)
	}
	absDir, _ := filepath.Abs("report")
	logPath := filepath.Join(absDir, "M0-"+time.Now().Format("20060102-150405")+".log")
	f, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("创建日志失败: %w", err)
	}
	defer f.Close()
	out = io.MultiWriter(os.Stdout, f)
	defer func() { out = os.Stdout }()

	sayln("================================================================")
	printf(" %s · M0 实机体检\n", version.Banner())
	sayln("================================================================")
	sayln(" 你只需要：在游戏里按提示做动作 → 回这个窗口按 Enter")
	sayln(" 输出会同时写进日志文件，跑完自动打开文件夹")
	sayln(" 想中止：输入 q 再回车")
	sayln("")

	// ── 1. 等进游戏 ──
	sayln("[1/7] 等待进入游戏")
	p, d, regions, err := waitForGame()
	if err != nil {
		return err
	}
	defer p.Close()
	globalProc = p

	// ── 2. 存档锚点 ──
	sayln("\n[2/7] 读存档锚点（自动）")
	anchor, err := pickAnchor(p)
	if err != nil {
		return err
	}
	printf("    锚点：pos=(%.1f, %.1f, %.1f)  playtime=%ds  korok=%d\n",
		anchor.Pos[0], anchor.Pos[1], anchor.Pos[2], anchor.Playtime, anchor.Koroks)
	printf("    存档：%s\n", anchor.Path)

	// ── 3. 窄窗扫描 ──
	sayln("\n[3/7] 以锚点为中心扫 ±120m（自动）")
	av := scan.Vec3{X: anchor.Pos[0], Y: anchor.Pos[1], Z: anchor.Pos[2]}
	var baseTotal int
	for attempt := 1; attempt <= 3; attempt++ {
		var baseHits map[uintptr]scan.Hit
		baseHits, baseTotal = scanWindow(p, regions, av, 120)
		printf("    窗口内命中 %d 个坐标槽\n", len(baseHits))
		if len(baseHits) > 0 {
			break
		}
		sayln("    一个都没命中 → 多半是存档太旧，你现在已经离存档点很远了。")
		if !waitEnter("    → 请回游戏里【原地存一次档】，再回来按 Enter 重试:") {
			return fmt.Errorf("用户中止")
		}
		if a2, e2 := pickAnchor(p); e2 == nil {
			anchor = a2
			av = scan.Vec3{X: a2.Pos[0], Y: a2.Pos[1], Z: a2.Pos[2]}
			printf("    新锚点：pos=(%.1f, %.1f, %.1f)\n", av.X, av.Y, av.Z)
		}
	}

	// ── 4. 走动采样 ──
	sayln("\n[4/7] 走动采样 —— 需要你在游戏里走 10 秒")
	var mv []winHit
	for attempt := 1; attempt <= 2; attempt++ {
		if !waitEnter(fmt.Sprintf("    → 回游戏，让林克【持续走动或跑动】约 10 秒（别传送、别骑马、别开菜单），然后按 Enter（第 %d 次）:", attempt)) {
			return fmt.Errorf("用户中止")
		}
		sayln("    采样中，请保持走动（约 5 秒）…")
		var n1, n2 int
		mv, n1, n2 = findMovingInWindow(p, regions, av, 150, 220, 1500*time.Millisecond)
		printf("    两拍窗口命中：%d / %d，位移 0.5~20m 的槽：%d 个\n", n1, n2, len(mv))
		if len(mv) > 0 {
			break
		}
		sayln("    没抓到位移。可能：采样时停住了 / 在菜单或过场 / 在神庙内（本地坐标）。")
	}
	printWinHits(mv, 8)

	// ── 5. 朝向验证 ──
	sayln("\n[5/7] 朝向验证（朝北≈0°，朝东≈90°）")
	haveBest := len(mv) > 0
	if haveBest {
		best := mv[0]
		sayln("    用列表里第一条做验证（rot=true 优先）。")
		type sample struct {
			dir    string
			pos    scan.Vec3
			yr, yc float64
		}
		var ss []sample
		for _, dir := range []string{"正北", "正东"} {
			if !waitEnter(fmt.Sprintf("    → 回游戏，朝【%s】走 3 秒（小地图上方是北），走完按 Enter:", dir)) {
				return fmt.Errorf("用户中止")
			}
			s, ok := readTf(p, best.Addr)
			if !ok {
				printf("    ✗ 0x%X 读不出 Transform 了（可能换场景/传送）\n", best.Addr)
				break
			}
			yr, yc := scan.YawFromMatrix(s.M)
			printf("    [%s] pos=(%.1f, %.1f, %.1f)  rot+%d  yawRow=%.1f°  yawCol=%.1f°\n",
				dir, s.P.X, s.P.Y, s.P.Z, s.RotOff, yr, yc)
			ss = append(ss, sample{dir: dir, pos: s.P, yr: yr, yc: yc})
		}
		if len(ss) == 2 {
			printf("\n    朝北→朝东 角度差：yawRow %+.1f° / yawCol %+.1f°\n",
				angDiff(ss[0].yr, ss[1].yr), angDiff(ss[0].yc, ss[1].yc))
			sayln("    （哪个接近 ±90°，哪个顺序就是对的）")
			printf("    两点水平距离：%.1f m\n", ss[0].pos.Dist2D(ss[1].pos))
		}
	} else {
		sayln("    （第 4 步没抓到候选，跳过）")
	}

	// ── 6. GDB ──
	sayln("\n[6/7] GDB Stub 探测（自动；没开就跳过，不影响定位）")
	for _, line := range gdbstub.Probe(*gdbAddr, 0, 2*time.Second) {
		sayln(line)
	}

	// ── 7. 摘要 ──
	var heapBytes uint64
	for _, r := range regions {
		heapBytes += uint64(r.Size)
	}
	sayln("\n================ 本次实测摘要 ================")
	printf("Eden      : %s\n", winmem.MainWindowTitle(p.PID))
	printf("进程      : pid=%d  %s\n", p.PID, p.ExePath())
	printf("DRAM      : block=0x%X  size=%.2fGB  arenaLow=0x%X\n",
		d.BlockBase, float64(d.BlockSize)/1073741824.0, d.ArenaLow)
	printf("User Heap : %d 块 / %.2fGB\n", len(regions), float64(heapBytes)/1073741824.0)
	printf("存档锚点  : (%.1f, %.1f, %.1f)  playtime=%ds\n", anchor.Pos[0], anchor.Pos[1], anchor.Pos[2], anchor.Playtime)
	printf("窗口命中  : %d 个（±120m）\n", baseTotal)
	if haveBest {
		b := mv[0]
		printf("玩家候选  : host=0x%X  pos=(%.1f, %.1f, %.1f)  位移=%.1fm  rot=%v\n",
			b.Addr, b.P.X, b.P.Y, b.P.Z, b.Delta, b.Rot)
		printf("同位移槽  : %d 个\n", len(mv))
		sayln("\n候选地址附近原始字节（hex + 浮点视图，供离线还原结构布局）：")
		dumpAround(p, b.Addr, 64, 128)
	} else {
		sayln("玩家候选  : 未捕获")
	}
	sayln("=============================================")
	printf("\n日志已保存：\n  %s\n", logPath)
	sayln("已帮你打开文件夹，把这个 .log 发给我就行。")

	openExplorer(logPath)
	return nil
}

// pickAnchor 读存档、返回最可信的锚点；存档不存在时让用户先存档。
func pickAnchor(p *winmem.Process) (saveanchor.Anchor, error) {
	for {
		anchors, roots := saveanchor.Find(p.ExePath())
		if len(anchors) > 0 {
			printf("    找到 %d 个存档（扫过 %d 个根目录）\n", len(anchors), len(roots))
			return anchors[0], nil
		}
		sayln("    没找到可解析的 game_data.sav。")
		for _, r := range roots {
			printf("      扫过：%s\n", r)
		}
		if !waitEnter("    → 请先在游戏里存一次档，然后按 Enter 重试（q 退出）:") {
			return saveanchor.Anchor{}, fmt.Errorf("用户中止")
		}
	}
}

func printWinHits(mv []winHit, top int) {
	if len(mv) == 0 {
		return
	}
	if top > len(mv) {
		top = len(mv)
	}
	printf("\n  %-4s %-14s %-30s %-7s %-7s %-5s %-5s %s\n", "#", "host", "pos(x, y, z)", "delta", "dist", "rot", "hist", "yawRow/yawCol")
	for i := 0; i < top; i++ {
		h := mv[i]
		yaw := "(读取失败)"
		if globalProc != nil {
			if s, ok := readTf(globalProc, h.Addr); ok {
				yr, yc := scan.YawFromMatrix(s.M)
				yaw = fmt.Sprintf("%.1f / %.1f", yr, yc)
			}
		}
		printf("  %-4d %-14s (%9.1f,%8.1f,%9.1f) %-7.2f %-7.1f %-5v %-5v %s\n",
			i+1, fmt.Sprintf("0x%X", h.Addr), h.P.X, h.P.Y, h.P.Z, h.Delta, h.Dist, h.Rot, h.Hist, yaw)
	}
}

// angDiff 角度最短差（-180~180）。
func angDiff(a, b float64) float64 {
	return math.Mod(b-a+540, 360) - 180
}

// waitForGame 循环等「Eden 在跑 + 游戏已载入」。
func waitForGame() (*winmem.Process, winmem.Dram, []winmem.Region, error) {
	for round := 1; round <= 200; round++ {
		pids := winmem.FindPIDs(*procName)
		if len(pids) == 0 {
			printf("    没找到 %s 进程。\n", *procName)
			if !waitEnter("    → 请先启动 Eden，然后按 Enter 重试:") {
				return nil, winmem.Dram{}, nil, fmt.Errorf("用户中止")
			}
			continue
		}
		pid := uint32(*pidFlag)
		if pid == 0 {
			pid = pids[0]
		}
		p, err := winmem.Open(pid)
		if err != nil {
			printf("    打开进程失败: %v\n", err)
			if !waitEnter("    → 若 Eden 是管理员启动的，请用管理员权限运行本程序，然后按 Enter 重试:") {
				return nil, winmem.Dram{}, nil, fmt.Errorf("用户中止")
			}
			continue
		}
		d, _, err := p.FindDram()
		if err != nil {
			printf("    %v\n", err)
			p.Close()
			if !waitEnter("    → 按 Enter 重试:") {
				return nil, winmem.Dram{}, nil, fmt.Errorf("用户中止")
			}
			continue
		}
		regions := scan.HeapRegions(p, d, 64<<10)
		if len(regions) == 0 {
			sayln("    Eden 在跑，但还没进游戏（User Heap 是空的）。")
			p.Close()
			if !waitEnter("    → 进《旷野之息》、读到存档、人物能走动后，按 Enter 继续:") {
				return nil, winmem.Dram{}, nil, fmt.Errorf("用户中止")
			}
			continue
		}
		printf("    ✓ pid=%d  %s\n", pid, p.ExePath())
		printf("    ✓ 窗口：%s\n", winmem.MainWindowTitle(pid))
		return p, d, regions, nil
	}
	return nil, winmem.Dram{}, nil, fmt.Errorf("等待超时")
}

func openExplorer(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	_ = exec.Command("explorer", "/select,"+abs).Start()
}

// ---- 单项诊断命令（与向导共用逻辑） ----

func cmdMoving(p *winmem.Process) {
	hr("移动一致性扫描（锚点窗口版）")
	globalProc = p
	_, regions, err := heapReady(p)
	if err != nil {
		printf("[moving] %v\n", err)
		return
	}
	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) == 0 {
		sayln("[moving] 没有存档锚点，无法缩窗；先跑 -cmd save 看看")
		return
	}
	a := anchors[0]
	av := scan.Vec3{X: a.Pos[0], Y: a.Pos[1], Z: a.Pos[2]}
	printf("[moving] 锚点 %s，窗口 ±150m → ±220m\n", a.VecString())
	sayln("[moving] 现在开始在游戏里走动 10 秒…")
	mv, n1, n2 := findMovingInWindow(p, regions, av, 150, 220, 1500*time.Millisecond)
	printf("[moving] 窗口命中 %d / %d，位移候选 %d\n", n1, n2, len(mv))
	printWinHits(mv, *topFlag)
}

func cmdTfSlots(p *winmem.Process) {
	hr("Transform 签名槽统计（全堆，诊断用）")
	_, regions, err := heapReady(p)
	if err != nil {
		printf("[tf] %v\n", err)
		return
	}
	t0 := time.Now()
	slots, loose := scanTransformSlots(p, regions)
	printf("[tf] 签名槽 %d 个（松散命中 %d 已过滤），耗时 %v\n", len(slots), loose, time.Since(t0).Round(time.Millisecond))

	var world int
	for _, s := range slots {
		if math.Abs(float64(s.P.X)) > 50 || math.Abs(float64(s.P.Z)) > 50 {
			world++
		}
	}
	printf("[tf] 世界尺度 %d 个 / 单位变换 %d 个\n", world, len(slots)-world)
	sayln("[tf] 说明：世界尺度这批里混着大量静态表，不能单独当身份判据 —— 身份判定请用 -cmd save + 窄窗。")
}

func cmdSave() {
	hr("存档锚点")
	exe := ""
	if pids := winmem.FindPIDs(*procName); len(pids) > 0 {
		if p, err := winmem.Open(pids[0]); err == nil {
			exe = p.ExePath()
			p.Close()
		}
	}
	anchors, roots := saveanchor.Find(exe)
	sayln("[save] 扫过的存档根：")
	for _, r := range roots {
		printf("   %s\n", r)
	}
	if len(anchors) == 0 {
		sayln("[save] 没找到任何可解析的 game_data.sav")
		return
	}
	printf("[save] 解析到 %d 个存档（按可信度排序）：\n", len(anchors))
	for i, a := range anchors {
		printf("   %d) pos=%s playtime=%ds korok=%d mtime=%s\n      %s\n",
			i+1, a.VecString(), a.Playtime, a.Koroks, a.MTime.Format("2006-01-02 15:04"), a.Path)
	}
	printf("[save] 采用第 1 条当锚点：%s\n", anchors[0].VecString())
}

// dumpAround 打印 addr 前后一段内存的 hex 与浮点视图。
// 目的：哪怕旋转矩阵/朝向不在我们猜的位置，也能从日志里离线还原结构布局。
func dumpAround(p *winmem.Process, addr uintptr, before, after int) {
	start := addr - uintptr(before)
	buf, err := p.Read(start, before+after)
	if err != nil {
		printf("    dump 失败: %v\n", err)
		return
	}
	for i := 0; i+4 <= len(buf); i += 16 {
		off := i - before
		var hx strings.Builder
		for j := i; j < i+16 && j < len(buf); j++ {
			hx.WriteString(fmt.Sprintf("%02X ", buf[j]))
		}
		var fl strings.Builder
		for j := i; j+4 <= i+16 && j+4 <= len(buf); j += 4 {
			fl.WriteString(fmt.Sprintf("%12.3f", math.Float32frombits(binary.LittleEndian.Uint32(buf[j:]))))
		}
		printf("  %+5d  %-48s %s\n", off, hx.String(), fl.String())
	}
}

// isHistoryCopy 判断某坐标槽是否落在「位置历史副本链」上。
// 判据：±32/±64 字节处还有合法世界坐标，且与当前点相距 < 60m（同物体近期轨迹）。
// 实测样本（2026-10-20）：1126.9,117.0,1800.5 → 1135.5,117.6,1796.9 → 1130.0,117.5,1796.7。
func isHistoryCopy(p *winmem.Process, addr uintptr, self scan.Vec3) bool {
	const pre = 80
	buf, err := p.Read(addr-pre, pre+160)
	if err != nil {
		return false
	}
	hits := 0
	for _, d := range []int{-64, -32, 32, 64} {
		o := pre + d
		if o < 0 || o+12 > len(buf) {
			continue
		}
		v := scan.Vec3{
			X: math.Float32frombits(binary.LittleEndian.Uint32(buf[o:])),
			Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[o+4:])),
			Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[o+8:])),
		}
		if v.Valid() && v.Dist3D(self) < 60 {
			hits++
		}
	}
	return hits >= 2
}

// cmdRingScan 找 PlayerInfo 的「位置环形缓冲」。
//
// 依据 zeldaret/botw 反编译与 DeepWiki（Player Actor 章节）：
// PlayerInfo 含 sead::Vector3f mPlayerPos 与 sead::FixedRingBuffer mPreviousPositions
// （最近 60 个位置）。实测该缓冲以 32 字节步长连续排布 —— 这正是我们之前误判为
// "位置历史副本"的东西。找到它 = 找到 PlayerInfo 区块，之后再也不用坐标猜身份。
func cmdRingScan(p *winmem.Process) {
	hr("位置历史缓冲扫描（PlayerInfo 内的 mPreviousPositions）")
	_, regions, err := heapReady(p)
	if err != nil {
		printf("[ring] %v\n", err)
		return
	}

	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) == 0 {
		sayln("[ring] 没有存档锚点，无法约束；先存一次档再跑。")
		return
	}
	a0 := anchors[0]
	anchor := scan.Vec3{X: a0.Pos[0], Y: a0.Pos[1], Z: a0.Pos[2]}
	printf("[ring] 存档锚点 %s，半径 %.0fm\n", a0.VecString(), *radius)

	// ★ 关键判据（2026-10-20）：用 known 缓存里「当前锁定的玩家坐标」当真相，
	// 找末条坐标精确等于它的位置历史缓冲 —— 这才叫"先认出人"，不是"找坐标像的人"。
	var playerNow scan.Vec3
	hasPlayer := false
	if e, ok := knownpkg.Load().Pick(p.PID, p.StartedAt()); ok {
		if v, valid := scan.ReadVec3(p, e.Addr); valid {
			playerNow, hasPlayer = v, true
			printf("[ring] 玩家当前位置（known 锁定的槽实时读）：(%.1f, %.1f, %.1f)\n", v.X, v.Y, v.Z)
		}
	}
	if !hasPlayer {
		printf("[ring] ⚠ 没有 known 锁定地址 → 只能按存档锚点排序（可能挑错）；\n")
		printf("       先跑一次 enavi.exe 让它锁定并写 known_player.json，再回来跑本命令。\n")
	}

	pass := func() []posRun {
		var runs []posRun
		var mu sync.Mutex
		runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
			var local []posRun
			for off := 0; off+32*8+12 <= len(buf); off += 4 {
				v0 := vecAt(buf, off)
				if !v0.Valid() || v0.Dist3D(anchor) > float64(*radius) {
					continue
				}
				n, prev := 1, v0
				spread := 0.0
				for n < 60 {
					o := off + n*32
					if o+12 > len(buf) {
						break
					}
					v := vecAt(buf, o)
					if !v.Valid() || v.Dist3D(prev) > 200 || v.Dist3D(anchor) > float64(*radius) {
						break
					}
					if dd := v.Dist3D(v0); dd > spread {
						spread = dd
					}
					n++
					prev = v
				}
				if n >= 8 && spread >= 1.0 {
					local = append(local, posRun{addr: addr + uintptr(off), n: n, spread: spread, from: v0, to: prev})
					off += (n - 1) * 32
				}
			}
			if len(local) == 0 {
				return
			}
			mu.Lock()
			runs = append(runs, local...)
			mu.Unlock()
		})
		sort.Slice(runs, func(i, j int) bool { return runs[i].n > runs[j].n })
		return mergeRuns(runs)
	}

	t0 := time.Now()
	first := pass()
	printf("[ring] 第 1 次采样：%d 处，耗时 %v\n", len(first), time.Since(t0).Round(time.Millisecond))
	if len(first) == 0 {
		sayln("[ring] 没命中。把 -r 调大（如 -r 2000），或先在游戏里存一次档。")
		return
	}

	sayln("[ring] 等 2 秒 —— 请在游戏里走动（不要传送）…")
	time.Sleep(2 * time.Second)
	second := pass()

	idx2 := map[uintptr]posRun{}
	for _, r := range second {
		idx2[r.addr] = r
	}
	var list []ringInfo
	for _, r1 := range first {
		r2, ok := idx2[r1.addr]
		if !ok {
			continue
		}
		moved := r2.to.Dist3D(r1.to) > 0.3
		list = append(list, ringInfo{run: r2, last2: r2.to, moved: moved, dAnchor: r2.to.Dist3D(anchor)})
	}
	for i := range list {
		if hasPlayer {
			list[i].dPlayer = list[i].last2.Dist3D(playerNow)
		} else {
			list[i].dPlayer = list[i].dAnchor
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if hasPlayer {
			return list[i].dPlayer < list[j].dPlayer // 末条 ≈ 玩家当前位置者优先
		}
		if list[i].moved != list[j].moved {
			return list[i].moved
		}
		return list[i].dAnchor < list[j].dAnchor
	})

	printf("[ring] 两次都在的缓冲 %d 处；其中「末条坐标变化」的 %d 处\n", len(list), countMoved(list))
	printf("\n  %-4s %-14s %-6s %-6s %-8s %-30s %-9s %s\n", "#", "host", "len", "spread", "末条变化", "末条坐标", "距玩家", "距存档")
	for i, it := range list {
		if i >= 18 {
			printf("  …（省略 %d 处）\n", len(list)-18)
			break
		}
		printf("  %-4d %-14s %-6d %-6.1f %-8v (%9.1f,%8.1f,%9.1f) %-9.1f %.1fm\n",
			i+1, fmt.Sprintf("0x%X", it.run.addr), it.run.n, it.run.spread, it.moved,
			it.last2.X, it.last2.Y, it.last2.Z, it.dPlayer, it.dAnchor)
	}

	if hasPlayer {
		near := 0
		for _, it := range list {
			if it.dPlayer <= 2.0 {
				near++
			}
		}
		printf("\n[ring] ★ 末条坐标距玩家 ≤2m 的缓冲：%d 处", near)
		if near == 1 {
			printf("  → 唯一命中，这就是玩家的位置历史（PlayerInfo 区块找到了）\n")
		} else if near == 0 {
			printf("  → 没有。玩家当前位置可能不在任何缓冲末条（刚传送/读档后缓冲还没更新）\n")
		} else {
			printf("  → 多个，需要玩家走两步再测（真玩家的缓冲会跟着更新）\n")
		}
	}

	if len(list) > 0 {
		top := list[0]
		printf("\n[ring] 最优候选 %s 结构剖析（缓冲头前后）：\n", fmt.Sprintf("0x%X", top.run.addr))
		dissectRing(p, top.run.addr)
	}
	sayln("\n[ring] 判读：真玩家的缓冲应满足「末条变化=true」+「末条坐标≈玩家当前位置」；")
	sayln("     若同时只有一两个满足，它就是 PlayerInfo 的位置历史，可作为稳定锚点。")
}

func countMoved(list []ringInfo) int {
	n := 0
	for _, it := range list {
		if it.moved {
			n++
		}
	}
	return n
}

// dissectRing 剖析环形缓冲候选：往前找缓冲头（指针/容量/长度），往后看条目布局。
func dissectRing(p *winmem.Process, addr uintptr) {
	const pre = 128
	buf, err := p.Read(addr-uintptr(pre), pre+32*12)
	if err != nil {
		printf("    读取失败: %v\n", err)
		return
	}
	printf("    相对偏移   float32 视图                                    int64/指针候选\n")
	for i := pre - 64; i < pre+32*4; i += 16 {
		if i < 0 || i+16 > len(buf) {
			continue
		}
		f := [4]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(buf[i:])),
			math.Float32frombits(binary.LittleEndian.Uint32(buf[i+4:])),
			math.Float32frombits(binary.LittleEndian.Uint32(buf[i+8:])),
			math.Float32frombits(binary.LittleEndian.Uint32(buf[i+12:])),
		}
		p1 := binary.LittleEndian.Uint64(buf[i:])
		p2 := binary.LittleEndian.Uint64(buf[i+8:])
		mark := " "
		if i == pre {
			mark = "★"
		}
		printf("  %s%+5d  %11.3f %11.3f %11.3f %11.3f   0x%X 0x%X\n",
			mark, i-pre, f[0], f[1], f[2], f[3], p1, p2)
	}
}

// vecAt 从 buf 的 off 处读三个 float32 当坐标。
func vecAt(buf []byte, off int) scan.Vec3 {
	if off+12 > len(buf) {
		return scan.Vec3{}
	}
	return scan.Vec3{
		X: math.Float32frombits(binary.LittleEndian.Uint32(buf[off:])),
		Y: math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:])),
		Z: math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:])),
	}
}

// mergeRuns 合并同一段缓冲被多次命中的重叠区间（起点相距 < 512 字节视为同一处）。
func mergeRuns(in []posRun) []posRun {
	var out []posRun
	for _, r := range in {
		merged := false
		for i := range out {
			if absDiff(r.addr, out[i].addr) < 512 {
				if r.n > out[i].n {
					out[i] = r
				}
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n > out[j].n })
	return out
}

func absDiff(a, b uintptr) uintptr {
	if a > b {
		return a - b
	}
	return b - a
}

// posRun 一段连续的"同一物体的位置序列"（环形缓冲的候选）。
type posRun struct {
	addr   uintptr
	n      int
	spread float64
	from   scan.Vec3
	to     scan.Vec3
}

// cmdPlayerInfo 用「心心/体力指纹」直接定位 PlayerInfo 区块。
//
// 这是 Enavi 定位的正确姿势：不猜坐标，先认出 PlayerInfo（单例），
// 再在区块内读 mPlayerPos。指纹来自存档（MaxHartValue / StaminaMax），
// 三个数凑一起在 2.6GB 堆里通常只有个位数命中。
func cmdPlayerInfo(p *winmem.Process) {
	hr("PlayerInfo 指纹定位")
	_, regions, err := heapReady(p)
	if err != nil {
		printf("[pi] %v\n", err)
		return
	}
	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) == 0 {
		sayln("[pi] 没有存档锚点（存档里才有心心/体力值），先进游戏存一次档")
		return
	}
	a := anchors[0]
	printf("[pi] 存档属性：%s  （MaxHartValue=%d, StaminaMax=%.0f）\n",
		a.AttrString(), a.MaxHart, math.Float32frombits(a.StaminaMax))

	fp := scan.NewFingerprint(a.MaxHart, a.CurrentHart,
		math.Float32bits(math.Float32frombits(a.StaminaCurrentMax)),
		math.Float32bits(math.Float32frombits(a.StaminaMax)))
	printf("[pi] 指纹：%s  usable=%v\n", fp.String(), fp.Usable())
	if !fp.Usable() {
		sayln("[pi] 存档里的心心/体力值不可用，无法用指纹法")
		return
	}

	type piHit struct {
		addr uintptr
		kind string
		pos  scan.Vec3
		posOff int
		dHeart float32
	}
	var hits []piHit
	var mu sync.Mutex
	t0 := time.Now()

	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		var local []piHit
		for off := 0; off+12 <= len(buf); off += 4 {
			kind, ok := fp.Match(buf, off)
			if !ok {
				continue
			}
			h := piHit{addr: addr + uintptr(off), kind: kind, posOff: -1}
			// 在指纹前后 ±160 字节里找合法世界坐标（mPlayerPos 应在附近）
			lo, hi := off-160, off+160
			if lo < 0 {
				lo = 0
			}
			if hi > len(buf) {
				hi = len(buf)
			}
			bestD := 1e18
			for q := lo; q+12 <= hi; q += 4 {
				v := vecAt(buf, q)
				if !v.Valid() {
					continue
				}
				d := v.Dist3D(scan.Vec3{X: a.Pos[0], Y: a.Pos[1], Z: a.Pos[2]})
				if d < bestD {
					bestD = d
					h.pos = v
					h.posOff = q - off
				}
			}
			h.dHeart = float32(bestD)
			local = append(local, h)
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		hits = append(hits, local...)
		mu.Unlock()
	})
	printf("[pi] 扫描耗时 %v，指纹命中 %d 处\n", time.Since(t0).Round(time.Millisecond), len(hits))

	if len(hits) == 0 {
		sayln("[pi] 一处都没命中。可能：")
		sayln("     ① 玩家吃过临时黄心 → CurrentHart≠存档值（MaxHart 应仍可靠）")
		sayln("     ② 内存里存的是 int 不是 float，或字段顺序与 DeepWiki 不同")
		sayln("     ③ 该值只存在 gdt::Manager（存档），PlayerInfo 里是另一份副本")
		return
	}

	// 按"离存档坐标最近"排序：mPlayerPos 必然就在玩家附近
	sort.Slice(hits, func(i, j int) bool { return hits[i].dHeart < hits[j].dHeart })
	printf("[pi] 按离存档坐标由近到远：\n")
	fmtStr := "  %-4s %-14s %-4s %-8s %-30s %s\n"
	printf(fmtStr, "#", "host", "kind", "posOff", "最近合法坐标", "距存档")
	for i, h := range hits {
		if i >= 15 {
			printf("  …（省略 %d 处）\n", len(hits)-15)
			break
		}
		printf("  %-4d %-14s %-4s %-8d (%9.1f,%8.1f,%9.1f) %.1fm\n",
			i+1, fmt.Sprintf("0x%X", h.addr), h.kind, h.posOff, h.pos.X, h.pos.Y, h.pos.Z, h.dHeart)
	}

	// 前一名的完整结构
	top := hits[0]
	printf("\n[pi] 最优候选 %s（kind=%s, 坐标在指纹%+d）结构剖析：\n", fmt.Sprintf("0x%X", top.addr), top.kind, top.posOff)
	dissectRing(p, top.addr)
	sayln("\n[pi] 判读：mPlayerPos 应紧邻指纹字段（同属 PlayerInfo）。")
	sayln("     候选唯一或只有两三个 → 指纹法成立，可以直接用它锁定，不再依赖坐标扫描。")
}

// cmdProbeValues 探测「存档里的数值在内存里以什么形式存在」。
// 目的：指纹法 0 命中时，先确认 76 / 3000 这类值到底在不在、是 float 还是 int，
// 以及它们是否在同一个结构里（邻域聚类）。
func cmdProbeValues(p *winmem.Process) {
	hr("存档数值在内存中的形式探测")
	_, regions, err := heapReady(p)
	if err != nil {
		printf("[probe] %v\n", err)
		return
	}
	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) == 0 {
		sayln("[probe] 没有存档锚点")
		return
	}
	a := anchors[0]
	heart := math.Float32bits(float32(a.MaxHart))
	cur := math.Float32bits(float32(a.CurrentHart))
	stam := a.StaminaMax // 已经是 3000.0 的位模式
	printf("[probe] 目标：MaxHart=%.0f(0x%X) CurrentHart=%.0f(0x%X) Stamina=%.0f(0x%X)\n",
		float32(a.MaxHart), heart, float32(a.CurrentHart), cur, math.Float32frombits(stam), stam)

	type bucket struct {
		name string
		bits uint32
	}
	pats := []bucket{
		{"float(心)", heart},
		{"int(心)", a.MaxHart},
		{"float(当前心)", cur},
		{"float(体力)", stam},
	}

	counts := make(map[string]int)
	// 每种类型单独收集样本，避免被高频类型挤满
	samples := make(map[string][]uintptr)
	var mu sync.Mutex
	t0 := time.Now()

	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		type hitRec struct {
			name string
			at   uintptr
		}
		var local []hitRec
		for i := 0; i+4 <= len(buf); i += 4 {
			u := binary.LittleEndian.Uint32(buf[i:])
			for _, pt := range pats {
				if u == pt.bits {
					local = append(local, hitRec{pt.name, addr + uintptr(i)})
				}
			}
		}
		if len(local) == 0 {
			return
		}
		mu.Lock()
		for _, h := range local {
			counts[h.name]++
			if len(samples[h.name]) < 400 {
				samples[h.name] = append(samples[h.name], h.at)
			}
		}
		mu.Unlock()
	})
	printf("[probe] 扫描耗时 %v\n", time.Since(t0).Round(time.Millisecond))
	for _, pt := range pats {
		printf("   %-14s 命中 %d 次\n", pt.name, counts[pt.name])
	}

	// 对每个「float(心)」命中，看 ±512 字节内有没有体力值 + 合法坐标
	anchor := scan.Vec3{X: a.Pos[0], Y: a.Pos[1], Z: a.Pos[2]}
	printf("\n[probe] 对 float(心) 命中点做邻域聚类（±512B 内找 体力=%.0f 与合法坐标）：\n", math.Float32frombits(stam))
	nBoth, nChecked := 0, 0
	for _, addr := range samples["float(心)"] {
		if nChecked >= 60 {
			break
		}
		nChecked++
		buf, err := p.Read(addr-512, 1024)
		if err != nil {
			continue
		}
		stamOff, curOff := -9999, -9999
		for i := 0; i+4 <= len(buf); i += 4 {
			u := binary.LittleEndian.Uint32(buf[i:])
			if u == stam && stamOff == -9999 && (i-512) != 0 {
				stamOff = i - 512
			}
			if u == cur && curOff == -9999 && (i-512) != 0 {
				curOff = i - 512
			}
		}
		posOff, posVal, bestD := -1, scan.Vec3{}, 1e18
		for i := 0; i+12 <= len(buf); i += 4 {
			v := vecAt(buf, i)
			if !v.Valid() {
				continue
			}
			if d := v.Dist3D(anchor); d < bestD {
				bestD, posOff, posVal = d, i-512, v
			}
		}
		if stamOff == -9999 || posOff == -1 {
			continue
		}
		nBoth++
		if nBoth > 12 {
			continue
		}
		printf("   %s  体力@%+5d  当前心@%+5d  坐标@%+5d (%.1f,%.1f,%.1f) 距存档=%.1fm\n",
			fmt.Sprintf("0x%X", addr), stamOff, curOff, posOff, posVal.X, posVal.Y, posVal.Z, bestD)
	}
	printf("\n[probe] 检查了 %d 个点，「体力 + 合法坐标」同框的有 %d 个\n", nChecked, nBoth)
	if nBoth > 0 && nBoth <= 20 {
		sayln("[probe] ✓ 数量少 → PlayerInfo 指纹法可用，可据此唯一锁定")
	} else if nBoth == 0 {
		sayln("[probe] ✗ 心心与体力不在同一结构里 → 心跳值不能单独当 PlayerInfo 指纹")
	} else {
		sayln("[probe] ⚠ 命中偏多 → 还需叠加坐标/指针条件才能唯一")
	}
}

// ringInfo 一处位置历史缓冲的两次采样对比结果。
type ringInfo struct {
	run     posRun
	last2   scan.Vec3
	moved   bool
	dAnchor float64
	dPlayer float64
}
