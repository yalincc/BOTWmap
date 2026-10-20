// 本文件 = 「原始 ARM64 是否在 Eden 内存里」的全进程字节搜索。
//
// 用法：
//   enavi-probe -cmd findbytes -bytes <hex> [-span 1MB]
//   enavi-probe -cmd findbytes -nso <main_uncompressed.nso> [-nsooff 0x100]
//
// 命中即证明：Eden 内存里存在与磁盘 NSO 逐字节相同的 ARM64 机器码。
package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

var (
	bytesFlag = flag.String("bytes", "", "要搜索的十六进制字节串")
	nsoFlag   = flag.String("nso", "", "NSO 文件（解压后）；配合 -nsooff 从该文件取样")
	nsoOff    = flag.Int64("nsooff", 0x100, "从 NSO 文件的哪个偏移取样")
	sampleLen = flag.Int("blen", 64, "取样长度（字节）")
	nsamples  = flag.Int("count", 8, "取几个样本")
)

// cmdFindBytes 全进程搜索字节序列。
func cmdFindBytes(p *winmem.Process) {
	var needles [][]byte
	var labels []string

	if *nsoFlag != "" {
		data, err := os.ReadFile(*nsoFlag)
		if err != nil {
			printf("[find] 读 NSO 失败：%v\n", err)
			return
		}
		// 取若干个分散样本，覆盖 .text 前中后
		span := int64(len(data)) - int64(*sampleLen)
		if span <= 0 {
			printf("[find] NSO 太小\n")
			return
		}
		step := span / int64(*nsamples+1)
		for i := 1; i <= *nsamples; i++ {
			off := *nsoOff + step*int64(i)
			if off+int64(*sampleLen) > int64(len(data)) {
				break
			}
			needles = append(needles, data[off:off+int64(*sampleLen)])
			labels = append(labels, fmt.Sprintf("NSO+0x%X", off))
		}
	}
	if *bytesFlag != "" {
		raw, err := hex.DecodeString(strings.TrimSpace(*bytesFlag))
		if err != nil {
			printf("[find] hex 解析失败：%v\n", err)
			return
		}
		needles = append(needles, raw)
		labels = append(labels, "命令行指定")
	}
	if len(needles) == 0 {
		printf("[find] 请给 -nso 或 -bytes\n")
		return
	}

	d, _, err := p.FindDram()
	if err != nil {
		printf("[find] %v\n", err)
		return
	}

	// 搜索范围：全部已提交可读区（≥64KB）
	regions := p.Committed(64 << 10)
	var total uint64
	for _, r := range regions {
		total += uint64(r.Size)
	}
	printf("[find] 搜索范围 %d 区 / %s，样本 %d 个（每个 %d 字节）\n",
		len(regions), gb(total), len(needles), *sampleLen)

	// 用流式匹配：把每段读进内存，用 index 找
	type hitRec struct {
		label string
		at    uintptr
	}
	var hits []hitRec
	seen := map[string]bool{}

	runChunks(p, regions, *workers, func(addr uintptr, buf []byte) {
		for i, nd := range needles {
			if len(nd) == 0 {
				continue
			}
			from := 0
			for {
				j := indexBytes(buf, nd, from)
				if j < 0 {
					break
				}
				at := addr + uintptr(j)
				key := fmt.Sprintf("%s@%X", labels[i], at)
				if !seen[key] {
					seen[key] = true
					hits = append(hits, hitRec{labels[i], at})
				}
				from = j + 1
			}
		}
	})

	printf("\n[find] 命中 %d 处：\n", len(hits))
	for i, h := range hits {
		if i >= 40 {
			printf("   ...（其余 %d 处略）\n", len(hits)-40)
			break
		}
		zone := "?"
		switch {
		case h.at >= d.BlockBase && h.at < d.BlockBase+d.BlockSize:
			zone = "DRAM物理"
		case h.at >= d.ArenaLow && h.at < d.HeapLo():
			zone = "模块区"
		case h.at >= d.HeapLo() && h.at < d.HeapHi():
			zone = "UserHeap"
		}
		gva := uintptr(0)
		if h.at >= d.ArenaLow {
			gva = h.at - d.ArenaLow
		}
		printf("   [%s] host=0x%X gva=0x%X  %s\n", h.label, h.at, gva, zone)
	}
	if len(hits) == 0 {
		printf("   （无命中）\n")
	}
}

// indexBytes 在 buf 里找 needle（朴素实现，够用）。
func indexBytes(buf, needle []byte, from int) int {
	if len(needle) == 0 || from < 0 || from+len(needle) > len(buf) {
		return -1
	}
	n := len(needle)
	first := needle[0]
	last := len(buf) - n
	for i := from; i <= last; i++ {
		if buf[i] != first {
			continue
		}
		ok := true
		for j := 1; j < n; j++ {
			if buf[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

var _ = binary.LittleEndian
var _ = scan.FindASCII
var _ = strconv.Itoa
