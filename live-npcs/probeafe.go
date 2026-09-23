// probeafe.go：同一 actor 名的全部 hash 引用点（实例候选）诊断。
//
// 用途：验证"位置跳动"根因——同名 NPC 是否有多个实例候选（真实例 vs 放置记录），
// 以及不同名字是否反查到同一实例（误命中）。用法：live-npcs --probeafe=006,020

package main

import (
	"fmt"
	"math"
	"strings"
	"unsafe"
)

// runProbeAFE 对给定 Npc_HatenoVillageXXX 名，dump 全部 hash 引用点。
func runProbeAFE(h uintptr, args string) {
	names := strings.Split(args, ",")
	if len(names) == 0 || names[0] == "" {
		fmt.Println("usage: --probeafe=Npc_HatenoVillage006,020")
		return
	}
	want := map[string]bool{}
	for _, n := range names {
		if !strings.HasPrefix(n, "Npc_") {
			if len(n) > 0 && n[0] >= '0' && n[0] <= '9' {
				n = "Npc_HatenoVillage" + n // 纯数字 → Hateno 村 NPC
			} else {
				n = "Npc_" + n
			}
		}
		want[n] = true
	}

	blocks := clipBlocks(h, npcRegionLo, npcRegionHi)
	infos := map[string]uint32{} // name → hash
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, n := range scanActorStringsRange(h, b.Base, b.Base+b.Size) {
			if !strings.HasPrefix(n.Name, "Npc_") || strings.ContainsAny(n.Name, ":/") {
				continue
			}
			if seen[n.Name] {
				continue
			}
			seen[n.Name] = true
			buf := readMem(h, n.Addr, 0x60)
			if buf == nil {
				continue
			}
			hv := *(*uint32)(unsafe.Pointer(&buf[0x4C]))
			if hv < 0x34400000 || hv >= 0x34500000 {
				continue
			}
			infos[n.Name] = hv
		}
	}

	for name := range want {
		hv, ok := infos[name]
		fmt.Printf("\n== %s hash=0x%08X ==\n", name, hv)
		if !ok {
			fmt.Println("  (ActorInfo not collected this scan)")
			continue
		}
		refs := map[uintptr]bool{}
		for _, b := range blocks {
			off := uintptr(0)
			for off < b.Size {
				n := uintptr(4 << 20)
				if b.Size-off < n {
					n = b.Size - off
				}
				buf := readMem(h, b.Base+off, int(n))
				if buf == nil {
					off += n
					continue
				}
				for i := 0; i+4 <= len(buf); i += 4 {
					if *(*uint32)(unsafe.Pointer(&buf[i])) == hv {
						refs[b.Base+off+uintptr(i)] = true
					}
				}
				off += n
			}
		}
		fmt.Printf("  %d hash refs:\n", len(refs))
		for r := range refs {
			buf := readMem(h, r, 0x90)
			if buf == nil {
				fmt.Printf("  0x%012X unreadable\n", r)
				continue
			}
			x := *(*float32)(unsafe.Pointer(&buf[0x80]))
			y := *(*float32)(unsafe.Pointer(&buf[0x84]))
			z := *(*float32)(unsafe.Pointer(&buf[0x88]))
			sane := saneF(x) && saneF(y) && saneF(z) &&
				math.Abs(float64(x)) <= 12000 && math.Abs(float64(y)) <= 12000 && math.Abs(float64(z)) <= 12000
			scale := *(*float32)(unsafe.Pointer(&buf[0x0C]))
			// 矩阵特征：+0x40 起前 3 行是否正交旋转
			m := make([]float32, 9)
			for k := 0; k < 9; k++ {
				m[k] = *(*float32)(unsafe.Pointer(&buf[0x40+k*4]))
			}
			dot := m[0]*m[3] + m[1]*m[4] + m[2]*m[5]
			fmt.Printf("  0x%012X scale=%6.2f xyz=(%9.2f,%9.2f,%9.2f) sane=%v dot12=%.4f\n",
				r, scale, x, y, z, sane, dot)
		}
	}
}
