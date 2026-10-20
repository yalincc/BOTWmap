// M2'：known 地址 10Hz 连续采样验证。
//
// 目的：验证「known 位置历史缓冲地址」可直接当定位输出——
//   每 100ms 读一次 elem1（known 地址 +0x28 槽），连续 20 帧：
//     ① 每帧结构指纹通过（单位四元数 + 合法世界坐标）；
//     ② 玩家移动时坐标逐帧平滑变化（无乒乓跳变）；
//     ③ 静止时保留最后位置。
// 输出坐标序列 + 每帧位移，供与游戏内实际动作对照。
package main

import (
	"encoding/binary"
	"flag"
	"math"
	"time"

	"botwmap/enavi2/internal/winmem"
)

var (
	m2pAddr    = flag.Uint64("addr", 0, "known 宿主地址（位置历史缓冲 base；elem1 = addr+0x28）")
	m2pFrames  = flag.Int("frames", 20, "采样帧数")
	m2pHz      = flag.Int("hz", 10, "采样频率（Hz）")
	m2pElemOff = flag.Int("elemoff", 0x28, "elem1 相对 base 的偏移（默认 0x28）")
)

func cmdM2P() {
	hr("M2' known 地址 10Hz 连续采样")

	if *m2pAddr == 0 {
		printf("[usage] -addr 0x.. 必填\n")
		return
	}
	var pid uint32
	if *pidFlag > 0 {
		pid = uint32(*pidFlag)
	} else {
		pids := winmem.FindPIDs("eden")
		if len(pids) == 0 {
			printf("[attach] 没找到 eden.exe，请先启动 Eden 并进游戏\n")
			return
		}
		pid = pids[0]
	}
	p, err := winmem.Open(pid)
	if err != nil {
		printf("[attach] Open 失败：%v\n", err)
		return
	}
	defer p.Close()
	printf("[attach] pid=%d %s\n", pid, p.ExePath())
	printf("[known] base=0x%X elem1=0x%X 采样 %d 帧 @ %dHz\n\n", *m2pAddr, *m2pAddr+uint64(*m2pElemOff), *m2pFrames, *m2pHz)

	elem1 := uintptr(*m2pAddr + uint64(*m2pElemOff))
	interval := time.Second / time.Duration(*m2pHz)
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}

	var prev [3]float32
	havePrev := false
	bad := 0
	for i := 0; i < *m2pFrames; i++ {
		buf, err := p.ReadTimeout(elem1, 0x1C, 500*time.Millisecond)
		if err != nil || len(buf) < 0x1C {
			printf("  #%02d 读取失败：%v\n", i, err)
			bad++
			time.Sleep(interval)
			continue
		}
		pos := f3(buf)
		w := math.Float32frombits(binary.LittleEndian.Uint32(buf[0x0C:]))
		x := math.Float32frombits(binary.LittleEndian.Uint32(buf[0x10:]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(buf[0x14:]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(buf[0x18:]))
		n := float64(w)*float64(w) + float64(x)*float64(x) + float64(y)*float64(y)
		ok := n >= 0.95 && n <= 1.05 && worldCoordOK(pos[0], pos[1], pos[2])
		if !ok {
			bad++
			printf("  #%02d pos(%.2f, %.2f, %.2f) 指纹不符（模长²=%.4f）\n", i, pos[0], pos[1], pos[2], n)
			time.Sleep(interval)
			continue
		}
		dist := 0.0
		if havePrev {
			dist = math.Hypot(float64(pos[0]-prev[0]), float64(pos[2]-prev[2]))
		}
		prev = pos
		havePrev = true
		printf("  #%02d pos(%.2f, %.2f, %.2f) 角度z=%.1f 帧位移 %.2fm\n", i, pos[0], pos[1], pos[2], z, dist)
		time.Sleep(interval)
	}

	printf("\n--- M2' 判定 ---\n")
	if bad > 0 {
		printf("⚠ 有 %d/%d 帧指纹不符——known 地址已失效（缓冲迁移/游戏重启），需重扫\n", bad, *m2pFrames)
		return
	}
	printf("★ PASS：%d 帧全部结构指纹通过——known 秒锁可直接输出坐标（静止时保留最后位置）\n", *m2pFrames)
}
