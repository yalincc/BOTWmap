// 本文件 = 位置历史缓冲的「逐格」跟测（确定哪一格是最新帧）。
//
// 目的：位置历史缓冲是 0x28 步长的元素数组，实测 elem0 空、elem1/elem2 带朝向四元数。
// 需要确定：
//   - 玩家移动时，是固定某一格在变，还是在格之间轮转？
//   - 哪一格离「当前时刻」最近？
//
// 用法：enavi-probe -cmd trackcells -base 0x162449FC000 -cells 6 -secs 2
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"botwmap/enavi/internal/winmem"
)

var (
	trackBase  = flag.String("tbase", "", "缓冲基址（宿主地址）")
	trackCells = flag.Int("cells", 6, "跟踪几格")
)

func cmdTrackCells(p *winmem.Process) {
	hr("位置历史缓冲：逐格跟测")

	base, err := strconv.ParseUint(trimHex(*trackBase), 16, 64)
	if err != nil {
		printf("[cells] -tbase 解析失败\n")
		return
	}
	b := uintptr(base)
	n := *trackCells
	size := n * 0x28

	outPath := filepath.Join("report", fmt.Sprintf("trackcells-%s.txt", time.Now().Format("20060102-150405")))
	of, _ := os.Create(outPath)
	if of != nil {
		defer of.Close()
	}
	emit := func(f string, a ...any) {
		line := fmt.Sprintf(f, a...)
		fmt.Println(line)
		if of != nil {
			of.WriteString(line + "\n")
			of.Sync()
		}
	}
	emit("跟踪 0x%X 起 %d 格 → %s", b, n, outPath)
	emit("%-8s %s", "时刻", "每格 (X,alt,Z) [带* = 有朝向四元数]")

	prev := make([][3]float32, n)
	for round := 0; ; round++ {
		buf, err := p.ReadTimeout(b, size, 800*time.Millisecond)
		if err != nil || len(buf) < size {
			emit("读取失败：%v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var parts []string
		for k := 0; k < n; k++ {
			o := k * 0x28
			p0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[o:]))
			p1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+4:]))
			p2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+8:]))
			w := math.Float32frombits(binary.LittleEndian.Uint32(buf[o+0x0C:]))
			mark := " "
			if w > 0.90 && w <= 1.0 {
				mark = "*"
			}
			delta := ""
			if k < n && prev[k] != [3]float32{} {
				d := math.Hypot(float64(p0-prev[k][0]), float64(p2-prev[k][2]))
				if d > 0.05 {
					delta = fmt.Sprintf("(Δ%.1f)", d)
				}
			}
			parts = append(parts, fmt.Sprintf("e%d%s(%.1f,%.1f,%.1f)%s", k, mark, p0, p1, p2, delta))
			prev[k] = [3]float32{p0, p1, p2}
		}
		emit("%-8s %s", time.Now().Format("15:04:05"), join(parts, "  "))
		time.Sleep(2 * time.Second)
	}
}

func trimHex(s string) string {
	if len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X") {
		return s[2:]
	}
	return s
}

func join(a []string, sep string) string {
	out := ""
	for i, s := range a {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
