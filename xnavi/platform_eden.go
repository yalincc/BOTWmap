// platform_eden.go — Eden 平台驱动（模块化隔离，独立自包含）。
//
// Eden = Yuzu fork（Switch 模拟器），进程名精确 "eden"。
// 与 Cemu/Ryujinx 的本质差异（live-eden/engine v1.0.0 已验证固化）：
//   - 内存碎片化（实测 25082 区域 / 24.4GB），玩家坐标槽落在 0.44MB 微型区域，
//     不能沿用"只扫最大块"策略——必须扫全部 RW 区域（64KB 下限去噪）；
//   - 无固定偏移（金手指链 0x02D1EA00 在 Eden 上确认不可用，见开发计划阶段 0），
//     定位 = 存档锚点窗口扫描 + 分组打分（struct>0 → copies 降序 → dist 升序）；
//   - 坐标槽会话内稳定、场景切换漂移 → 绝对地址缓存 + 失效重扫；
//   - 占位 Actor 池海量重复（copies 可达百万级）会霸榜 → 占位过滤 + junkGroup 丢弃。
//
// 隔离设计：本文件实现 Platform 接口 + Eden 专用定位（edenLocator 接口），
// 状态机通过类型断言分支调用，Cemu/Ryujinx 不实现该接口、路径零改动——
// 防止 TOTK 事故重演（多套定位策略在同一状态机里互相干扰）。

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"
)

// edenLocator Eden 平台专用定位接口（仅 edenPlatform 实现）。
// watch.go 用类型断言分支调用；Cemu/Ryujinx 不实现 → 默认路径不变。
type edenLocator interface {
	// EdenLocate 执行 Eden 定位：
	//   bypassKnown=false：先试绝对地址缓存（秒锁），失败再锚点窗口扫描；
	//   bypassKnown=true：跳过缓存，强制全量锚点窗口扫描（冻结重扫用）。
	// 失败返回 nil。
	EdenLocate(bypassKnown bool) *LocateResult
	// EdenSaveKnown 移动确认后写入绝对地址缓存（神庙槽拒绝）。
	EdenSaveKnown(addr uintptr)
}

type edenPlatform struct {
	h   uintptr
	pid uint32
}

func init() {
	registerPlatform(&edenPlatform{})
}

func (p *edenPlatform) Name() string { return "Eden" }

// Attach 精确匹配 "eden"（排除自身与含 "core" 的进程——core/gui 都可能
// 以 eden 开头，误 attach 自己会锁死定位，live-eden engine 踩坑固化）。
func (p *edenPlatform) Attach() bool {
	pid := edenFindPid()
	if pid == 0 {
		if p.h != 0 {
			closeHandle(p.h)
		}
		p.h, p.pid = 0, 0
		return false
	}
	if pid == p.pid && p.h != 0 {
		if hLive(p.h) {
			return true
		}
		closeHandle(p.h)
		p.h = 0
	}
	if p.h != 0 {
		closeHandle(p.h)
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		p.h, p.pid = 0, 0
		return false
	}
	p.h, p.pid = h, pid
	return true
}

func (p *edenPlatform) Handle() uintptr      { return p.h }
func (p *edenPlatform) PID() uint32         { return p.pid }
func (p *edenPlatform) LittleEndian() bool  { return true }

// edenFindPid 进程名精确匹配 "eden"（不含 .exe，不区分大小写），
// 排除自身与 core 进程（live-eden engine winapi.go 同款）。
func edenFindPid() uint32 {
	self := uint32(os.Getpid())
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapprocess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return 0
	}
	defer closeHandle(snap)
	var e ProcessEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		name := strings.ToLower(utf16ToString(e.ExeFile[:]))
		if e.ProcessID != self && !strings.Contains(name, "core") &&
			strings.TrimSuffix(name, ".exe") == "eden" {
			return e.ProcessID
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return 0
}

// Blocks Eden 全部已提交 RW 区域（64KB 下限去噪，MAPPED + PRIVATE）。
// 与 Cemu/Ryujinx 的"只取大块"不同：Eden 玩家槽可能落在微型区域，必须全扫。
// 注意：0 块检测（Blocks(256.0)）仍返回大块 → 游戏在跑即非空，语义兼容。
func (p *edenPlatform) Blocks(minMB float64) []MemBlock {
	return edenGuestBlocks(p.h, minMB)
}

// LargestBlock 最大 RW 块（known 兼容字段；Eden 快路径走绝对地址缓存，不依赖）。
func (p *edenPlatform) LargestBlock(minMB float64) (uintptr, uintptr) {
	best, bestSize := uintptr(0), uintptr(0)
	for _, b := range edenGuestBlocks(p.h, minMB) {
		if b.Size > bestSize {
			best, bestSize = b.Base, b.Size
		}
	}
	return best, bestSize
}

// edenSaveDirOverride 由 --eden-save-dir= 指定（GUI 设置"Eden 存档路径"），
// 直接作为存档根，优先于一切自动探测。
var edenSaveDirOverride string

func setEdenSaveDirOverride(d string) { edenSaveDirOverride = d }

// SaveRoots Eden 存档根候选：环境变量 → GUI 手动路径 → 运行中 eden.exe 目录推导
// → 盘符便携布局 → APPDATA。返回多个候选（全部存在则首个命中；调用方递归找 game_data.sav）。
func (p *edenPlatform) SaveRoots() []string {
	var roots []string
	if v := os.Getenv("BOTW_SAVE_DIR"); v != "" {
		roots = append(roots, v)
	}
	if sd := edenSaveDirOverride; sd != "" {
		// GUI 设置"Eden 存档路径"（直接存档根，BOTW 为 nand/user/save 下含 01007EF00011E000 的目录）
		roots = append(roots, sd)
	}
	if p.h != 0 {
		if exe := edenProcessPath(p.h); exe != "" {
			exeDir := filepath.Dir(exe)
			roots = append(roots,
				filepath.Join(exeDir, "user", "nand", "user", "save"),
				filepath.Join(exeDir, "..", "user", "nand", "user", "save"),
				filepath.Join(exeDir, "..", "..", "user", "nand", "user", "save"))
		}
	}
	for _, drive := range []string{"C:\\", "D:\\", "E:\\", "F:\\", "G:\\", "H:\\", "I:\\", "J:\\", "K:\\", "L:\\"} {
		for _, sub := range []string{
			filepath.Join(drive, "YUZU", "eden", "user", "nand", "user", "save"),
			filepath.Join(drive, "yuzu", "eden", "user", "nand", "user", "save"),
			filepath.Join(drive, "YUZU", "user", "nand", "user", "save"),
		} {
			roots = append(roots, sub)
		}
	}
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		appdata = os.Getenv("USERPROFILE") + "\\AppData\\Roaming"
	}
	roots = append(roots,
		filepath.Join(appdata, "Eden", "user", "nand", "user", "save"),
		filepath.Join(appdata, "yuzu", "user", "nand", "user", "save"))
	return roots
}

// edenProcessPath 进程主模块完整路径（存档目录推导用）。
func edenProcessPath(h uintptr) string {
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 || size == 0 {
		return ""
	}
	return string(utf16.Decode(buf[:size]))
}

var (
	edenArenaMu        sync.Mutex
	cachedEdenPid      uint32
	cachedEdenArenaLow uintptr
)

// edenFindArenaLow 寻找 Switch 虚拟机的 4GB MEM_MAPPED DRAM 区，反解 arenaLow。
// Switch 在宿主 Windows 上通过唯一的 4GB MEM_MAPPED 提交区映射 DRAM，其后紧随 512GB Switch 虚拟地址空间。
func edenFindArenaLow(h uintptr) uintptr {
	if h == 0 {
		return 0
	}
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		if !vqRetry(h, addr, &mbi) {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		if mbi.State == memCommit && mbi.Type == memMapped && size == (1<<32) {
			return base + (1 << 32)
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	return 0
}

func edenGetArenaLow(h uintptr, pid uint32) uintptr {
	edenArenaMu.Lock()
	defer edenArenaMu.Unlock()
	if cachedEdenPid == pid && cachedEdenArenaLow != 0 {
		return cachedEdenArenaLow
	}
	al := edenFindArenaLow(h)
	if al != 0 {
		cachedEdenPid = pid
		cachedEdenArenaLow = al
	}
	return al
}

// ---- Eden 内存枚举 ----

// edenGuestBlocksWithOptions 枚举 Eden 可读写内存区域。
// heapOnly=true：仅返回落在 Switch 虚拟机用户态堆空间（User Heap：[arenaLow + 0x2000000000, arenaLow + 0x3000000000)）的块。
//   在 Switch 64位 Horizon OS 中，所有动态游戏 Actor（包括林克角色对象与其坐标）全部分配在 User Heap 中。
//   该空间在宿主仅约 2.65GB（相比全宿主 19.3GB 剔除了 86% 的显卡贴图/宿主杂质），秒级扫描关键！
// heapOnly=false：枚举全宿主 ≥64KB 的 RW 区域（用于最后的兜底安全扫描）。
func edenGuestBlocksWithOptions(h uintptr, minMB float64, heapOnly bool, pid uint32) []MemBlock {
	if h == 0 {
		return nil
	}
	minSize := uintptr(minMB * 1048576.0)
	const floor = uintptr(64 * 1024)
	if minSize < floor {
		minSize = floor
	}
	var heapLo, heapHi uintptr
	if heapOnly {
		al := edenGetArenaLow(h, pid)
		if al != 0 {
			heapLo = al + uintptr(0x2000000000)
			heapHi = al + uintptr(0x3000000000)
		}
	}

	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	if heapLo != 0 {
		addr = heapLo
	}
	var out []MemBlock
	for addr < limit {
		if heapHi != 0 && addr >= heapHi {
			break
		}
		var mbi MemoryBasicInformation
		if !vqRetry(h, addr, &mbi) {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit && (mbi.Type == memMapped || mbi.Type == memPrivate) &&
			(p == 0x02 || p == 0x04) && size >= minSize {
			out = append(out, MemBlock{base, size})
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	return out
}

// edenGuestBlocks 兼容旧调用（全宿主枚举）。
func edenGuestBlocks(h uintptr, minMB float64) []MemBlock {
	return edenGuestBlocksWithOptions(h, minMB, false, 0)
}

// ---- Eden 定位：存档锚点 → 窗口扫描 → 分组打分（engine locate.go 移植）----

// edenScanRegion 读进程内存后扫描一块区域。
// window>0：窗口模式（仅收集窗口内命中，玩家≈存档位置时最快）；
// window<=0：全内存 sane 模式（收集全部合法坐标三元组，玩家走远也能定位，
// 探针验证路径：全内存 sane 扫 → 距存档锚点最近的命中 = 玩家坐标）。
// 两种模式都做占位 Actor 池预过滤（Eden 特有）：近零跳过 + 三值近似相等跳过。
func edenScanRegion(h uintptr, base, size uintptr, refs [][3]float32, window float32, hits *[]Hit) {
	var gx0, gx1, gh0, gh1, gz0, gz1 float32
	windowed := window > 0
	if windowed && len(refs) > 0 {
		gx0, gx1 = refs[0][0]-window, refs[0][0]+window
		gh0, gh1 = refs[0][1]-window, refs[0][1]+window
		gz0, gz1 = refs[0][2]-window, refs[0][2]+window
	}
	off := uintptr(0)
	for off < size {
		if len(*hits) > hitCap {
			return
		}
		n := uintptr(chunkSize)
		if size-off < n {
			n = size - off
		}
		buf := readMem(h, base+off, int(n))
		if buf == nil || len(buf) < 12 {
			off += n
			continue
		}
		a := floats(buf)
		for i := 0; i+3 <= len(a); i++ {
			x, y, z := a[i], a[i+1], a[i+2]
			// 占位池预过滤（与 engine scanRegion 同判据）：
			// 近零三元组（|x|,|y|,|z| <5）= 未初始化槽；真实坐标 |x|,|y| 都 >5。
			if x < 5 && x > -5 && y < 5 && y > -5 {
				continue
			}
			if z < 5 && z > -5 {
				continue
			}
			// 三值近似相等 = 占位池主特征（玩家真坐标三轴几乎不可能两两相等到 0.5m）。
			if abs32(x-y) < 0.5 && abs32(y-z) < 0.5 {
				continue
			}
			// 坐标合法性预校验（全内存模式必须：不合法三元组不进候选，
			// 否则"距锚点最近"会被引擎内部小向量/垃圾值污染）。
			if !gameValidTriple(x, y, z) {
				continue
			}
			if windowed {
				if x <= gx0 || x >= gx1 || y <= gh0 || y >= gh1 || z <= gz0 || z >= gz1 {
					continue
				}
			}
			if windowed {
				bestD, bestRi := float32(1e18), 0
				for ri, r := range refs {
					dx, dy, dz := x-r[0], y-r[1], z-r[2]
					d := dx*dx + dy*dy + dz*dz
					if d < bestD {
						bestD, bestRi = d, ri
					}
				}
				if bestD <= window*window {
					*hits = append(*hits, Hit{base + off + uintptr(i)*4, x, y, z, bestRi, float32(math.Sqrt(float64(bestD)))})
				}
			} else {
				// 全内存模式：dist 统一按主锚计算（调用方按 dist 升序取最近）。
				bestD := float32(1e18)
				if len(refs) > 0 {
					r := refs[0]
					dx, dy, dz := x-r[0], y-r[1], z-r[2]
					bestD = dx*dx + dy*dy + dz*dz
				}
				*hits = append(*hits, Hit{base + off + uintptr(i)*4, x, y, z, 0, float32(math.Sqrt(float64(bestD)))})
			}
		}
		off += n
	}
}

// edenScanAll 并发扫描全部区域（worker pool）。
// 注意：不跳过任何大块——Eden 玩家坐标槽会跨块漂移（2026-10-07 实测从 1GB 块
// 漂到 4GB 块），跳过 ≥2GB 巨型块会漏掉玩家；全扫 8.5GB 实测 0.7s 可接受。
func edenScanAll(h uintptr, blocks []MemBlock, refs [][3]float32, window float32, hits *[]Hit) {
	type job struct{ base, size uintptr }
	var jobs []job
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(chunkSize)
			if b.Size-off < n {
				n = b.Size - off
			}
			jobs = append(jobs, job{b.Base + off, n})
			off += n
		}
	}
	workers := runtime.NumCPU()
	if workers > 6 {
		workers = 6
	}
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	ch := make(chan job)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []Hit
			for j := range ch {
				edenScanRegion(h, j.base, j.size, refs, window, &local)
			}
			if len(local) > 0 {
				mu.Lock()
				*hits = append(*hits, local...)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

// edenJunkGroup 占位池垃圾组判定（engine junkGroup 原样移植）：
// 海量重复 + 无结构 / 三值近似相等 / 原点附近小值 / 北轴近零 + 中小坐标。
// 占位池 copies 可达数万-百万、struct 全 0，不丢弃会把玩家组压出 top30。
func edenJunkGroup(g *Group) bool {
	if g.Copies > 18000 && g.Struct == 0 {
		return true
	}
	if abs32(g.X-g.Y) < 0.5 && abs32(g.Y-g.Z) < 0.5 {
		return true
	}
	if abs32(g.X) < 20 && abs32(g.Y) < 20 && abs32(g.Z) < 20 {
		return true
	}
	if abs32(g.Z) < 5 && abs32(g.X) < 500 && abs32(g.Y) < 500 {
		return true
	}
	return false
}

// edenGroupAndRank 分组打分：先扩 top80 检查 struct，丢弃 junk 组，再截断 top30。
// 排序唯一规则（engine 与 xnavi 双端已验证）：struct>0 门槛 → copies 降序 → dist 升序。
func edenGroupAndRank(h uintptr, hits []Hit) ([]*Group, []ShortlistEntry) {
	groups := map[[3]int32]*Group{}
	for _, hit := range hits {
		k := [3]int32{
			int32(math.Round(float64(hit.X) * 10)),
			int32(math.Round(float64(hit.Y) * 10)),
			int32(math.Round(float64(hit.Z) * 10)),
		}
		g := groups[k]
		if g == nil {
			g = &Group{Ri: hit.Ri, Dist: hit.Dist, X: hit.X, Y: hit.Y, Z: hit.Z}
			groups[k] = g
		}
		g.Addrs = append(g.Addrs, hit.Addr)
		if hit.Dist < g.Dist {
			g.Dist, g.Ri = hit.Dist, hit.Ri
		}
	}
	var byCopies []*Group
	for _, g := range groups {
		g.Copies = len(g.Addrs)
		byCopies = append(byCopies, g)
	}
	// 截断（Eden 特有，与 Cemu/Ryujinx 相反）：
	//   玩家/NPC 槽 copies 小（dump 实证玩家 copies=1），静态物体组 copies 大
	//   （81~301 草/树/装饰多实例）。copies 降序截断会把玩家组（copies=1）
	//   挤出 top80——copies=1 的组在 1.6 万命中里有几千个。
	//   改为 copies 升序截断：活动槽优先保留，静态大组自然排后。
	sort.Slice(byCopies, func(i, j int) bool { return byCopies[i].Copies < byCopies[j].Copies })
	top := byCopies
	if len(top) > 80 {
		top = top[:80]
	}
	for _, g := range top {
		// 检查组内全部地址（并发扫描 hits 顺序不确定，截断采样会漏矩阵槽 → struct=0）。
		for _, a := range g.Addrs {
			if rotOK(h, a) {
				g.Struct++
			}
		}
	}
	kept := top[:0]
	for _, g := range top {
		if edenJunkGroup(g) {
			continue
		}
		kept = append(kept, g)
	}
	if len(kept) > 30 {
		kept = kept[:30]
	}
	// 排序（Eden 与 Cemu/Ryujinx 相反的关键差异）：
	//   Cemu/Ryujinx copies 降序——固定偏移场景 copies 多=活跃槽；
	//   Eden copies 升序——玩家槽 copies=1（2026-10-07 dump 实证），静态物体
	//   组 copies=81~301（草/树/装饰多实例）降序时霸榜把玩家挤出 top80。
	//   唯一规则：struct>0 门槛 → copies 升序 → dist 升序。
	sort.Slice(kept, func(i, j int) bool {
		a, b := kept[i], kept[j]
		aLive, bLive := a.Struct > 0, b.Struct > 0
		if aLive != bLive {
			return aLive
		}
		if a.Copies != b.Copies {
			return a.Copies > b.Copies
		}
		return a.Dist < b.Dist
	})
	shortlist := make([]ShortlistEntry, 0, len(kept))
	for _, g := range kept {
		shortlist = append(shortlist, ShortlistEntry{
			Hud:    [3]float32{g.X, g.Y, g.Z},
			Copies: g.Copies,
			Struct: g.Struct,
			Dist:   g.Dist,
			Slot:   g.Ri,
			Addrs:  g.Addrs[:min(len(g.Addrs), 24)],
		})
	}
	return kept, shortlist
}

// edenGroupHasValid 检查组内前 8 个地址是否存在合法坐标（锁定前校验）。
// Eden 内存活跃，单地址瞬时全零/垃圾常见——只测 Addrs[0] 会把最大簇误判
// invalid 然后 fallback 到次优半死槽（engine 2026-10-07 实测踩坑）。
func edenGroupHasValid(h uintptr, g *Group) bool {
	n := len(g.Addrs)
	if n > 8 {
		n = 8
	}
	for i := 0; i < n; i++ {
		if currentGame.DecodeAt(h, g.Addrs[i]) != nil {
			return true
		}
	}
	return false
}

// edenLiveGroups 组级移动检测：只对 top 分组（前 12 组）的代表地址采样。
// 两次采样间隔 150ms。若玩家正在走动，坐标发生变化的组标记为活动组。
// 耗时仅 ~150ms，大幅替代旧版全候选逐个采样的数十秒卡顿。
func edenLiveGroups(h uintptr, ranked []*Group) []*Group {
	if len(ranked) == 0 {
		return nil
	}
	nGroups := len(ranked)
	if nGroups > 12 {
		nGroups = 12
	}
	type sample struct {
		addr uintptr
		pos  [3]float32
	}
	first := make([]sample, nGroups)
	for i := 0; i < nGroups; i++ {
		g := ranked[i]
		if len(g.Addrs) > 0 {
			first[i].addr = g.Addrs[0]
			if d := currentGame.DecodeAt(h, g.Addrs[0]); d != nil {
				first[i].pos = [3]float32{d[0], d[1], d[2]}
			}
		}
	}
	time.Sleep(150 * time.Millisecond)
	var live []*Group
	for i := 0; i < nGroups; i++ {
		if first[i].addr == 0 {
			continue
		}
		d1 := currentGame.DecodeAt(h, first[i].addr)
		if d1 == nil {
			continue
		}
		if dist3(first[i].pos, [3]float32{d1[0], d1[1], d1[2]}) > 0.5 {
			live = append(live, ranked[i])
		}
	}
	return live
}

// pickEdenGroup 从分组打分结果选出最佳结构候选组（兼容站桩静止与轻度位移）。
func pickEdenGroup(h uintptr, ranked []*Group, refs [][3]float32) *Group {
	for _, g := range ranked {
		if g.Struct <= 0 {
			continue
		}
		limit := len(g.Addrs)
		if limit > 8 {
			limit = 8
		}
		for i := 0; i < limit; i++ {
			d := currentGame.DecodeAt(h, g.Addrs[i])
			if d == nil {
				continue
			}
			// 宽容差 40m：确保组内地址与组代表基本一致，防止严重错位
			if dist3([3]float32{d[0], d[1], d[2]}, [3]float32{g.X, g.Y, g.Z}) < 40.0 {
				g.Addrs[0], g.Addrs[i] = g.Addrs[i], g.Addrs[0]
				return g
			}
		}
	}
	return nil
}

// edenPlaceholderCoord 占位池坐标特征：三轴近整数（±0.02）。
func edenPlaceholderCoord(x, y, z float32) bool {
	return abs32(x-float32(math.Round(float64(x)))) < 0.02 &&
		abs32(y-float32(math.Round(float64(y)))) < 0.02 &&
		abs32(z-float32(math.Round(float64(z)))) < 0.02
}

// edenFilterHits 候选初筛：
//   - alt≈0：静态装饰；
//   - 整数三轴：占位池未激活 actor 默认位置；
//   - 三轴近相等：占位初始值。
func edenFilterHits(hits []Hit) []Hit {
	out := hits[:0]
	for _, hit := range hits {
		if abs32(hit.Y) < 1.0 {
			continue
		}
		if edenPlaceholderCoord(hit.X, hit.Y, hit.Z) {
			continue
		}
		if abs32(hit.X-hit.Y) < 0.5 && abs32(hit.Y-hit.Z) < 0.5 {
			continue
		}
		out = append(out, hit)
	}
	return out
}

// EdenLocate 实现 edenLocator 接口（Eden 阶梯式极速定位路径）。
// Tier 0: known 绝对地址秒锁（bypassKnown=false 时，读 valid 且非神庙槽即锁，耗时 0ms）；
// Tier 1 & 2: Switch 虚拟机 User Heap 空间定向窄窗初筛（仅 2.65GB 真实堆，±120m → ±400m，耗时 0.2~0.5s）；
// Tier 3: 仅当堆空间未命中时，回退全宿主 19GB 盲扫安全兜底。
func (p *edenPlatform) EdenLocate(bypassKnown bool) *LocateResult {
	if p.h == 0 || !hLive(p.h) {
		if !p.Attach() {
			return nil
		}
	}
	h := p.h

	// Tier 0: 绝对地址缓存秒锁（进游戏站桩秒锁快通道，耗时 0ms）
	if !bypassKnown {
		for _, a := range edenKnownAddrs() {
			d := currentGame.DecodeAt(h, a)
			if d == nil {
				continue
			}
			if edenShrinePos([3]float32{d[0], d[1], d[2]}) {
				continue
			}
			return &LocateResult{Addr: a, Copies: 0, Hud: [3]float32{d[0], d[1], d[2]}}
		}
	}

	// 存档锚点读取
	anchors := currentGame.SaveAnchors(p)
	if len(anchors) == 0 {
		fmt.Println("  [eden] no usable save anchor found - retrying")
		return nil
	}
	refs := [][3]float32{anchors[0].Pos}
	fmt.Printf("  [eden] save anchor: mem=(%.1f, %.1f, %.1f)\n", anchors[0].Pos[0], anchors[0].Pos[1], anchors[0].Pos[2])

	t0 := time.Now()
	const edenScanMinMB = 0.0625 // 64KB 下限：Eden 内存碎片化，必须扫 64KB 以上全部微型块

	var best *Group
	var shortlist []ShortlistEntry

	// Tier 1 & 2: 优先定向在 Switch User Heap (2.65GB 真实堆) 中搜索
	al := edenGetArenaLow(h, p.pid)
	if al != 0 {
		heapBlocks := edenGuestBlocksWithOptions(h, edenScanMinMB, true, p.pid)
		var heapBytes uintptr
		for _, b := range heapBlocks {
			heapBytes += b.Size
		}
		fmt.Printf("  [eden] [heap] Switch User Heap: %d regions (%.2f GB) [arena=0x%X]\n",
			len(heapBlocks), float64(heapBytes)/1073741824.0, al)

		for _, win := range []float32{120.0, 400.0} {
			tPass := time.Now()
			var hits []Hit
			edenScanAll(h, heapBlocks, refs, win, &hits)
			fmt.Printf("  [eden] [heap] pass win=±%.0fm: %d hits in %.2fs\n", win, len(hits), time.Since(tPass).Seconds())
			hits = edenFilterHits(hits)
			if len(hits) == 0 {
				continue
			}
			ranked, sl := edenGroupAndRank(h, hits)
			if len(ranked) == 0 {
				continue
			}
			shortlist = sl

			// 玩家在动 → 优先锁活动组
			live := edenLiveGroups(h, ranked)
			if len(live) > 0 {
				best = live[0]
				fmt.Printf("  [eden] lock moving group 0x%012X copies=%d struct=%d mem=(%.1f, %.1f, %.1f)\n",
					best.Addrs[0], best.Copies, best.Struct, best.X, best.Y, best.Z)
				break
			}

			// 玩家站桩不动 → 选首个合法结构组
			best = pickEdenGroup(h, ranked, refs)
			if best != nil {
				fmt.Printf("  [eden] lock static candidate 0x%012X copies=%d struct=%d mem=(%.1f, %.1f, %.1f)\n",
					best.Addrs[0], best.Copies, best.Struct, best.X, best.Y, best.Z)
				break
			}
		}
	}

	// Tier 3: 全宿主 19GB 盲扫（仅当 User Heap 空间两轮均未命中时安全兜底）
	if best == nil {
		if al != 0 {
			fmt.Printf("  [eden] heap search missed after %.2fs, falling back to full host scan (19GB)...\n", time.Since(t0).Seconds())
		}
		tFallback := time.Now()
		allBlocks := edenGuestBlocksWithOptions(h, edenScanMinMB, false, 0)
		var total uintptr
		for _, b := range allBlocks {
			total += b.Size
		}
		fmt.Printf("  [eden] [fallback] full host RW regions: %d (%.1f GB)\n", len(allBlocks), float64(total)/1073741824.0)

		for _, win := range []float32{120.0, 400.0} {
			var hits []Hit
			edenScanAll(h, allBlocks, refs, win, &hits)
			fmt.Printf("  [eden] [fallback] pass win=±%.0fm: %d hits in %.1fs\n", win, len(hits), time.Since(tFallback).Seconds())
			hits = edenFilterHits(hits)
			if len(hits) == 0 {
				continue
			}
			ranked, sl := edenGroupAndRank(h, hits)
			if len(ranked) == 0 {
				continue
			}
			shortlist = sl

			live := edenLiveGroups(h, ranked)
			if len(live) > 0 {
				best = live[0]
				break
			}
			best = pickEdenGroup(h, ranked, refs)
			if best != nil {
				break
			}
		}

		// 终极全内存 sane 扫描
		if best == nil {
			var allHits []Hit
			edenScanAll(h, allBlocks, refs, -1, &allHits)
			fmt.Printf("  [eden] fallback full-RAM scan: %d candidates in %.1fs\n", len(allHits), time.Since(tFallback).Seconds())
			allHits = edenFilterHits(allHits)
			if len(allHits) > 0 {
				ranked, sl := edenGroupAndRank(h, allHits)
				shortlist = sl
				live := edenLiveGroups(h, ranked)
				if len(live) > 0 {
					best = live[0]
				} else {
					best = pickEdenGroup(h, ranked, refs)
				}
			}
		}
	}

	if best == nil {
		fmt.Println("  [eden] FAILED: no confident candidate")
		return nil
	}

	addr := best.Addrs[0]
	var hud [3]float32
	if d := currentGame.DecodeAt(h, addr); d != nil {
		hud = [3]float32{d[0], d[1], d[2]}
	}
	fmt.Printf("  [eden] locked 0x%012X copies=%d struct=%d total %.1fs\n",
		addr, best.Copies, best.Struct, time.Since(t0).Seconds())

	return &LocateResult{
		Addr:      addr,
		Copies:    best.Copies,
		Struct:    best.Struct,
		Hud:       hud,
		Shortlist: shortlist,
	}
}

// EdenSaveKnown 移动确认后写入绝对地址缓存（神庙槽拒绝，knownSlotOK 同款）。
func (p *edenPlatform) EdenSaveKnown(addr uintptr) {
	if p.h == 0 {
		return
	}
	d := currentGame.DecodeAt(p.h, addr)
	if d == nil {
		return
	}
	if edenShrinePos([3]float32{d[0], d[1], d[2]}) {
		return
	}
	edenKnownAdd(addr)
}

// edenShrinePos 判定坐标是否神庙/洞穴本地坐标（远离大地图锚点簇）。
// 判据（engine isShrinePos 同款）：距最近存档锚点 >800m = 本地场景坐标。
// 本地坐标三轴都小（<100）已被 gameValidTriple 拒绝，这里兜底"坐标合法但
// 明显不在任何存档位置附近"的场景（如神庙本地大坐标）。
func edenShrinePos(pos [3]float32) bool {
	anchors := currentGame.SaveAnchors(currentPlatform())
	if len(anchors) == 0 {
		return false
	}
	best := float32(1e18)
	for _, a := range anchors {
		dx, dy, dz := pos[0]-a.Pos[0], pos[1]-a.Pos[1], pos[2]-a.Pos[2]
		d := dx*dx + dy*dy + dz*dz
		if d < best {
			best = d
		}
	}
	return math.Sqrt(float64(best)) > 800.0
}

// ---- Eden 绝对地址缓存（known_eden_botw.json，独立于 xnavi 的块+偏移模式）----
//
// Eden 无固定 guest DRAM 块锚点（内存碎片化），"块基址+偏移"无意义；
// 坐标槽绝对地址跨会话是否稳定由 ASLR 决定：稳定 → 秒锁；不稳定 → 读失败
// 自动回落锚点窗口扫描（bypassKnown 语义天然覆盖）。

const edenKnownFile = "known_eden_botw.json"
const edenKnownCap = 10
const edenKnownEvictFail = 3

type edenKnownData struct {
	Addrs   []string          `json:"addrs"`
	Fails   map[string]int    `json:"fails,omitempty"`
	Updated string            `json:"updated"`
}

func edenKnownPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), edenKnownFile)
}

// edenKnownAddrs 读绝对地址缓存（去重）。
// v1 不做失败淘汰（地址试读每次定位至多 10 次 RPM，成本可忽略）；
// 失效地址由 known 冻结 → FrozenRescanAfter → 全量重扫自愈，新地址经
// EdenSaveKnown 写入覆盖。Fails 字段保留（未来接 known 锁确认窗口用）。
func edenKnownAddrs() []uintptr {
	var d edenKnownData
	if data, err := os.ReadFile(edenKnownPath()); err == nil {
		json.Unmarshal(data, &d)
	}
	var out []uintptr
	add := func(v uintptr) {
		if v > 0x10000 && v < 0x7FFFFFFFFFFF {
			for _, x := range out {
				if x == v {
					return
				}
			}
			out = append(out, v)
		}
	}
	for _, v := range d.Addrs {
		add(parseHex(v))
	}
	return out
}

// edenKnownAdd 追加一个绝对地址（去重 + cap 截断 + 保留失败计数）。
func edenKnownAdd(addr uintptr) {
	var d edenKnownData
	if data, err := os.ReadFile(edenKnownPath()); err == nil {
		json.Unmarshal(data, &d)
	}
	seen := map[uintptr]bool{}
	var list []string
	for _, a := range edenKnownAddrs() {
		if a == addr || seen[a] || d.Fails[hex8(a)] >= edenKnownEvictFail {
			continue
		}
		seen[a] = true
		list = append(list, hex8(a))
	}
	if !seen[addr] {
		list = append([]string{hex8(addr)}, list...)
	}
	if len(list) > edenKnownCap {
		list = list[:edenKnownCap]
	}
	d.Addrs = list
	d.Updated = time.Now().Format("2006-01-02 15:04:05")
	buf, _ := json.MarshalIndent(d, "", "  ")
	os.WriteFile(edenKnownPath(), buf, 0644)
}

// edenKnownFail 记录一次读失败；连续 3 次移出缓存（地址失效：ASLR 变化/槽回收）。
func edenKnownFail(addr uintptr) {
	var d edenKnownData
	if data, err := os.ReadFile(edenKnownPath()); err == nil {
		json.Unmarshal(data, &d)
	}
	if d.Fails == nil {
		d.Fails = map[string]int{}
	}
	d.Fails[hex8(addr)]++
	var list []string
	for _, v := range d.Addrs {
		if d.Fails[v] < edenKnownEvictFail {
			list = append(list, v)
		}
	}
	d.Addrs = list
	d.Updated = time.Now().Format("2006-01-02 15:04:05")
	buf, _ := json.MarshalIndent(d, "", "  ")
	os.WriteFile(edenKnownPath(), buf, 0644)
	fmt.Printf("  [eden-known] addr %s fail=%d (evict>=%d)\n", hex8(addr), d.Fails[hex8(addr)], edenKnownEvictFail)
}
