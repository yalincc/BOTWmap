// 本文件 = 坐标槽的持续跟踪验证。
//
// 用途：确认某个地址是不是「玩家当前坐标」——
//  玩家走动时它会变；玩家不动时它不变；传送时它跳变。
// 这是判定坐标槽的**决定性实验**（比任何结构推断都可靠）。
//
// 用法：
//   enavi-probe -cmd trackcoord -addr 0x162449FC050 -pid N
//   enavi-probe -cmd trackcoord -file report/hunt3f-hits.txt -pid N   （跟踪一批，看哪些在动）
package main

import (
	"bufio"
	"flag"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"botwmap/enavi/internal/winmem"
)

var (
	trackAddr = flag.String("caddr", "", "要跟踪的地址（宿主地址，0x 前缀）；可逗号分隔")
	trackFile = flag.String("file", "", "从文件读一批地址来跟踪")
	trackSecs = flag.Int("secs", 3, "采样间隔（秒）")
)

// cmdTrackCoord 持续跟踪地址上的 3 个 float。
func cmdTrackCoord(p *winmem.Process) {
	hr("坐标槽跟踪验证")

	var addrs []uintptr
	if *trackAddr != "" {
		for _, s := range strings.Split(*trackAddr, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
			if err != nil {
				printf("[track] 地址 %q 解析失败\n", s)
				continue
			}
			addrs = append(addrs, uintptr(v))
		}
	}
	if *trackFile != "" {
		f, err := os.Open(*trackFile)
		if err == nil {
			sc := bufio.NewScanner(f)
			n := 0
			for sc.Scan() && n < 4000 {
				parts := strings.Fields(sc.Text())
				if len(parts) == 0 {
					continue
				}
				v, err := strconv.ParseUint(strings.TrimPrefix(parts[0], "0x"), 16, 64)
				if err != nil {
					continue
				}
				addrs = append(addrs, uintptr(v))
				n++
			}
			f.Close()
			printf("[track] 从文件读到 %d 个地址\n", n)
		}
	}
	if len(addrs) == 0 {
		printf("[track] 请给 -addr 或 -file\n")
		return
	}
	printf("[track] 跟踪 %d 个地址，每 %d 秒采样一次\n", len(addrs), *trackSecs)

	type sample struct {
		v [3]float32
		ok bool
	}
	prev := make([]sample, len(addrs))
	first := make([]sample, len(addrs))
	got := make([]bool, len(addrs))

	outPath := filepath.Join("report", fmt.Sprintf("trackcoord-%s.txt", time.Now().Format("20060102-150405")))
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
	emit("跟踪文件 → %s", outPath)

	for round := 0; ; round++ {
		changed := 0
		for i, a := range addrs {
			buf, err := p.ReadTimeout(a, 12, 500*time.Millisecond)
			if err != nil || len(buf) < 12 {
				continue
			}
			var v [3]float32
			for k := 0; k < 3; k++ {
				v[k] = math.Float32frombits(binary.LittleEndian.Uint32(buf[k*4:]))
			}
			if !got[i] {
				first[i] = sample{v, true}
				got[i] = true
			}
			if prev[i].ok {
				d := math.Hypot(float64(v[0]-prev[i].v[0]), float64(v[2]-prev[i].v[2]))
				if d > 0.05 {
					changed++
					emit("  [变动] 0x%X  (%.2f, %.2f, %.2f)  位移 %.2f m",
						a, v[0], v[1], v[2], d)
				}
			}
			prev[i] = sample{v, true}
		}
		emit("第 %d 轮：%d 个地址中有 %d 个发生变化", round+1, len(addrs), changed)
		time.Sleep(time.Duration(*trackSecs) * time.Second)
	}
}
