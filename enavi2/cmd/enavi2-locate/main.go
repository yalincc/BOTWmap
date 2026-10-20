// enavi2-locate：Eden (BOTW 1.6.0) 玩家坐标定位服务（M3 里程碑）。
//
// 路线：known 宿主地址秒锁（用户终极形态已接受"known 缓存秒锁"）。
//   known base = 玩家 Actor 宿主（0x291DE86EF40 实测）。
//   +0x28/+0x50 是历史采样缓冲（60-90s 循环推进，不是实时位置）；
//   +0x0FD0/+0x0B28 是每帧实时更新的主位置字段（scanhost 12s×24帧实测，
//   玩家跑动 59m 全程跟随，总位移一致）。
//
// 定位算法：
//   1) 每轮读双实时字段（+0x0FD0/+0x0B28），各自做世界坐标合法校验；
//   2) 有效字段取「距上一轮输出更近」者输出（玩家连续移动，新位置必离上轮近）；
//   3) 首帧无参考时取第一个有效字段。
//
// 输出：HTTP GET /pos → {ok, verified, source:"player", gx,gy,gz, mx,my, age}
//   mx/my 暂为游戏坐标副本（BOTWmap 地图坐标换算后续接入）。
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"botwmap/enavi2/internal/winmem"
)

var (
	addrFlag   = flag.Uint64("addr", 0x291DE86EF40, "玩家宿主地址（known；默认实测值）")
	portFlag   = flag.Int("port", 8766, "HTTP 端口")
	hzFlag     = flag.Int("hz", 10, "采样频率 Hz（默认 10）")
	elem1Off   = flag.Int("elem1", 0x0FD0, "实时位置字段1 相对 base 偏移（scanhost 实测：每帧更新主位置）")
	elem2Off   = flag.Int("elem2", 0x0B28, "实时位置字段2 相对 base 偏移（scanhost 实测：同步移动副本）")
	readSleep  = flag.Duration("readsleep", 8*time.Millisecond, "单次读后休眠（限速，防卡模拟器）")
	pidFlag    = flag.Int("pid", 0, "目标进程 PID（默认自动找 eden.exe）")
)

// Pos 一次采样输出。
type Pos struct {
	OK       bool      `json:"ok"`
	Verified bool      `json:"verified"`
	Source   string    `json:"source"`
	GX       float32   `json:"gx"`
	GY       float32   `json:"gy"`
	GZ       float32   `json:"gz"`
	MX       float32   `json:"mx"`
	MY       float32   `json:"my"`
	Age      float64   `json:"age"`
	Slot     int       `json:"slot"`
	Seq      float32   `json:"seq"`
	Updated  time.Time `json:"-"`
}

var (
	mu     sync.RWMutex
	last   Pos
	gotFix bool
)

// posOK 世界坐标合法（M1' 实测收敛版；整数 X/Z 放行——真实玩家坐标可为整数）。
func posOK(x, alt, z float32) bool {
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
	if x == 0 && z == 0 {
		return false
	}
	if math.Abs(xf) < 10 && math.Abs(zf) < 10 {
		return false
	}
	// 参数表特征：X≈0 且 alt≈1（占位坐标）；真实玩家 alt 连续非 1
	if math.Abs(xf-math.Round(xf)) < 0.001 && math.Abs(af-math.Round(af)) < 0.001 {
		return false
	}
	// alt 与 z 同时为整数且 x 为整数（全整数三元组参数表特征）；允许 X/Z 整数但 alt 非整数
	if math.Abs(xf-math.Round(xf)) < 0.001 && math.Abs(af-math.Round(af)) < 0.001 && math.Abs(zf-math.Round(zf)) < 0.001 {
		return false
	}
	return true
}

func readPos(p *winmem.Process, addr uintptr) ([3]float32, bool) {
	buf, err := p.ReadTimeout(addr, 12, 500*time.Millisecond)
	if err != nil || len(buf) < 12 {
		return [3]float32{}, false
	}
	pos := [3]float32{
		math.Float32frombits(binary.LittleEndian.Uint32(buf[0:])),
		math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
		math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
	}
	if !posOK(pos[0], pos[1], pos[2]) {
		return [3]float32{}, false
	}
	return pos, true
}

func dist2(a, b [3]float32) float64 {
	dx := float64(a[0] - b[0])
	dz := float64(a[2] - b[2])
	return dx*dx + dz*dz
}

func locHandler(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	if !gotFix {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":false,"verified":false,"source":"player","error":"no fix yet"}`+"\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(last)
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
			log.Fatalf("[attach] 没找到 eden.exe，请先启动 Eden 并进游戏")
		}
		pid = pids[0]
	}
	p, err := winmem.Open(pid)
	if err != nil {
		log.Fatalf("[attach] Open 失败：%v", err)
	}
	defer p.Close()
	log.Printf("[attach] pid=%d %s", pid, p.ExePath())
	base := uintptr(*addrFlag)
	s1 := base + uintptr(*elem1Off)
	s2 := base + uintptr(*elem2Off)
	log.Printf("[known] base=0x%X 槽1=0x%X 槽2=0x%X @ %dHz", base, s1, s2, *hzFlag)

	http.HandleFunc("/pos", locHandler)
	go func() {
		log.Printf("[http] listening on :%d  GET /pos", *portFlag)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *portFlag), nil); err != nil {
			log.Fatalf("[http] %v", err)
		}
	}()

	interval := time.Second / time.Duration(*hzFlag)
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}

	var prev [3]float32
	havePrev := false
	loop := 0
	start := time.Now()
	for {
		t0 := time.Now()
		p1, ok1 := readPos(p, s1)
		time.Sleep(*readSleep)
		p2, ok2 := readPos(p, s2)
		time.Sleep(*readSleep)

		var chosen [3]float32
		slot := 0
		switch {
		case ok1 && ok2:
			// 双字段有效：取距上一轮输出更近者（玩家连续移动，新位置必近）
			if havePrev {
				if dist2(p1, prev) <= dist2(p2, prev) {
					chosen, slot = p1, 1
				} else {
					chosen, slot = p2, 2
				}
			} else {
				chosen, slot = p1, 1
			}
		case ok1:
			chosen, slot = p1, 1
		case ok2:
			chosen, slot = p2, 2
		default:
			loop++
			if loop%50 == 0 {
				log.Printf("[warn] 双字段均校验失败，loop=%d", loop)
			}
			time.Sleep(interval)
			continue
		}

		prev = chosen
		havePrev = true
		age := time.Since(start).Seconds()
		mu.Lock()
		last = Pos{
			OK:       true,
			Verified: true,
			Source:   "player",
			GX:       chosen[0],
			GY:       chosen[1],
			GZ:       chosen[2],
			MX:       chosen[0],
			MY:       chosen[2],
			Age:      age,
			Slot:     slot,
			Seq:      0,
			Updated:  time.Now(),
		}
		gotFix = true
		mu.Unlock()

		loop++
		if loop%10 == 1 {
			log.Printf("[pos] (%.1f, %.1f, %.1f) slot=%d age=%.2fs", chosen[0], chosen[1], chosen[2], slot, age)
		}
		// 对齐采样周期
		elapsed := time.Since(t0)
		if elapsed < interval {
			time.Sleep(interval - elapsed)
		}
	}
}
