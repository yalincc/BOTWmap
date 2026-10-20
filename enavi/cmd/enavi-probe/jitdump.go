// 本文件 = JIT 可分析性验证（老大指定的「第一轮」）。
//
// 要回答的问题只有一句：Eden 进程内存里的代码，还能不能当「原始 ARM64」来静态分析？
//
// 背景：Eden 是模拟器，游戏 NSO 里的 ARM64 机器码要么
//   (A) 仍以原始字节形式留在 guest 内存（可被 AArch64 反汇编器直接读）
//   (B) 只剩宿主侧 LLVM 翻出来的 x86-64 JIT 产物（ARM64 语义已丢）
//
// 判定口径（本文件负责取证据，判定由 tools/jit_analyze.py 做）：
//  1. 在 Eden 内存里找 NSO 头魔数（NSO0 / NRO0 / MOD0）→ 锚出模块基址候选；
//  2. 把候选段各 dump 1MB 到 report/；
//  3. 顺带把宿主侧「可执行」大段也 dump 1MB —— 若那里是 JIT 产物，
//     AArch64 反汇编必然崩，而 x86-64 反汇编正常，这就等于拿到了 (B) 的实锤。
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

// jitCandidate 一个 dump 候选。
type jitCandidate struct {
	Tag      string // 短标签，写进文件名
	Addr     uintptr
	Size     uintptr
	GuestVA  uintptr // 若落在 arenaLow 之后则是 guest VA，否则 0
	Why      string  // 为什么挑它
	Path     string  // dump 文件路径
	Prot     uint32
	RegionSz uintptr
}

// cmdJitDump 主体。
func cmdJitDump(p *winmem.Process) {
	hr("JIT 可分析性验证：取代码段样本")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[jit] %v\n", err)
		return
	}
	printf("[jit] DRAM base=0x%X size=%.2fGB  arenaLow=0x%X\n",
		d.BlockBase, float64(d.BlockSize)/1073741824.0, d.ArenaLow)

	outDir := filepath.Join("report")
	os.MkdirAll(outDir, 0o755)
	stamp := time.Now().Format("20060102-150405")

	var cands []jitCandidate

	// ── 候选 1：guest 模块区里 prot=0x02（只读）的段 ──
	// 期望：NSO 的 .text/.rodata 就在这里。若是原始 ARM64，AArch64 反汇编立刻见效。
	hr("候选来源 1：模块区只读段（prot=0x02）—— 期望是 NSO 原始段")
	type roRegion struct {
		r winmem.Region
	}
	var ros []roRegion
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.Readable() && (r.Protect&0xFF) == winmem.PageReadOnly && r.Size >= 256<<10 {
			ros = append(ros, roRegion{r})
		}
		return false
	})
	sort.Slice(ros, func(i, j int) bool { return ros[i].r.Size > ros[j].r.Size })
	total := uint64(0)
	for _, x := range ros {
		total += uint64(x.r.Size)
	}
	printf("[jit] 模块区只读段 %d 个 / 合计 %s\n", len(ros), gb(total))
	for i, x := range ros {
		if i >= 8 {
			printf("   ...（其余 %d 个略）\n", len(ros)-8)
			break
		}
		printf("   %s gva=0x%X size=%s\n", x.r, x.r.Base-d.ArenaLow, gb(uint64(x.r.Size)))
	}
	for i, x := range ros {
		if i >= 3 {
			break
		}
		cands = append(cands, jitCandidate{
			Tag:   fmt.Sprintf("ro%d", i),
			Addr:  x.r.Base,
			Size:  x.r.Size,
			Why:   fmt.Sprintf("模块区只读段 #%d（.text 首选）", i),
			Prot:  x.r.Protect,
			RegionSz: x.r.Size,
		})
	}
	// guest VA 记上
	for i := range cands {
		if cands[i].Addr >= d.ArenaLow {
			cands[i].GuestVA = cands[i].Addr - d.ArenaLow
		}
	}

	// ── 候选 2：NSO 魔数 ──
	hr("候选来源 2：NSO/NRO/MOD0 魔数（锚模块基址）")
	for _, magic := range []string{"NSO0", "NRO0", "MOD0"} {
		hits := findMagic(p, d, magic)
		if len(hits) == 0 {
			printf("[jit] %s：未找到\n", magic)
			continue
		}
		for _, h := range hits {
			gva := uintptr(0)
			if h >= d.ArenaLow {
				gva = h - d.ArenaLow
			}
			printf("[jit] %s 命中 host=0x%X gva=0x%X（%s）\n", magic, h, gva, fmtAddr(d, h))
			cands = append(cands, jitCandidate{
				Tag:      "magic_" + magic,
				Addr:     h,
				GuestVA:  gva,
				Why:      magic + " 魔数位置，往后 0x1000 大概率是 .text",
				RegionSz: 0,
			})
		}
	}

	// ── 候选 3：宿主侧可执行段（JIT 产物所在地）──
	hr("候选来源 3：宿主侧可执行大段（= LLVM JIT 产物区，用来确认 (B) 面貌）")
	var exec []winmem.Region
	p.Regions(0, 0, func(r winmem.Region) bool {
		pr := r.Protect & 0xFF
		if r.State == winmem.MemCommit && (pr == 0x20 || pr == 0x40) && r.Size >= 1<<20 {
			exec = append(exec, r)
		}
		return false
	})
	sort.Slice(exec, func(i, j int) bool { return exec[i].Size > exec[j].Size })
	printf("[jit] 宿主可执行大段（≥1MB）%d 个\n", len(exec))
	for i, r := range exec {
		if i >= 8 {
			printf("   ...（其余 %d 个略）\n", len(exec)-8)
			break
		}
		printf("   %s size=%s\n", r, gb(uint64(r.Size)))
	}
	if len(exec) > 0 {
		cands = append(cands, jitCandidate{
			Tag:   "hostx",
			Addr:  exec[0].Base,
			Size:  exec[0].Size,
			Why:   "宿主侧最大可执行段（JIT 产物嫌疑）",
			Prot:  exec[0].Protect,
			RegionSz: exec[0].Size,
		})
	}

	// ── dump ──
	hr("dump 1MB 样本")
	const dumpSize = 1 << 20
	for i := range cands {
		c := &cands[i]
		addr := c.Addr
		if strings.HasPrefix(c.Tag, "magic_") {
			// 魔数之后 0x1000 处是 .text 起点
			addr = c.Addr + 0x1000
		}
		buf, err := p.Read(addr, dumpSize)
		if err != nil {
			printf("[jit] %s 读取失败（0x%X）：%v\n", c.Tag, addr, err)
			continue
		}
		name := fmt.Sprintf("jitdump-%s-%s-%s.bin", stamp, c.Tag, fmt.Sprintf("%X", addr))
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, buf, 0o644); err != nil {
			printf("[jit] 写文件失败 %s：%v\n", path, err)
			continue
		}
		c.Path = path
		c.Addr = addr
		printf("[jit] %-12s addr=0x%X  gva=0x%X  → %s\n", c.Tag, addr, c.GuestVA, path)
	}

	// ── 附带：把候选的段头信息（若是 NSO 头）解出来 ──
	hr("NSO 段头解析（若上面命中 NSO0）")
	for _, c := range cands {
		if !strings.HasPrefix(c.Tag, "magic_") {
			continue
		}
		hdr, err := p.Read(c.Addr, 0x100)
		if err != nil {
			continue
		}
		magic := string(hdr[0:4])
		ver := binary.LittleEndian.Uint32(hdr[4:8])
		// NSO 段表：0x10 起三组 (offset,size)
		segs := [][2]uint32{}
		for i := 0; i < 3; i++ {
			off := 0x10 + i*8
			if off+8 > len(hdr) {
				break
			}
			segs = append(segs, [2]uint32{
				binary.LittleEndian.Uint32(hdr[off : off+4]),
				binary.LittleEndian.Uint32(hdr[off+4 : off+8]),
			})
		}
		printf("[jit] %s  ver=0x%X  text=(off 0x%X size 0x%X) rodata=(off 0x%X size 0x%X) data=(off 0x%X size 0x%X)\n",
			magic, ver,
			segs[0][0], segs[0][1], segs[1][0], segs[1][1], segs[2][0], segs[2][1])
	}

	hr("结论素材")
	for _, c := range cands {
		if c.Path == "" {
			continue
		}
		printf("  %-14s %s  %s\n", c.Tag, c.Why, c.Path)
	}
	sayln("\n[jit] 下一步：跑 tools/jit_analyze.py 做 AArch64 / x86-64 双口径反汇编质量评估。")
}

// findMagic 在模块区 + DRAM 里找 4 字节魔数。
func findMagic(p *winmem.Process, d winmem.Dram, magic string) []uintptr {
	var out []uintptr
	seen := map[uintptr]bool{}
	scanRange := func(lo, hi uintptr) {
		const chunk = 8 << 20
		for off := lo; off < hi; off += chunk {
			n := chunk
			if remain := int(hi - off); n > remain {
				n = remain
			}
			buf, err := p.Read(off, n)
			if err != nil {
				continue
			}
			for _, a := range scan.FindASCII(buf, off, magic) {
				if !seen[a] {
					seen[a] = true
					out = append(out, a)
				}
			}
		}
	}
	scanRange(d.ArenaLow, d.HeapLo())
	scanRange(d.BlockBase, d.BlockBase+d.BlockSize)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
