// memmap.go：M3' 逆向辅助——枚举 guest 进程内存区域（含代码段），
// 用于识别 actor 对象 vtable（指针指向代码段 = 对象头部特征）。
// 用法：live-npcs.exe --memmap

package main

import (
	"fmt"
)

type memRegion struct {
	Base uintptr
	Size uintptr
	Protect uint32
	Type   uint32
	State  uint32
}

func enumRegions(h uintptr) []memRegion {
	var out []memRegion
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, addr, &mbi) == 0 {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit {
			out = append(out, memRegion{base, size, p, mbi.Type, mbi.State})
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

// 指针指向的代码/只读区（protect 包含 execute 或 R）→ 可能是 vtable
func runMemMap(h uintptr) {
	regs := enumRegions(h)
	fmt.Printf("total committed regions: %d\n", len(regs))
	// 聚合：按 protect 类型统计
	type agg struct {
		protect uint32
		count   int
		size    uintptr
		first   uintptr
		last    uintptr
	}
	byProt := map[uint32]*agg{}
	for _, r := range regs {
		a := byProt[r.Protect]
		if a == nil {
			a = &agg{protect: r.Protect, first: r.Base, last: r.Base + r.Size}
			byProt[r.Protect] = a
		}
		a.count++
		a.size += r.Size
		if r.Base < a.first {
			a.first = r.Base
		}
		if r.Base+r.Size > a.last {
			a.last = r.Base + r.Size
		}
	}
	// 按大小排序输出
	for _, a := range byProt {
		fmt.Printf("protect=0x%02X  regions=%d  size=0x%X  range=0x%012X..0x%012X\n",
			a.protect, a.count, a.size, a.first, a.last)
	}
	fmt.Println()
	// 详细列出较大的代码/只读区（protect 5=RX, 1=R 等）
	fmt.Println("-- readable/executable regions (potential vtable targets) --")
	n := 0
	for _, r := range regs {
		p := r.Protect & 0xFF
		if p == 0x01 || p == 0x02 || p == 0x04 || p == 0x05 || p == 0x08 || p == 0x0C || p == 0x20 || p == 0x40 {
			if r.Size >= 0x1000 && n < 80 {
				fmt.Printf("  0x%012X size=0x%X prot=0x%02X type=%d\n", r.Base, r.Size, r.Protect, r.Type)
				n++
			}
		}
	}
	if n == 0 {
		fmt.Println("  (none listed)")
	}

	// 按大小排序输出所有 RO/XR 大区域（主代码段/资源映射候选）
	fmt.Println("\n-- largest RO/XR regions (top 40 by size) --")
	type szr struct {
		r    memRegion
		size uintptr
	}
	var sorted []szr
	for _, r := range regs {
		p := r.Protect & 0xFF
		if p == 0x01 || p == 0x20 || p == 0x10 || p == 0x08 || p == 0x80 {
			sorted = append(sorted, szr{r, r.Size})
		}
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].size > sorted[i].size {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	shown2 := 0
	for _, s := range sorted {
		fmt.Printf("  0x%012X size=0x%X prot=0x%02X type=%d\n", s.r.Base, s.r.Size, s.r.Protect, s.r.Type)
		shown2++
		if shown2 >= 40 {
			break
		}
	}
}
