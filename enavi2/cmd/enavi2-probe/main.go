// Enavi2-probe —— M1 探针入口（特征码唯一性验证）。
//
// 本文件为 enavi2 独立实现；辅助逻辑（runChunks 限速、findBytesAll 等）
// 参考自 enavi/cmd/enavi-probe（复制后改造，enavi 原目录未动）。
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"botwmap/enavi2/internal/winmem"
)

var (
	cmd       = flag.String("cmd", "", "命令：m1")
	nsoFlag   = flag.String("nso", "", "NSO 文件路径（解压后，如 main_uncompressed.nso）")
	bytesFlag = flag.String("bytes", "", "hex 字节特征码（与 -nso 二选一）")
	pidFlag   = flag.Int("pid", 0, "目标进程 PID（默认自动找 eden.exe）")
	workers   = flag.Int("w", 2, "扫描并发数（探针限速，默认 2，别开大）")
	chunkMB   = flag.Int("chunkmb", 8, "每块读取大小（MB）")
	scanDelay = flag.Duration("delay", 8*time.Millisecond, "每块读取之间的休眠（防拖死 Eden）")

	out = os.Stdout
)

func printf(format string, a ...any) { fmt.Fprintf(out, format, a...) }

func sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }

func hr(t string) { printf("\n===== %s =====\n", t) }

func gb(n uint64) string { return fmt.Sprintf("%.2fGB", float64(n)/1073741824.0) }

func main() {
	flag.Parse()
	switch *cmd {
	case "m1":
		cmdM1()
	case "aob":
		cmdAOB()
	case "m1p":
		cmdM1P()
	case "m2p":
		cmdM2P()
	default:
		hr("用法")
		printf("  enavi2-probe -cmd m1   -nso <main_uncompressed.nso>  # 字符串探针（已证伪，对照用）\n")
		printf("  enavi2-probe -cmd aob  -nso <main_uncompressed.nso>  # 指令字节 AOB（已证伪，对照用）\n")
		printf("  enavi2-probe -cmd m1p [-anchor x,z] [-dumpaddr a,b]  # ★ M1' User Heap 指纹验证\n")
		printf("  enavi2-probe -cmd m2p -addr 0x..                     # ★ M2' known 地址 10Hz 连续采样\n")
		printf("  通用：[-pid N] [-w 并发] [-delay 块间休眠] 默认自动找 eden.exe\n")
	}
}

// findBytesAll 在 buf 里找 needle 的全部出现位置，返回绝对地址。
func findBytesAll(buf []byte, base uintptr, needle []byte) []uintptr {
	var out []uintptr
	if len(needle) == 0 {
		return out
	}
	from := 0
	for {
		j := indexBytes(buf, needle, from)
		if j < 0 {
			break
		}
		out = append(out, base+uintptr(j))
		from = j + 1
	}
	return out
}

// indexBytes 在 buf 里找 needle（朴素实现，够用）。
func indexBytes(buf, needle []byte, from int) int {
	if len(needle) == 0 || from < 0 || from+len(needle) > len(buf) {
		return -1
	}
	n := len(needle)
	first := needle[0]
	last := len(buf) - n
	for i := from; i <= last; i++ {
		if buf[i] != first {
			continue
		}
		ok := true
		for j := 1; j < n; j++ {
			if buf[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// runChunks 把区域切成定长块，用 worker 池并发读取并回调。
// ★ 限速：全速读会跟模拟器抢内存带宽，实测会把 Eden 拖到卡加载画面；
// 每块之间强制 sleep、并发压到很低 —— 探针是离线工具，慢一点没关系。
func runChunks(p *winmem.Process, regions []winmem.Region, workers int, fn func(addr uintptr, buf []byte)) {
	if workers < 1 {
		workers = 1
	}
	const overlap = 256
	chunk := *chunkMB << 20
	type job struct {
		addr uintptr
		n    int
	}
	var jobs []job
	for _, r := range regions {
		if !r.Readable() || r.Size == 0 {
			continue
		}
		for off := uintptr(0); off < r.Size; off += uintptr(chunk) {
			n := chunk
			if remain := int(r.Size - off); remain < n {
				n = remain
			}
			if uintptr(n) < r.Size-off {
				n += overlap
			}
			jobs = append(jobs, job{r.Base + off, n})
		}
	}
	if len(jobs) == 0 {
		return
	}
	ch := make(chan job, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				buf, err := p.Read(j.addr, j.n)
				time.Sleep(*scanDelay)
				if err != nil || len(buf) < 12 {
					continue
				}
				fn(j.addr, buf)
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}
