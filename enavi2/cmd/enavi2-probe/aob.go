// AOB：指令字节特征码验证（.text 代码段取样，实机搜索）。
//
// 背景（2026-10-21 M1 实测）：字符串探针不可用——
//   1) 探针字符串在 NSO 里位于 .data 段（0x15A2100 起），运行期被游戏拷进多个字符串池，
//      实机每个探针 3~5 份命中，各副本相对偏移与文件不一致 → 无法算 K；
//   2) 正解：取 .text 代码段（0x100 起 ~22.6MB）的指令字节做 AOB。
//      指令是执行码，运行时不会进字符串池；副本 = NSO 镜像副本（enavi 实测 12+ 份）。
//
// 本命令：从 NSO 的 .text 区取 N 个样本（每样本 32 字节），全进程搜索，
// 输出每个样本的命中数与 K（gva − fileOff）聚类。判据：
//   - 样本命中唯一/可筛（≤10）；
//   - 或命中多份但 K 高度一致（同一布局的多副本），可从多命中里挑执行副本。
package main

import (
	"flag"
	"os"
	"sort"

	"botwmap/enavi2/internal/winmem"
)

var (
	aobSamples  = flag.Int("nsamples", 8, "取样样本数")
	aobLen      = flag.Int("alength", 32, "每个样本字节数")
	textStart   = flag.Int64("tstart", 0x100, "text 段文件偏移起点")
	textSize    = flag.Int64("tsize", 0, "text 段文件长度（0=自动：文件大小−tstart）")
)

// cmdAOB 指令字节 AOB 验证。
func cmdAOB() {
	hr("AOB 指令字节特征码验证")

	// 附加 Eden
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
	}
	p, err := winmem.Open(pid)
	if err != nil {
		printf("[attach] Open 失败：%v\n", err)
		return
	}
	defer p.Close()
	printf("[attach] pid=%d %s\n", pid, p.ExePath())

	d, _, err := p.FindDram()
	if err != nil {
		printf("[dram] %v\n", err)
		return
	}
	printf("[dram] DRAM block=0x%X size=%s arenaLow=0x%X\n",
		d.BlockBase, gb(uint64(d.BlockSize)), d.ArenaLow)

	if *nsoFlag == "" {
		printf("[nso] 请给 -nso <main_uncompressed.nso>\n")
		return
	}
	nso, err := os.ReadFile(*nsoFlag)
	if err != nil {
		printf("[nso] 读文件失败：%v\n", err)
		return
	}
	size := *textSize
	if size <= 0 {
		size = int64(len(nso)) - *textStart
	}
	textEnd := *textStart + size
	printf("[nso] %s（%.1f MB），text 区 [0x%X, 0x%X)\n",
		*nsoFlag, float64(len(nso))/1048576.0, *textStart, textEnd)

	// 取样：等距覆盖 text 前中后，跳过含过多 0x00 的位置（不太像指令）
	type sample struct {
		label string
		off   int64
		bytes []byte
	}
	var samples []sample
	span := size - int64(*aobLen)
	if span <= 0 {
		printf("[nso] text 区太小\n")
		return
	}
	step := span / int64(*aobSamples+1)
	for i := 1; i <= *aobSamples; i++ {
		off := *textStart + step*int64(i)
		if off+int64(*aobLen) > textEnd {
			break
		}
		b := nso[off : off+int64(*aobLen)]
		zeros := 0
		for _, c := range b {
			if c == 0 {
				zeros++
			}
		}
		if zeros > len(b)/2 {
			continue // 全是 0，跳过
		}
		samples = append(samples, sample{printfSafe("T+0x%X", off), off, b})
	}
	if len(samples) == 0 {
		printf("[nso] 没有可用样本（text 区全 0？）\n")
		return
	}
	printf("[scan] 样本 %d 个（每个 %d 字节），全进程搜索（并发 %d，块间休眠 %s）\n",
		len(samples), *aobLen, *workers, *scanDelay)

	// 搜索
	regions := p.Committed(64 << 10)
	type hit struct {
		host uintptr
		gva  uintptr
		zone string
	}
	hits := make([][]hit, len(samples))
	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		for i, s := range samples {
			for _, a := range findBytesAll(buf, addr, s.bytes) {
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

	// 输出
	printf("\n--- 命中明细 ---\n")
	for i, s := range samples {
		hs := hits[i]
		printf("样本 %s（fileOff 0x%X）→ %d 处命中\n", s.label, s.off, len(hs))
		if len(hs) <= 10 {
			for _, h := range hs {
				printf("   host=0x%X gva=0x%X  %s\n", h.host, h.gva, h.zone)
			}
		} else {
			for _, h := range hs[:10] {
				printf("   host=0x%X gva=0x%X  %s\n", h.host, h.gva, h.zone)
			}
			printf("   ... 其余 %d 处略\n", len(hs)-10)
		}
	}

	// K 聚类（K = gva − fileOff，跨样本一致 = 同一布局多副本）
	km := map[int64][]string{}
	for i, s := range samples {
		for _, h := range hits[i] {
			if h.gva == 0 {
				continue
			}
			k := int64(h.gva) - s.off
			km[k] = append(km[k], s.label)
		}
	}
	printf("\n--- K 值聚类（gva − fileOff）---\n")
	if len(km) == 0 {
		printf("（无有效 K）\n")
		return
	}
	type kv struct {
		k   int64
		n   int
		sub []string
	}
	var list []kv
	for k, sub := range km {
		list = append(list, kv{k, len(sub), sub})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for _, it := range list {
		printf("   K = 0x%X   覆盖样本 %d/%d（%v）\n", it.k, it.n, len(samples), it.sub)
	}
	if len(list) > 0 && list[0].n >= 2 {
		printf("\n★ 判据：K = 0x%X 覆盖 %d/%d 样本 → 存在统一布局的代码镜像\n", list[0].k, list[0].n, len(samples))
		printf("   main .text guest VA ≈ 0x%X（K + textStart 0x%X）\n", uint64(list[0].k)+uint64(*textStart), *textStart)
		printf("   命中唯一 → AOB 特征码成立；命中多份但 K 一致 → 用 R-X 权限/区域特征挑执行副本\n")
	} else {
		printf("⚠ 无 ≥2 样本支持的 K：样本间布局不一致，指令 AOB 也存疑\n")
	}
}

// printfSafe 生成样本标签（避免直接依赖 fmt 包，保持 main.go 统一）。
func printfSafe(format string, a ...any) string { return sprintf(format, a...) }
