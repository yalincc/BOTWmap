// enavi2-valuescan：在 Eden User Heap 里搜索指定世界坐标的 float32 三元组。
//
// 用途：玩家静止在已知位置（如存档点）时，玩家位置历史缓冲里存着该坐标；
// 指纹扫描漏判时，用「值扫描」直接找坐标字节模式，命中地址即玩家相关结构。
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"botwmap/enavi2/internal/winmem"
)

var (
	pidFlag = flag.Uint("pid", 0, "Eden pid（0=自动找）")
	tx      = flag.Float64("x", 0, "目标 X")
	talt    = flag.Float64("alt", 0, "目标 alt(Y)")
	tz      = flag.Float64("z", 0, "目标 Z")
	tol     = flag.Float64("tol", 0.5, "每轴容差（米）")
	limit   = flag.Int("limit", 200, "最大命中数")
	workers = flag.Int("workers", 6, "并发数")
	delay   = flag.Duration("delay", 8*time.Millisecond, "每次读后休眠（保护模拟器）")
)

func main() {
	flag.Parse()
	if *tx == 0 && *talt == 0 && *tz == 0 {
		fmt.Println("[usage] -x -alt -z 必填（目标坐标）；-tol 容差默认 0.5m")
		os.Exit(1)
	}

	var pid uint32
	if *pidFlag > 0 {
		pid = uint32(*pidFlag)
	} else {
		pids := winmem.FindPIDs("eden")
		if len(pids) == 0 {
			fmt.Println("[attach] 没找到 eden.exe")
			os.Exit(1)
		}
		pid = pids[0]
	}
	p, err := winmem.Open(pid)
	if err != nil {
		fmt.Println("[attach] Open 失败：", err)
		os.Exit(1)
	}
	defer p.Close()
	fmt.Printf("[attach] pid=%d %s\n", pid, p.ExePath())

	d, _, err := p.FindDram()
	if err != nil {
		fmt.Println("[dram]", err)
		os.Exit(1)
	}
	fmt.Printf("[dram] arenaLow=0x%X UserHeap guest [0x%X,0x%X) → host [0x%X,0x%X)\n",
		d.ArenaLow, d.HeapLo()-d.ArenaLow, d.HeapHi()-d.ArenaLow, d.HeapLo(), d.HeapHi())

	lo, hi := d.HeapLo(), d.HeapHi()
	var regions []winmem.Region
	p.Regions(lo, hi, func(r winmem.Region) bool {
		if r.RW() && r.Size >= 64<<10 {
			regions = append(regions, r)
		}
		return false
	})
	var total uint64
	for _, r := range regions {
		total += uint64(r.Size)
	}
	fmt.Printf("[heap] User Heap 可读写块 %d 个，合计 %.2f GB\n", len(regions), float64(total)/1073741824.0)

	tx32, talt32, tz32 := float32(*tx), float32(*talt), float32(*tz)

	type hit struct {
		addr uintptr
		x, y, z float32
	}
	var hits []hit
	var mu sync.Mutex

	const chunk = 8 << 20
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += chunk {
			n := chunk
			if remain := int(r.Size - off); n > remain {
				n = remain
			}
			if int(off)+n < int(r.Size) {
				n += 256 // overlap 防三元组跨块
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}

	ch := make(chan job, *workers*2)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				time.Sleep(*delay)
				if err != nil || len(buf) < 12 {
					continue
				}
				var local []hit
				for off := 0; off+12 <= len(buf); off += 4 {
					x := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
					y := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
					z := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
					if math.Abs(float64(x-tx32)) <= *tol && math.Abs(float64(y-talt32)) <= *tol && math.Abs(float64(z-tz32)) <= *tol {
						local = append(local, hit{j.addr + uintptr(off), x, y, z})
					}
				}
				if len(local) == 0 {
					continue
				}
				mu.Lock()
				hits = append(hits, local...)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	fmt.Printf("\n--- 命中 %d 个（坐标 %.2f, %.2f, %.2f ±%.1fm）---\n", len(hits), *tx, *talt, *tz, *tol)
	if len(hits) == 0 {
		fmt.Println("⚠ 零命中：目标坐标不在 User Heap（玩家不在该点/已移动/块不可读）")
		os.Exit(0)
	}
	// 容差标注（float32 比较在遍历中直接做）
	show := len(hits)
	if show > *limit {
		show = *limit
	}
	// 按地址排
	for i := 0; i < len(hits) && i < show; i++ {
		// 读 0x30 看结构
		ctx, err := p.Read(hits[i].addr, 0x30)
		var hexs string
		if err == nil && len(ctx) >= 0x30 {
			var b []byte
			for k := 0; k < 0x30; k++ {
				b = append(b, []byte(fmt.Sprintf("%02X ", ctx[k]))...)
			}
			hexs = string(b)
		}
		fmt.Printf("  #%02d host=0x%X  (%.2f, %.2f, %.2f)  %s\n", i, hits[i].addr, hits[i].x, hits[i].y, hits[i].z, hexs)
	}
	if len(hits) > show {
		fmt.Printf("  ... 其余 %d 个略\n", len(hits)-show)
	}
}
