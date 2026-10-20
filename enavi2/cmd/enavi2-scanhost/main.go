// enavi2-scanhost：扫描宿主对象区域，找"每帧持续更新"的实时位置字段。
//
// 背景：0x291DE86EF40 的 +0x28/+0x50 是历史采样缓冲（60-90s 循环推进），
// 不是实时位置。玩家 Actor 对象内应有每帧更新的主位置字段。
// 本工具读 base±range 字节 × frames 帧（10Hz），离线找：
//   - float32 三元组（4 字节对齐）
//   - 每帧都变化（玩家移动时）
//   - 相邻帧位移 < maxStep（平滑移动，非轮转跳变）
//   - 坐标在合法世界范围
// 输出候选字段偏移。
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"time"

	"botwmap/enavi2/internal/winmem"
)

var (
	baseFlag  = flag.Uint64("base", 0x291DE86EF40, "宿主 base")
	rangeFlag = flag.Int("range", 0x800, "扫描范围字节（±0x800 内）")
	frames    = flag.Int("frames", 24, "采样帧数")
	hzFlag    = flag.Int("hz", 2, "采样频率（Hz=2 → 12s 窗口）")
	maxStep   = flag.Float64("maxstep", 12, "相邻帧最大位移 m（平滑移动上限）")
	pidFlag   = flag.Int("pid", 0, "PID（默认自动找 eden）")
)

func f32(b []byte, off int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b[off:]))
}

func validWorld(x, alt, z float32) bool {
	xf, af, zf := float64(x), float64(alt), float64(z)
	if math.IsNaN(xf) || math.IsInf(xf, 0) || math.IsNaN(af) || math.IsInf(af, 0) || math.IsNaN(zf) || math.IsInf(zf, 0) {
		return false
	}
	if math.Abs(xf) > 8000 || math.Abs(zf) > 8000 {
		return false
	}
	if af < -600 || af > 6000 {
		return false
	}
	return true
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags)

	var pid uint32
	if *pidFlag > 0 {
		pid = uint32(*pidFlag)
	} else {
		pids := winmem.FindPIDs("eden")
		if len(pids) == 0 {
			log.Fatalf("没找到 eden.exe")
		}
		pid = pids[0]
	}
	p, err := winmem.Open(pid)
	if err != nil {
		log.Fatalf("Open: %v", err)
	}
	defer p.Close()

	base := uintptr(*baseFlag)
	n := *rangeFlag
	log.Printf("[scan] base=0x%X ±0x%X bytes, %d 帧 @ %dHz (请玩家在扫描期间持续移动)", base, n, *frames, *hzFlag)

	// 采集 frames 帧
	all := make([][]byte, 0, *frames)
	interval := time.Second / time.Duration(*hzFlag)
	for i := 0; i < *frames; i++ {
		buf, err := p.ReadTimeout(base, n, 800*time.Millisecond)
		if err != nil || len(buf) < n {
			log.Fatalf("读 0x%X 失败: %v", base, err)
		}
		all = append(all, buf)
		time.Sleep(interval)
	}

	// 找 4 字节对齐的三元组候选
	found := 0
	for off := 0; off+12 <= n; off += 4 {
		// 首帧校验合法性
		x0, a0, z0 := f32(all[0], off), f32(all[0], off+4), f32(all[0], off+8)
		if !validWorld(x0, a0, z0) {
			continue
		}
		// 每帧都变化（玩家持续移动）且平滑
		ok := true
		prevX, prevZ := x0, z0
		total := 0.0
		for i := 1; i < *frames; i++ {
			x, a, z := f32(all[i], off), f32(all[i], off+4), f32(all[i], off+8)
			if !validWorld(x, a, z) {
				ok = false
				break
			}
			dx, dz := float64(x-prevX), float64(z-prevZ)
			d := math.Hypot(dx, dz)
			total += d
			if d > *maxStep {
				ok = false
				break
			}
			if x == prevX && z == prevZ {
				ok = false // 必须每帧移动（玩家持续移动时）
				break
			}
			prevX, prevZ = x, z
		}
		if !ok {
			continue
		}
		found++
		fmt.Printf("[hit] +0x%04X 起始帧 (%.2f, %.2f, %.2f) 总位移 %.2fm\n",
			off, x0, a0, z0, total)
	}
	fmt.Printf("[done] 候选 %d 个（若为 0：玩家未持续移动/该对象无实时字段）\n", found)
}
