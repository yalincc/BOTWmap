package main

import (
	"fmt"
	"unsafe"
)

func runProbe71(h uintptr) {
	// 1) 基址区域可读性探测（含 JIT/代码段候选 0x036C...）
	bases := []uintptr{
		0x7100000000, 0x7100004000, 0x7100008000, 0x7101000000,
		0x7102000000, 0x71026521F0, 0x7101DA3EC8, 0x7100D5D144, 0x71011DBAE4,
		0x036C4F9C0000, 0x036C4F9C4000, 0x036BE9721000, 0x036C60F40000,
		0x018D03DC5000, 0x018C78F9B000, 0x018C98F27000, 0x016A6B203513,
	}
	for _, b := range bases {
		buf := readMem(h, b, 32)
		if buf == nil {
			fmt.Printf("0x%012X: UNREADABLE\n", b)
			continue
		}
		hex := ""
		ascii := ""
		for i := 0; i < 16; i++ {
			hex += fmt.Sprintf("%02X ", buf[i])
			if buf[i] >= 32 && buf[i] <= 126 {
				ascii += string(buf[i])
			} else {
				ascii += "."
			}
		}
		fmt.Printf("0x%012X: %s |%s|\n", b, hex, ascii)
	}

	// 2) 只读大块（vtable 表候选）：读若干 8B，看是否指向 JIT 代码段
	fmt.Println("\n-- rodata/vtable-table candidates: 8B ptr targets --")
	tabs := []uintptr{0x018D03DC5000, 0x018C78F9B000, 0x018C98F27000, 0x016A6B203000}
	for _, t := range tabs {
		buf := readMem(h, t, 256)
		if buf == nil {
			fmt.Printf("0x%012X: UNREADABLE\n", t)
			continue
		}
		inJIT, inTab, inGuest := 0, 0, 0
		for i := 0; i+8 <= len(buf); i += 8 {
			v := *(*uint64)(unsafe.Pointer(&buf[i]))
			if v >= 0x036B00000000 && v < 0x037000000000 {
				inJIT++
			}
			if v >= 0x018C00000000 && v < 0x018E00000000 {
				inTab++
			}
			if v >= 0x00000100000000 && v < 0x00001000000000 {
				inGuest++
			}
		}
		fmt.Printf("0x%012X: ptrs->JIT=%d  ptrs->rodata=%d  ptrs->guestlow=%d\n", t, inJIT, inTab, inGuest)
	}
}
