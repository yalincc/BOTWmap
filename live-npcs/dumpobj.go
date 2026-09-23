// dumpobj.go：M3' 逆向辅助——dump 名字字符串周围的原始结构。
// scanstr 发现名字 Npc_Kakariko001/Darukeru/... 以 ~0xE80 间隔成组重复：
// 可能是"内嵌定长名字的对象记录"。本工具 dump 名字 ±2KB，解析字段/字符串/float。
// 用法：live-npcs.exe --dumpobj=<名字字符串地址hex>

package main

import (
	"fmt"
	"strconv"
	"strings"
)

func runDumpObj(h uintptr, addrArg string) {
	addr, err := strconv.ParseUint(strings.TrimPrefix(addrArg, "0x"), 16, 64)
	if err != nil {
		fmt.Printf("bad addr: %v\n", err)
		return
	}
	fmt.Printf("dumping around 0x%012X (name string)...\n\n", uintptr(addr))
	// 先读名字文本
	nb := readMem(h, uintptr(addr), 96)
	if nb == nil {
		fmt.Println("name unreadable")
		return
	}
	name := ""
	for _, c := range nb {
		if c < 32 || c > 126 {
			break
		}
		name += string(c)
		if len(name) >= 80 {
			break
		}
	}
	fmt.Printf("name: %q\n", name)

	// dump 名字前 0x40（记录头方向）与后 0x100
	lo := uintptr(addr) - 0x40
	if lo > uintptr(addr) {
		lo = 0
	}
	hi := uintptr(addr) + 0x100
	fmt.Printf("range 0x%012X..0x%012X\n\n", lo, hi)

	// 以 16 字节行打印 hex + ascii，并标注可疑 float
	buf := readMem(h, lo, int(hi-lo))
	if buf == nil {
		fmt.Println("range unreadable")
		return
	}
	for off := 0; off < len(buf); off += 16 {
		abs := lo + uintptr(off)
		hexPart := ""
		asciiPart := ""
		for i := 0; i < 16 && off+i < len(buf); i++ {
			b := buf[off+i]
			hexPart += fmt.Sprintf("%02X ", b)
			if b >= 32 && b <= 126 {
				asciiPart += string(b)
			} else {
				asciiPart += "."
			}
		}
		// 找 float 候选（偏移 4 对齐）
		fl := ""
		for i := 0; i+4 <= 16 && off+i+4 <= len(buf); i += 4 {
			f := float32le(buf[off+i : off+i+4])
			if f != 0 && f > -50000 && f < 50000 && (f == float32(int32(f))) && int32(f) != 0 {
				fl += fmt.Sprintf(" f%d=%.0f", i, f)
			}
		}
		marker := ""
		if off >= 0x40 && off < 0x40+len(name) {
			marker = "  <-- name"
		}
		if strings.Contains(asciiPart, "Npc_") || strings.Contains(asciiPart, "Enemy_") || strings.Contains(asciiPart, "Animal_") {
			marker = "  <== STR"
		}
		fmt.Printf("0x%012X  %-48s |%s|%s%s\n", abs, hexPart, asciiPart, fl, marker)
	}
}

func float32le(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	u := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return float32(int32(u))
}
