// nameregions.go：M3' 逆向第八步——统计名字字符串的内存区域分布。
// 若 actor 名字内嵌在运行时对象里（char[0x40]），它们应出现在堆区（非 EventFlow 资源区）。
// 用法：live-npcs.exe --nameregions

package main

import (
	"fmt"
	"strings"
)

func runNameRegions(h uintptr) {
	blocks := guestBlocks(h, minBlockMB)
	fmt.Printf("scanning name strings (Npc_/Enemy_/Animal_ prefix)...\n")
	hits := scanActorStrings(h, blocks)
	fmt.Printf("total: %d\n", len(hits))

	// 按 0x100000000 (4GB) 粒度分区
	type region struct {
		count  int
		names  map[string]int
		first  uintptr
		last   uintptr
	}
	regs := map[uintptr]*region{}
	for _, hh := range hits {
		if !(strings.HasPrefix(hh.Name, "Npc_") || strings.HasPrefix(hh.Name, "Enemy_") || strings.HasPrefix(hh.Name, "Animal_")) {
			continue
		}
		bucket := hh.Addr &^ 0xFFFFFFFF
		r := regs[bucket]
		if r == nil {
			r = &region{names: map[string]int{}, first: hh.Addr, last: hh.Addr}
			regs[bucket] = r
		}
		r.count++
		if r.names[hh.Name] == 0 {
			r.names[hh.Name] = 1
		} else {
			r.names[hh.Name]++
		}
		if hh.Addr < r.first {
			r.first = hh.Addr
		}
		if hh.Addr > r.last {
			r.last = hh.Addr
		}
	}
	// 输出分区（按 count 降序）
	type rk struct {
		b   uintptr
		r   *region
	}
	var list []rk
	for b, r := range regs {
		list = append(list, rk{b, r})
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].r.count > list[i].r.count {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	fmt.Println("\n-- name string regions (4GB buckets, Npc_/Enemy_/Animal_) --")
	for _, k := range list {
		r := k.r
		// 分区内的不同名字数 vs 重复次数 → 判断是资源表（大量重复）还是对象（每个名字出现1~2次）
		fmt.Printf("  0x%012X  count=%-6d uniq=%-4d first=0x%012X last=0x%012X\n",
			k.b, r.count, len(r.names), r.first, r.last)
		// 输出几个代表名字
		i := 0
		for name, c := range r.names {
			if c == 1 && i < 6 { // 只出现 1 次的名字 = 可能是对象内嵌
				fmt.Printf("      uniq1: %s\n", name)
				i++
			}
		}
	}
}
