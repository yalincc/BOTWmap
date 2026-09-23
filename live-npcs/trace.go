// trace.go：M3' 指针链回溯——从玩家槽出发，探查 actor 对象布局。
// 目标：找到 actor 对象基址 → 名字字段 → ActorCreator 链表 → 遍历所有 actor。
// 用法：live-npcs.exe --trace [--trace-window=N]（N=玩家槽前后探查字节数，默认 0x80000）
// 本文件是逆向调试工具，探查完成后并入 --reverse 模式。

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

// tracePlayer 从玩家槽地址出发探查 actor 对象：
//  1. 玩家槽（坐标 float 位置）前后扫描，找 ASCII 名字字符串（Player/Npc_/Enemy_ 等）
//  2. 扫描 8 字节指针，找"指向玩家槽附近区域"的内部指针（可能是对象引用/链表）
func tracePlayer(h uintptr, playerAddr uintptr, half int) {
	fmt.Printf("\n=== trace: player slot addr=0x%012X, window ±0x%X ===\n", playerAddr, half)
	lo := playerAddr - uintptr(half)
	hi := playerAddr + uintptr(half)

	// 1) 扫描字符串（按 64KB 块读，ASCII 可打印序列 >=4）
	fmt.Println("\n-- strings (len>=4, printable ASCII) near player slot --")
	found := map[uintptr]string{}
	addr := lo
	for addr < hi {
		size := 64 << 10
		if hi-addr < uintptr(size) {
			size = int(hi - addr)
		}
		buf := readMem(h, addr, size)
		if buf != nil {
			for i := 0; i < len(buf)-3; i++ {
				c := buf[i]
				if c < 32 || c > 126 {
					continue
				}
				j := i
				for j < len(buf) && buf[j] >= 32 && buf[j] <= 126 {
					j++
				}
				if j-i >= 4 {
					s := string(buf[i:j])
					// 只关注特征字符串
					if strings.Contains(s, "Player") || strings.Contains(s, "Npc") ||
						strings.Contains(s, "Enemy") || strings.Contains(s, "Animal") ||
						strings.Contains(s, "Actor") || strings.Contains(s, "GameROM") {
						sa := addr + uintptr(i)
						found[sa] = s
						fmt.Printf("  0x%012X: %q\n", sa, s)
					}
				}
				i = j
			}
		}
		addr += uintptr(size)
	}
	if len(found) == 0 {
		fmt.Println("  (no feature strings found in window)")
	}

	// 2) 扫描 8 字节对齐指针：指向 [lo, hi) 内部的值（对象内引用/链表线索）
	fmt.Println("\n-- internal pointers (8B values pointing within window) --")
	type ptrHit struct {
		at, to uintptr
	}
	var ptrs []ptrHit
	addr = lo
	for addr < hi {
		size := 64 << 10
		if hi-addr < uintptr(size) {
			size = int(hi - addr)
		}
		buf := readMem(h, addr, size)
		if buf != nil {
			for i := 0; i+8 <= len(buf); i += 8 {
				v := *(*uint64)(unsafe.Pointer(&buf[i]))
				if v >= uint64(lo) && v < uint64(hi) && v%8 == 0 {
					ptrs = append(ptrs, ptrHit{addr + uintptr(i), uintptr(v)})
				}
			}
		}
		addr += uintptr(size)
	}
	// 输出：按"指向地址"聚簇，展示引用关系
	seen := map[uintptr]bool{}
	cnt := 0
	for _, p := range ptrs {
		if seen[p.to] {
			continue
		}
		seen[p.to] = true
		if p.to >= lo && p.to < hi {
			// 打印引用它的前几个地址
			var refs []uintptr
			for _, q := range ptrs {
				if q.to == p.to {
					refs = append(refs, q.at)
				}
			}
			fmt.Printf("  target 0x%012X <- refs: %v\n", p.to, refs[:minInt(len(refs), 6)])
			cnt++
			if cnt >= 40 {
				fmt.Println("  ... (truncated)")
				break
			}
		}
	}
	if cnt == 0 {
		fmt.Println("  (no internal pointers)")
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// runTrace 定位玩家槽并探查。
func runTrace(h uintptr) {
	half := 0x80000
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--trace-window=") {
			if n, err := strconv.ParseUint(a[len("--trace-window="):], 0, 32); err == nil {
				half = int(n)
			}
		}
	}

	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		fmt.Println("no save anchor")
		return
	}
	// 窗口扫描找玩家组
	groups, _, secs := findEntitiesNear(h, [][3]float32{anchors[0].Pos}, 400)
	player := findPlayerGroup(groups, anchors)
	if player == nil {
		fmt.Println("player group not found")
		return
	}
	fmt.Printf("player group: pos=(%.1f, %.1f, %.1f) RepAddr=0x%012X struct=%d copies=%d (scan %.1fs)\n",
		player.X, player.Y, player.Z, player.RepAddr, player.Struct, player.Copies, secs)
	tracePlayer(h, player.RepAddr, half)
}
