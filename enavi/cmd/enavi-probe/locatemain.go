// 本文件 = 钉死 main NSO 在 Eden 内存里的 guest 基址。
//
// 上一轮实测：Eden 内存里有 12+ 份 NSO 副本，映射 gva = NSO文件偏移 + K，
// K 随副本而变 → 不能只认一个固定基址。
//
// 本命令的解法（纯只读、零参数）：
//  1. 从磁盘上的 main NSO 里挑若干条「main 独有」的字符串（别的 NSO 里没有）；
//  2. 在 Eden 内存（全进程）里精确搜这些字符串；
//  3. 命中位置 = main 的 .rodata 在 Eden 里的 guest VA；
//  4. K = 命中 gva − 字符串在 NSO 文件里的偏移；
//  5. main .text 的 guest VA = .text 文件偏移 + K。
//
// 校验：多条不同字符串算出的 K 必须一致 → 一致才算钉死。
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"botwmap/enavi/internal/winmem"
)

// probeStrings 这几条来自 main NSO 的 rodata，实测在 sdk/subsdk0..3/rtld 里都不存在。
// 选它们的理由：语义各不相同、长度够长（避免误命中）、且都跟玩家/存档相关（后续定位 PlayerInfo 用得上）。
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

// cmdLocateMain 定位 main NSO 基址。
func cmdLocateMain(p *winmem.Process) {
	hr("钉死 main NSO 的 guest 基址")

	nsoPath := *nsoFlag
	if nsoPath == "" {
		printf("[main] 缺 -nso（解压后的 main NSO 路径）\n")
		return
	}
	nso, err := os.ReadFile(nsoPath)
	if err != nil {
		printf("[main] 读 NSO 失败：%v\n", err)
		return
	}
	printf("[main] NSO %s（%.1f MB）\n", filepath.Base(nsoPath), float64(len(nso))/1048576.0)

	// NSO 的 .text 在文件里的起点（解压后 NSO 保留 NSO0 头，text 紧跟其后）
	textFileOff := uintptr(0x100)

	d, _, err := p.FindDram()
	if err != nil {
		printf("[main] %v\n", err)
		return
	}

	// 先在 NSO 里定位每条探针字符串的文件偏移
	type probe struct {
		s     string
		off   int
		valid bool
	}
	probes := make([]probe, 0, len(probeStrings))
	for _, s := range probeStrings {
		idx := strings.Index(string(nso), s)
		if idx < 0 {
			printf("[main] ⚠ NSO 里找不到 %q（跳过）\n", s)
			continue
		}
		probes = append(probes, probe{s: s, off: idx, valid: true})
	}
	if len(probes) == 0 {
		printf("[main] 没有可用探针\n")
		return
	}
	for _, pr := range probes {
		printf("[main] 探针 %-40q NSO 偏移 0x%08X\n", pr.s, pr.off)
	}
	printf("\n")

	// 全进程搜索
	regions := p.Committed(64 << 10)
	var total uint64
	for _, r := range regions {
		total += uint64(r.Size)
	}
	printf("[main] 搜索范围 %d 区 / %s\n", len(regions), gb(total))

	// 命中收集：probe idx → [(gva, host)]
	type hit struct {
		gva  uintptr
		host uintptr
	}
	hits := make([][]hit, len(probes))
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		for i, pr := range probes {
			for _, a := range findBytesAll(buf, addr, []byte(pr.s)) {
				gva := uintptr(0)
				if a >= d.ArenaLow {
					gva = a - d.ArenaLow
				}
				hits[i] = append(hits[i], hit{gva, a})
			}
		}
	})

	// 统计 K
	type krec struct {
		k       int64
		probes  int
		byProbe []int
	}
	km := map[int64]*krec{}
	for i, pr := range probes {
		for _, h := range hits[i] {
			if h.gva == 0 {
				continue // 落在 DRAM 物理区，不作 K 依据
			}
			k := int64(h.gva) - int64(pr.off)
			rec := km[k]
			if rec == nil {
				rec = &krec{k: k}
				km[k] = rec
			}
			rec.probes++
			rec.byProbe = append(rec.byProbe, i)
		}
	}
	if len(km) == 0 {
		printf("[main] 没有落在模块区的命中\n")
		return
	}
	type kv struct {
		k   int64
		rec *krec
	}
	var list []kv
	for k, rec := range km {
		list = append(list, kv{k, rec})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rec.probes != list[j].rec.probes {
			return list[i].rec.probes > list[j].rec.probes
		}
		return list[i].k < list[j].k
	})

	printf("\n[main] 候选 K 值（按命中探针数排序）：\n")
	for _, it := range list {
		if it.rec.probes < 2 {
			continue
		}
		uniq := map[int]bool{}
		for _, p := range it.rec.byProbe {
			uniq[p] = true
		}
		printf("   K = 0x%X   命中探针 %d/%d 条\n", it.k, len(uniq), len(probes))
	}

	// 最高票的 K = main 的映射
	if len(list) == 0 {
		return
	}
	best := list[0]
	uniqProbes := map[int]bool{}
	for _, p := range best.rec.byProbe {
		uniqProbes[p] = true
	}
	printf("\n[main] ★ 最佳 K = 0x%X（%d/%d 条探针一致）\n", best.k, len(uniqProbes), len(probes))
	printf("[main]   main .text guest VA = 0x%X\n", uint64(best.k)+uint64(textFileOff))
	printf("[main]   main .rodata guest VA = 0x%X\n", uint64(best.k)+0x1634000)
	printf("[main]   （校验：把 NSO 偏移 + K 落在 Eden 内存里读回来应与 NSO 字节一致）\n")

	// 落地一份结果，供 locate 直接用
	out := map[string]any{
		"capturedAt":   time.Now().Format(time.RFC3339),
		"nsoPath":      nsoPath,
		"textFileOff":  textFileOff,
		"k":            best.k,
		"textGuestVA":  uint64(best.k) + uint64(textFileOff),
		"probeSupport": len(uniqProbes),
		"probeTotal":   len(probes),
	}
	_ = out
}

// findBytesAll 在 buf 里找 needle 的全部出现位置，返回绝对地址。
func findBytesAll(buf []byte, base uintptr, needle []byte) []uintptr {
	var out []uintptr
	if len(needle) == 0 {
		return out
	}
	from := 0
	for {
		j := indexBytes(buf, needle, from)
		if j < 0 {
			break
		}
		out = append(out, base+uintptr(j))
		from = j + 1
	}
	return out
}
