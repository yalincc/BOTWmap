// 本文件 = NSO 字节级交叉验证的取数部分。
//
// 目的：证明「Eden 内存里的代码 = 磁盘上那份 NSO 的原始 ARM64 机器码」，
// 而不只是「看起来像 ARM64」。
//
// 手法：把 Eden 代码区（只读段）整段 dump 出来，再拿解压后的 NSO 原始字节
// 去里面做精确匹配。命中即证明逐字节相同 ⇒ 静态分析可直接在 Eden 内存上做。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"botwmap/enavi/internal/winmem"
)

// cmdCodeDump 把 Eden 里所有"只读大段"（NSO 段所在处）整段导出到 report/。
// 产物 = 一个 .bin（各段顺序拼接）+ 一个 .idx（每段在 bin 里的偏移与宿主地址）。
func cmdCodeDump(p *winmem.Process) {
	hr("代码区整段导出（供 NSO 字节级比对）")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[codedump] %v\n", err)
		return
	}
	outDir := filepath.Join("report")
	os.MkdirAll(outDir, 0o755)
	stamp := time.Now().Format("20060102-150405")

	type seg struct {
		base uintptr
		size uintptr
		prot uint32
	}
	var segs []seg
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.Readable() && (r.Protect&0xFF) == winmem.PageReadOnly && r.Size >= 256<<10 {
			segs = append(segs, seg{r.Base, r.Size, r.Protect})
		}
		return false
	})
	// 也把 DRAM 里的大只读段算上（NSO 可能被放在那）
	p.Regions(d.BlockBase, d.BlockBase+d.BlockSize, func(r winmem.Region) bool {
		if r.Readable() && (r.Protect&0xFF) == winmem.PageReadOnly && r.Size >= 8<<20 {
			segs = append(segs, seg{r.Base, r.Size, r.Protect})
		}
		return false
	})
	sort.Slice(segs, func(i, j int) bool { return segs[i].base < segs[j].base })
	if len(segs) == 0 {
		printf("[codedump] 没找到只读大段\n")
		return
	}

	binPath := filepath.Join(outDir, fmt.Sprintf("codedump-%s.bin", stamp))
	idxPath := filepath.Join(outDir, fmt.Sprintf("codedump-%s.idx", stamp))
	bf, err := os.Create(binPath)
	if err != nil {
		printf("[codedump] 创建失败：%v\n", err)
		return
	}
	defer bf.Close()
	idx, _ := os.Create(idxPath)
	defer idx.Close()

	var written uint64
	fmt.Fprintf(idx, "# arenaLow=0x%X blockBase=0x%X\n", d.ArenaLow, d.BlockBase)
	fmt.Fprintf(idx, "# bin_offset\tguest_va\thost_addr\tsize\n")
	for i, s := range segs {
		buf, err := p.Read(s.base, int(s.size))
		if err != nil {
			printf("[codedump] 段 %d (0x%X) 读取失败：%v\n", i, s.base, err)
			continue
		}
		gva := uintptr(0)
		if s.base >= d.ArenaLow {
			gva = s.base - d.ArenaLow
		}
		fmt.Fprintf(idx, "%d\t0x%X\t0x%X\t%d\n", written, gva, s.base, len(buf))
		bf.Write(buf)
		written += uint64(len(buf))
		printf("[codedump] 段 %d: host=0x%X gva=0x%X size=%s → bin 偏移 %d\n",
			i, s.base, gva, gb(uint64(len(buf))), written-uint64(len(buf)))
	}
	printf("[codedump] 合计 %s → %s\n", gb(written), binPath)
	printf("[codedump] 索引 → %s\n", idxPath)
}
