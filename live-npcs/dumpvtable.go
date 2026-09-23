package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

func runDumpVtable(h uintptr, addrArg string) {
	addr, err := strconv.ParseUint(strings.TrimPrefix(addrArg, "0x"), 16, 64)
	if err != nil {
		fmt.Printf("bad addr: %v\n", err)
		return
	}
	length := 0x4000
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--len=") {
			if n, err := strconv.ParseUint(a[len("--len="):], 0, 64); err == nil {
				length = int(n)
			}
		}
	}
	buf := readMem(h, uintptr(addr), length)
	if buf == nil {
		fmt.Printf("unreadable 0x%012X..0x%012X\n", uintptr(addr), uintptr(addr)+uintptr(length))
		return
	}
	fmt.Printf("dumping vtable object @0x%012X len=0x%X\n\n", addr, length)

	// 1) 结构概览：8B 字段分类（前 0x200）
	fmt.Println("-- 8B fields overview (first 0x200) --")
	for off := 0; off+8 <= 0x200 && off+8 <= len(buf); off += 8 {
		v := *(*uint64)(unsafe.Pointer(&buf[off]))
		desc := ""
		if v >= 0x7100000000 && v < 0x7110000000 {
			desc = "->CODE"
		} else if v >= 0x010000000000 && v < 0x020000000000 {
			desc = "->GUEST"
		} else if v > 0x10000 && v < 0x100000000 {
			desc = fmt.Sprintf("ptr/int %d", v)
		}
		f := float32le(buf[off : off+4])
		f2 := float32le(buf[off+4 : off+8])
		fs := ""
		if f != 0 && f > -20000 && f < 20000 && f == float32(int32(f)) {
			fs += fmt.Sprintf(" f=%.1f", f)
		}
		if f2 != 0 && f2 > -20000 && f2 < 20000 && f2 == float32(int32(f2)) {
			fs += fmt.Sprintf(" f2=%.1f", f2)
		}
		fmt.Printf("  +0x%04X 0x%016X %s%s\n", off, v, desc, fs)
	}

	// 2) ASCII 字符串
	fmt.Println("\n-- ascii strings --")
	for off := 0; off < len(buf)-4; off++ {
		if buf[off] >= 32 && buf[off] <= 126 {
			end := off
			for end < len(buf) && buf[end] >= 32 && buf[end] <= 126 {
				end++
			}
			if end-off >= 4 {
				fmt.Printf("  +0x%04X  %q\n", off, string(buf[off:end]))
				off = end - 1
			}
		}
	}

	// 3) 有意义的 float
	fmt.Println("\n-- floats --")
	for off := 0; off+4 <= len(buf); off += 4 {
		f := float32le(buf[off : off+4])
		if f != 0 && f > -20000 && f < 20000 && f == float32(int32(f)) && int32(f) != 0 {
			fmt.Printf("  +0x%04X  %.1f\n", off, f)
		}
	}
}
