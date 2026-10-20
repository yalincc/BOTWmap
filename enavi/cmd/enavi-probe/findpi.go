// findpi.go —— 专项探针：寻找 uking::act::PlayerInfo 结构体及其在 NSO 中的全局单例指针。
//
// 依据：uking/src/Game/Actor/Player/actPlayerInfo.h
// 结构排布：
//   PlayerBase*             mPlayerActor;            // 0x00 ~ 0x08
//   ksys::act::BaseProcLink mPlayerLink;             // 0x08 ~ 0x20/0x28
//   ksys::act::BaseProcLink mHorseLink;              // 0x20 ~ 0x38/0x48
//   sead::Vector3f          mPlayerPos;              // ★ 林克坐标 (12 字节)
//   sead::Vector3f          mPlayerPosForPostCalc;   // ★ 后处理坐标 (12 字节，与前值几乎完全一致)
//   f32                     mMaxHeartValue;          // 最大心数 (4 字节)
//   f32                     mStaminaCurrentMax;      // 耐力上限 (4 字节，如 1000.0f)
//   f32                     mStaminaMax;             // 耐力实际值 (4 字节)
//   sead::FixedRingBuffer   mPreviousPositions;      // 60格位置历史缓冲 (步长 0x28)

package main

import (
	"encoding/binary"
	"math"
	"sort"
	"sync"
	"time"

	"botwmap/enavi/internal/saveanchor"
	"botwmap/enavi/internal/scan"
	"botwmap/enavi/internal/winmem"
)

type piMatch struct {
	addr      uintptr
	gva       uintptr
	pos1      [3]float32
	pos2      [3]float32
	heart     float32
	stamina   float32
	distSave  float64
	hasRing   bool
	rawAround []byte
}

func cmdFindPI(p *winmem.Process) {
	hr("寻找 uking::act::PlayerInfo 结构体")

	d, _, err := p.FindDram()
	if err != nil {
		printf("[findpi] %v\n", err)
		return
	}
	printf("[findpi] DRAM arenaLow=0x%X  heap=[0x%X, 0x%X)\n", d.ArenaLow, d.HeapLo(), d.HeapHi())

	var anchor [3]float32
	var hasAnchor bool
	anchors, _ := saveanchor.Find(p.ExePath())
	if len(anchors) > 0 {
		anchor = anchors[0].Pos
		hasAnchor = true
		printf("[findpi] 存档锚点: (%.1f, %.1f, %.1f) playtime=%ds\n",
			anchor[0], anchor[1], anchor[2], anchors[0].Playtime)
	} else {
		printf("[findpi] ⚠️ 未找到存档锚点，将在全堆范围内搜索\n")
	}

	regions := scan.HeapRegions(p, d, 256<<10)
	printf("[findpi] UserHeap 待扫区域: %d 个\n", len(regions))

	var matches []piMatch
	var mu sync.Mutex
	t0 := time.Now()

	// 运行扫描，低并发(2) + 块间休眠(8ms)，绝不拖垮模拟器
	runChunks(p, regions, 2, func(chunkAddr uintptr, buf []byte) {
		var local []piMatch
		for off := 0; off+48 <= len(buf); off += 4 {
			// 1) 读取第 1 个坐标
			x1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
			y1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
			z1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))

			if !worldCoordOK(x1, y1, z1) {
				continue
			}

			// 如果有锚点，限制在锚点 600m 范围内
			dSave := 0.0
			if hasAnchor {
				dx := float64(x1 - anchor[0])
				dz := float64(z1 - anchor[2])
				dSave = math.Hypot(dx, dz)
				if dSave > 600.0 {
					continue
				}
			}

			// 2) 读取紧随其后的第 2 个坐标 (mPlayerPosForPostCalc)
			x2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+12:]))
			y2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+16:]))
			z2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+20:]))

			if math.Abs(float64(x1-x2)) > 0.3 || math.Abs(float64(y1-y2)) > 0.3 || math.Abs(float64(z1-z2)) > 0.3 {
				continue
			}

			// 3) 检查后续的心数与耐力值特征
			// mMaxHeartValue 通常在 +24，mStaminaCurrentMax 通常在 +28
			heart := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+24:]))
			stamina := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+28:]))

			// 心数合理范围：4.0 ~ 160.0；耐力合理范围：800.0 ~ 3500.0
			isHeartOK := heart >= 4.0 && heart <= 160.0
			isStamOK := stamina >= 800.0 && stamina <= 3500.0

			// 只要坐标双连吻合且至少一项属性命中，即高度疑似
			if !isHeartOK && !isStamOK {
				continue
			}

			// 4) 抓取周围上下文字节 (前 64 字节，后 128 字节)
			addr := chunkAddr + uintptr(off)
			gva := addr - d.ArenaLow
			ctxStart := off - 64
			if ctxStart < 0 {
				ctxStart = 0
			}
			ctxEnd := off + 128
			if ctxEnd > len(buf) {
				ctxEnd = len(buf)
			}
			raw := make([]byte, ctxEnd-ctxStart)
			copy(raw, buf[ctxStart:ctxEnd])

			local = append(local, piMatch{
				addr:      addr,
				gva:       gva,
				pos1:      [3]float32{x1, y1, z1},
				pos2:      [3]float32{x2, y2, z2},
				heart:     heart,
				stamina:   stamina,
				distSave:  dSave,
				rawAround: raw,
			})
		}

		if len(local) > 0 {
			mu.Lock()
			matches = append(matches, local...)
			mu.Unlock()
		}
	})

	printf("[findpi] 扫描完成，耗时 %v，共命中 %d 处疑似 PlayerInfo 结构\n\n",
		time.Since(t0).Round(time.Millisecond), len(matches))

	if len(matches) == 0 {
		printf("[findpi] 未找到双连坐标+属性指纹的结构。可能原因：当前玩家所处坐标距离存档超出 600m。\n")
		return
	}

	// 按距离存档升序排序
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].distSave < matches[j].distSave
	})

	for i, m := range matches {
		if i >= 5 {
			printf("...（其余 %d 处省略）\n", len(matches)-5)
			break
		}
		printf("==================== 候选 [%d] ====================\n", i+1)
		printf("  宿主地址 host : 0x%X\n", m.addr)
		printf("  Guest VA      : 0x%X\n", m.gva)
		printf("  mPlayerPos    : (%.2f, %.2f, %.2f)  [距存档: %.1fm]\n", m.pos1[0], m.pos1[1], m.pos1[2], m.distSave)
		printf("  PostCalcPos   : (%.2f, %.2f, %.2f)  [两坐标差: dx=%.3f, dy=%.3f, dz=%.3f]\n",
			m.pos2[0], m.pos2[1], m.pos2[2],
			m.pos1[0]-m.pos2[0], m.pos1[1]-m.pos2[1], m.pos1[2]-m.pos2[2])
		printf("  MaxHeartValue : %.1f (约 %d 颗心)\n", m.heart, int(m.heart/4))
		printf("  StaminaMax    : %.1f (约 %.1f 圈轮盘)\n", m.stamina, m.stamina/1000.0)

		// 检查前后指针
		scanPointersTo(p, d, m.addr)
	}
}

// scanPointersTo 寻找在模块区或堆中有哪些指针指向该结构体附近
func scanPointersTo(p *winmem.Process, d winmem.Dram, targetHost uintptr) {
	// 假设结构体起始位置在 mPlayerPos 前 0x20 ~ 0x80
	printf("  [反向指针追踪] 探测指向 0x%X 前方结构体头部的指针...\n", targetHost)

	// 搜模块区 (.bss / .data)
	var modRegions []winmem.Region
	p.Regions(d.ArenaLow, d.HeapLo(), func(r winmem.Region) bool {
		if r.Readable() && r.Size >= 0x1000 {
			modRegions = append(modRegions, r)
		}
		return false
	})

	type ptrHit struct {
		ptrAddr   uintptr
		ptrGva    uintptr
		targetGva uintptr
		offset    int64
	}
	var hits []ptrHit
	var mu sync.Mutex

	// 探测目标头部范围：[targetHost - 0x100, targetHost]
	targetGva := targetHost - d.ArenaLow

	runChunks(p, modRegions, 2, func(chunkAddr uintptr, buf []byte) {
		for off := 0; off+8 <= len(buf); off += 8 {
			v := binary.LittleEndian.Uint64(buf[off:])
			if v == 0 {
				continue
			}
			// 匹配 guest VA
			gv := uintptr(v)
			if gv >= targetGva-0x120 && gv <= targetGva {
				delta := int64(targetGva - gv)
				mu.Lock()
				hits = append(hits, ptrHit{
					ptrAddr:   chunkAddr + uintptr(off),
					ptrGva:    (chunkAddr + uintptr(off)) - d.ArenaLow,
					targetGva: gv,
					offset:    delta,
				})
				mu.Unlock()
			}
		}
	})

	if len(hits) == 0 {
		printf("  [反向指针追踪] 模块区暂未直接扫到指向头部的指针 (可能经间接单例或多级指针访问)\n")
		return
	}

	for _, h := range hits {
		printf("  ★ 找到模块区单例指针！指针地址: host=0x%X (GVA=0x%X) -> 指向对象头部 (mPlayerPos 位于对象 +0x%X 处)\n",
			h.ptrAddr, h.ptrGva, h.offset)
	}
}
