// M1：特征码唯一性验证（AOB 码源实机确认）。
//
// 目标：回答「磁盘 NSO 里选的特征码，在 Eden 实机内存里命中是否唯一/可筛」。
// 输出判据：
//   1) 每个特征码命中数量（期望唯一或 ≤10 可筛）；
//   2) K 值聚类（gva − NSO 文件偏移）——多条探针支持同一 K = main 基址钉死（Step 2 一并验证）。
//
// 参考来源：enavi/cmd/enavi-probe/locatemain.go（K 值法）与 findbytes.go（搜索与分区），
// 复制后按 enavi2 独立实现改造；enavi 原目录未动。
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strings"

	"botwmap/enavi2/internal/winmem"
)

// probeStrings main 独有探针（离线已验证：在 sdk/subsdk0..3/rtld 里不存在）。
var probeStrings = []string{
	"WarpPlayer(%.1f, %.1f, %.1f)",
	"GameDataFloatPlayerDirectionY",
	"PlayerUpdateOnlyBottomComebackPos",
	"cCalcPrio_PlayerBefore::preCalc",
	"RideHorsePlayerHoldTransOffset",
	"DiscoverPlayer_Npc_Kakariko008",
	"ReduceMaxSpeedChasePlayer",
	"IsTrgTargetChangeToPlayer",
	"TerrorDistAfterPlayerRescue",
	"UseCameraFrontByTargetPlayer",
}

// nsoSeg NSO0 头里的一个段（文件偏移/大小 + 内存偏移/大小）。
// 关键：文件偏移 ≠ 内存偏移，K 必须用「内存偏移」算，否则多探针 K 对不上。
type nsoSeg struct {
	fileOff, fileSize int64
	memOff, memSize   int64
}

// parseNsoHeader 解析 NSO0 头（0x100 字节），返回 text/rodata/data 三段。
func parseNsoHeader(data []byte) (text, rodata, dataSeg nsoSeg, ok bool) {
	if len(data) < 0x100 {
		return
	}
	u := func(o int) int64 { return int64(binary.LittleEndian.Uint32(data[o:])) }
	text = nsoSeg{u(0x10), u(0x14), u(0x38), u(0x3C)}
	rodata = nsoSeg{u(0x18), u(0x1C), u(0x40), u(0x44)}
	dataSeg = nsoSeg{u(0x20), u(0x24), u(0x48), u(0x4C)}
	ok = true
	return
}

// memOffOf 给定文件偏移，返回它在内存里的偏移（段内换算）；找不到返回 -1。
func memOffOf(fileOff int64, segs ...nsoSeg) int64 {
	for _, s := range segs {
		if s.fileSize <= 0 {
			continue
		}
		if fileOff >= s.fileOff && fileOff < s.fileOff+s.fileSize {
			return s.memOff + (fileOff - s.fileOff)
		}
	}
	return -1
}

func cmdM1() {
	hr("M1 特征码唯一性验证")

	// 0. 附加 Eden
	var pid uint32
	if *pidFlag > 0 {
		pid = uint32(*pidFlag)
	} else {
		pids := winmem.FindPIDs("eden")
		if len(pids) == 0 {
			printf("[attach] 没找到 eden.exe 进程，请先启动 Eden 并进游戏\n")
			return
		}
		pid = pids[0]
		if len(pids) > 1 {
			printf("[attach] 找到 %d 个 eden 进程，取第一个 pid=%d\n", len(pids), pid)
		}
	}
	p, err := winmem.Open(pid)
	if err != nil {
		printf("[attach] Open 失败：%v\n", err)
		return
	}
	defer p.Close()
	printf("[attach] pid=%d %s\n", pid, p.ExePath())

	// 1. DRAM 反解
	d, _, err := p.FindDram()
	if err != nil {
		printf("[dram] %v\n", err)
		return
	}
	printf("[dram] DRAM block=0x%X size=%s arenaLow=0x%X\n",
		d.BlockBase, gb(uint64(d.BlockSize)), d.ArenaLow)

	// 2. 构造特征码（needles）
	type needle struct {
		label  string
		bytes  []byte
		fileOff int64 // 在 NSO 文件里的偏移（-1 表示无）
		memOff  int64 // 换算后的内存偏移（K 值用；-1 表示无）
	}
	var needles []needle
	var textSeg, rodataSeg, dataSeg nsoSeg
	if *nsoFlag != "" {
		nso, err := os.ReadFile(*nsoFlag)
		if err != nil {
			printf("[nso] 读文件失败：%v\n", err)
			return
		}
		printf("[nso] %s（%.1f MB）\n", *nsoFlag, float64(len(nso))/1048576.0)
		var ok bool
		textSeg, rodataSeg, dataSeg, ok = parseNsoHeader(nso)
		if !ok {
			printf("[nso] ⚠ NSO 头解析失败（<0x100 字节）\n")
			return
		}
		printf("[nso] 段表：text file[0x%X,0x%X) mem[0x%X,0x%X) | rodata file[0x%X,0x%X) mem[0x%X,0x%X) | data file[0x%X,0x%X) mem[0x%X,0x%X)\n",
			textSeg.fileOff, textSeg.fileOff+textSeg.fileSize, textSeg.memOff, textSeg.memOff+textSeg.memSize,
			rodataSeg.fileOff, rodataSeg.fileOff+rodataSeg.fileSize, rodataSeg.memOff, rodataSeg.memOff+rodataSeg.memSize,
			dataSeg.fileOff, dataSeg.fileOff+dataSeg.fileSize, dataSeg.memOff, dataSeg.memOff+dataSeg.memSize)
		for _, s := range probeStrings {
			off := int64(strings.Index(string(nso), s))
			if off < 0 {
				printf("[nso] ⚠ NSO 里找不到探针 %q（跳过）\n", s)
				continue
			}
			mem := memOffOf(off, textSeg, rodataSeg, dataSeg)
			if mem < 0 {
				printf("[nso] ⚠ 探针 %q 不在任何段内（跳过）\n", s)
				continue
			}
			needles = append(needles, needle{s, []byte(s), off, mem})
		}
		if len(needles) == 0 {
			printf("[nso] 没有可用探针，改用 -bytes\n")
		}
	}
	if *bytesFlag != "" {
		var raw []byte
		s := strings.TrimSpace(*bytesFlag)
		s = strings.ReplaceAll(s, " ", "")
		s = strings.ReplaceAll(s, "??", "00") // 通配符暂不支持，先按 00 占位提示
		for i := 0; i+1 < len(s); i += 2 {
			var b byte
			if _, err := fmt.Sscanf(s[i:i+2], "%02X", &b); err != nil {
				printf("[bytes] hex 解析失败（第 %d 字符）\n", i)
				return
			}
			raw = append(raw, b)
		}
		if len(raw) > 0 {
			needles = append(needles, needle{"bytes", raw, -1, -1})
		}
	}
	if len(needles) == 0 {
		printf("[输入] 请给 -nso（默认 10 条探针字符串）或 -bytes <hex>\n")
		return
	}

	// 3. 全进程搜索（限速并发）
	regions := p.Committed(64 << 10)
	var total uint64
	for _, r := range regions {
		total += uint64(r.Size)
	}
	printf("[scan] 搜索范围 %d 区 / %s，特征码 %d 个，并发 %d，块间休眠 %s\n",
		len(regions), gb(total), len(needles), *workers, *scanDelay)

	type hit struct {
		host uintptr
		gva  uintptr
		zone string
	}
	hits := make([][]hit, len(needles))
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		for i, nd := range needles {
			for _, a := range findBytesAll(buf, addr, nd.bytes) {
				gva := uintptr(0)
				zone := "?"
				switch {
				case a >= d.BlockBase && a < d.BlockBase+d.BlockSize:
					zone = "DRAM物理"
				case a >= d.ArenaLow && a < d.HeapLo():
					zone = "模块区"
					gva = a - d.ArenaLow
				case a >= d.HeapLo() && a < d.HeapHi():
					zone = "UserHeap"
					gva = a - d.ArenaLow
				}
				hits[i] = append(hits[i], hit{a, gva, zone})
			}
		}
	})

	// 4. 输出命中 + 判据
	printf("\n--- 命中明细 ---\n")
	verdict := "✓ 全部唯一 / 可筛"
	for i, nd := range needles {
		hs := hits[i]
		printf("特征码 %q（fileOff 0x%X → memOff 0x%X）→ %d 处命中\n", nd.label, nd.fileOff, nd.memOff, len(hs))
		if len(hs) <= 12 {
			for _, h := range hs {
				printf("   host=0x%X gva=0x%X  %s\n", h.host, h.gva, h.zone)
			}
		} else {
			printf("   （前 12 处）\n")
			for _, h := range hs[:12] {
				printf("   host=0x%X gva=0x%X  %s\n", h.host, h.gva, h.zone)
			}
			printf("   ... 其余 %d 处略\n", len(hs)-12)
		}
		if len(hs) > 10 {
			verdict = "⚠ 命中过多（>10），需换更长的特征码或加版本判别"
		}
	}

	// 5. K 值聚类（K = gva − memOff，跨探针一致才算钉死）
	km := map[int64]int{} // K → 支持的探针数
	kProbe := map[int64][]int{}
	for i, nd := range needles {
		if nd.memOff < 0 {
			continue
		}
		for _, h := range hits[i] {
			if h.gva == 0 {
				continue // 落在 DRAM 物理区（未换算 gva），不作 K 依据
			}
			k := int64(h.gva) - nd.memOff
			km[k]++
			kProbe[k] = append(kProbe[k], i)
		}
	}
	printf("\n--- K 值聚类（gva − memOff，跨探针一致才算钉死）---\n")
	if len(km) == 0 {
		printf("（无有效 K：所有命中都落在 DRAM 物理区或 gva=0）\n")
		return
	}
	type kv struct {
		k   int64
		n   int
		ips []int
	}
	var list []kv
	for k, n := range km {
		list = append(list, kv{k, n, kProbe[k]})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for _, it := range list {
		if it.n < 2 {
			continue
		}
		printf("   K = 0x%X   支持 %d/%d 条探针（#%v）\n", it.k, it.n, len(probeStrings), it.ips)
	}
	if len(list) > 0 && list[0].n >= 2 {
		best := list[0]
		printf("\n★ 判据：K = 0x%X 由 %d 条探针一致支持 → main 基址钉死\n", best.k, best.n)
		printf("   main .text guest VA ≈ 0x%X（K + text.memOff 0x%X）\n", uint64(best.k)+uint64(textSeg.memOff), textSeg.memOff)
		printf("   main .rodata guest VA ≈ 0x%X（K + rodata.memOff 0x%X）\n", uint64(best.k)+uint64(rodataSeg.memOff), rodataSeg.memOff)
		printf("   下次启动重跑本命令，K 若不变 → 跨会话稳定（Step 2 通过）\n")
	} else {
		printf("⚠ 无 ≥2 条探针支持的 K：要么副本太多、要么探针选得不好，需换特征码\n")
	}

	printf("\n=== M1 结论：%s ===\n", verdict)
}
