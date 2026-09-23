// refscan.go：M3' 逆向第四步——反查"谁指向名字字符串"。
// 全内存扫描所有 8B 值，命中名字字符串地址集合 → 找到持有名字指针的 actor 对象字段。
// 然后观察这些引用地址的分布与聚簇（对象基址线索）。
// 用法：live-npcs.exe --refscan [--ref-min=N]（最少命中次数，默认 1）

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

func runRefScan(h uintptr) {
	minHits := 1
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--ref-min=") {
			if n, err := strconv.Atoi(a[len("--ref-min="):]); err == nil {
				minHits = n
			}
		}
	}
	blocks := guestBlocks(h, minBlockMB)

	// 1) 名字集合 + 名字文本（Npc_/Enemy_/Animal_/GameROM）
	fmt.Printf("scanning name strings...\n")
	hits := scanActorStrings(h, blocks)
	nameSet := map[uintptr]bool{}
	nameText := map[uintptr]string{}
	for _, hh := range hits {
		if strings.HasPrefix(hh.Name, "Npc_") || strings.HasPrefix(hh.Name, "Enemy_") ||
			strings.HasPrefix(hh.Name, "Animal_") || strings.HasPrefix(hh.Name, "GameROM") {
			nameSet[hh.Addr] = true
			nameText[hh.Addr] = hh.Name
		}
	}
	fmt.Printf("name targets: %d\n", len(nameSet))

	// 2) 全内存扫描 8B 值命中名字集合
	type ref struct {
		at   uintptr
		name uintptr
	}
	var refs []ref
	for _, b := range blocks {
		off := uintptr(0)
		for off < b.Size {
			n := uintptr(8 << 20)
			if b.Size-off < n {
				n = b.Size - off
			}
			buf := readMem(h, b.Base+off, int(n))
			if buf != nil {
				for i := 0; i+8 <= len(buf); i += 8 {
					v := *(*uint64)(unsafe.Pointer(&buf[i]))
					if nameSet[uintptr(v)] {
						refs = append(refs, ref{b.Base + off + uintptr(i), uintptr(v)})
					}
				}
			}
			off += n
		}
	}
	fmt.Printf("refs to name strings: %d\n", len(refs))

	// 3) 按名字聚簇展示
	type nmInfo struct {
		name string
		refs []uintptr
	}
	byName := map[uintptr]*nmInfo{}
	for _, r := range refs {
		if byName[r.name] == nil {
			byName[r.name] = &nmInfo{name: nameText[r.name]}
		}
		byName[r.name].refs = append(byName[r.name].refs, r.at)
	}
	fmt.Println("\n-- name strings and their refs (top 40 by ref count) --")
	type nmCount struct {
		nm   *nmInfo
		cnt  int
	}
	var sorted []nmCount
	for _, info := range byName {
		sorted = append(sorted, nmCount{info, len(info.refs)})
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].cnt > sorted[i].cnt {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	shown := 0
	for _, s := range sorted {
		if s.cnt < minHits {
			continue
		}
		if shown >= 40 {
			break
		}
		fmt.Printf("  %-32s refs=%d  first @0x%012X\n", s.nm.name, s.cnt, s.nm.refs[0])
		shown++
	}

	// 4) 引用地址聚簇分析（对象密集区）
	fmt.Println("\n-- ref address clustering (0x10000 buckets, count>=2) --")
	buckets := map[uintptr]int{}
	for _, r := range refs {
		buckets[r.at&^0xFFFF]++
	}
	type bk struct {
		base  uintptr
		count int
	}
	var bks []bk
	for b, c := range buckets {
		bks = append(bks, bk{b, c})
	}
	for i := 0; i < len(bks); i++ {
		for j := i + 1; j < len(bks); j++ {
			if bks[j].base < bks[i].base {
				bks[i], bks[j] = bks[j], bks[i]
			}
		}
	}
	for _, b := range bks {
		if b.count >= 2 {
			fmt.Printf("  0x%012X  refs=%d\n", b.base, b.count)
		}
	}
}
