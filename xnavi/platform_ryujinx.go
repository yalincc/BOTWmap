// platform_ryujinx.go — Ryujinx 平台驱动。
//
// 基线：BotwNavi（live-go）。差异点：
//   - 进程：前缀匹配 "ryujinx"（进程名多变：Ryujinx / Ryujinx.Ava）
//   - 内存：guest DRAM 表现为 MEM_MAPPED 提交区（有 32GB 高的镜像块需剔除）
//   - 字节序：Switch = 小端
//   - 存档：%APPDATA%\Ryujinx\bis\user\save；主档 = 最高 playtime 槽

package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
)

type ryujinxPlatform struct {
	h   uintptr
	pid uint32
}

func init() {
	registerPlatform(&ryujinxPlatform{})
}

func (p *ryujinxPlatform) Name() string { return "Ryujinx" }

func (p *ryujinxPlatform) Attach() bool {
	pid := findPid("ryujinx")
	if pid == 0 {
		p.h, p.pid = 0, 0
		return false
	}
	if pid == p.pid && p.h != 0 {
		return true
	}
	if p.h != 0 {
		closeHandle(p.h)
	}
	h, err := openProcess(pid)
	if err != nil || h == 0 {
		p.h, p.pid = 0, 0
		return false
	}
	p.h, p.pid = h, pid
	return true
}

func (p *ryujinxPlatform) Handle() uintptr { return p.h }
func (p *ryujinxPlatform) PID() uint32     { return p.pid }
func (p *ryujinxPlatform) LittleEndian() bool { return true }

// Blocks guest DRAM：MEM_MAPPED 提交区（剔除 32GB 高的镜像块）。
func (p *ryujinxPlatform) Blocks(minMB float64) []MemBlock {
	return ryuGuestBlocks(p.h, minMB)
}

// LargestBlock 最大 guest DRAM（known 偏移重定位锚点）。
func (p *ryujinxPlatform) LargestBlock(minMB float64) (uintptr, uintptr) {
	return ryuLargestGuestBlock(p.h, minMB)
}

// DecodeTriple 小端 12 字节 → (X, alt, Z)。
func (p *ryujinxPlatform) DecodeTriple(d []byte) (float32, float32, float32, bool) {
	if len(d) < 12 {
		return 0, 0, 0, false
	}
	x := math.Float32frombits(binary.LittleEndian.Uint32(d[0:4]))
	alt := math.Float32frombits(binary.LittleEndian.Uint32(d[4:8]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(d[8:12]))
	if x == 0 && alt == 0 && z == 0 {
		return 0, 0, 0, false // 未初始化槽
	}
	return x, alt, z, true
}

// SaveRoots Ryujinx 存档根（Switch 版）。
func (p *ryujinxPlatform) SaveRoots() []string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		appdata = os.Getenv("USERPROFILE") + "\\AppData\\Roaming"
	}
	return []string{filepath.Join(appdata, "Ryujinx", "bis", "user", "save")}
}

// KnownOffsets Ryujinx BOTW 已知坐标偏移。
// 0x97FA3FC0 实测不是玩家坐标（读出来 alt 为负 / 离存档点 2000+m），暂禁。
// 等重新用"移动验证法"找到真正的偏移再开。
func (p *ryujinxPlatform) KnownOffsets() []uintptr {
	return nil // []uintptr{0x97FA3FC0}
}

// ---- 内存枚举（Ryujinx 专属，原 live-go winapi.go）----

// ryuGuestBlocks 枚举所有足够大的 RW MEM_MAPPED 提交区域（去掉 32GB 高的镜像块）。
func ryuGuestBlocks(h uintptr, minMB float64) []MemBlock {
	minSize := uintptr(minMB * 1048576.0)
	var raw []MemBlock
	baseSet := map[uintptr]bool{}
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	const mirrorStep = uintptr(0x800000000)
	for addr < limit {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, addr, &mbi) == 0 {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		p := mbi.Protect & 0xFF
		if mbi.State == memCommit && mbi.Type == memMapped &&
			(p == 0x02 || p == 0x04) {
			if size >= minSize {
				raw = append(raw, MemBlock{base, size})
				baseSet[base] = true
			}
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	var out []MemBlock
	for _, b := range raw {
		if !baseSet[b.Base+mirrorStep] {
			out = append(out, b)
		}
	}
	return out
}

// ryuLargestGuestBlock 返回最大的一块 guest DRAM（供 known offsets 重定位）。
// 过滤镜像块：和 ryuGuestBlocks 一样排除 base+0x800000000 也存在的块。
func ryuLargestGuestBlock(h uintptr, minMB float64) (uintptr, uintptr) {
	const mirrorStep = uintptr(0x800000000)
	type block struct{ base, size uintptr }
	var candidates []block
	addr := uintptr(0)
	const limit = uintptr(0x7FFFFFFFFFFF)
	for addr < limit {
		var mbi MemoryBasicInformation
		if virtualQueryEx(h, addr, &mbi) == 0 {
			break
		}
		base, size := mbi.BaseAddress, mbi.RegionSize
		if mbi.State == memCommit && mbi.Type == memMapped &&
			((mbi.Protect&0xFF) == 0x02 || (mbi.Protect&0xFF) == 0x04) &&
			(mbi.Protect&pageGuard) == 0 && size >= uintptr(minMB*1048576.0) {
			candidates = append(candidates, block{base, size})
		}
		nxt := base + size
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	for _, c := range candidates {
		isMirror := false
		for _, c2 := range candidates {
			if c2.base == c.base+mirrorStep {
				isMirror = true
				break
			}
		}
		if !isMirror {
			return c.base, c.size
		}
	}
	if len(candidates) > 0 {
		return candidates[0].base, candidates[0].size
	}
	return 0, 0
}
