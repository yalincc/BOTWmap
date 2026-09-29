// diagnostic.go — 成功锁定后 dump 诊断信息，供离线分析指针链。
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type diagSample struct {
	Timestamp    string   `json:"timestamp"`
	Platform     string   `json:"platform"`
	BlockBase    uintptr  `json:"block_base"`
	PlayerAddr   uintptr  `json:"player_addr"`
	Offset       uintptr  `json:"offset_in_block"`
	Coords       [3]float32 `json:"coords"`
	NearbyPointers []uintptr `json:"nearby_pointers"`
	PointersToPlayer []uintptr `json:"pointers_to_player"`
}

func diagDir() string {
	dir := filepath.Join(".", "diagnostics")
	os.MkdirAll(dir, 0755)
	return dir
}

// dumpDiagnostic 在后台 goroutine 里跑，不阻塞跟随。
// 1) 记录基本信息 + 周边指针
// 2) 扫整个 guest 内存找指向 playerAddr 的 8 字节指针
func dumpDiagnostic(playerAddr uintptr, coords [3]float32) {
	go func() {
		defer func() { recover() }()
		p := currentPlatform()
		if p == nil {
			return
		}
		h := p.Handle()
		if h == 0 {
			return
		}
		blks := p.Blocks(minBlockMB)
		var blockBase uintptr
		for _, b := range blks {
			if playerAddr >= b.Base && playerAddr < b.Base+b.Size {
				blockBase = b.Base
				break
			}
		}
		if blockBase == 0 {
			blockBase = guestRamBase()
		}

		s := diagSample{
			Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
			Platform:   p.Name(),
			BlockBase:  blockBase,
			PlayerAddr: playerAddr,
			Offset:     playerAddr - blockBase,
			Coords:     coords,
		}

		// 周边指针：读 playerAddr 前后 512 字节，找看起来像指针的值
		near := readMem(h, playerAddr-256, 1024)
		if len(near) >= 8 {
			for i := 0; i+8 <= len(near); i += 8 {
				var v uint64
				if p.LittleEndian() {
					v = binary.LittleEndian.Uint64(near[i : i+8])
				} else {
					v = binary.BigEndian.Uint64(near[i : i+8])
				}
				// 指针值应该在 guest block 范围内
				uv := uintptr(v)
				for _, b := range blks {
					if uv >= b.Base && uv < b.Base+b.Size {
						s.NearbyPointers = append(s.NearbyPointers, playerAddr-256+uintptr(i))
						break
					}
				}
			}
		}

		// 扫整个 guest 内存找指向 playerAddr 的指针
		// 用 unsafe 零拷贝读，每 8 字节对齐
		target := uint64(playerAddr)
		for _, b := range blks {
			off := uintptr(0)
			for off < b.Size {
				n := uintptr(chunkSize)
				if b.Size-off < n {
					n = b.Size - off
				}
				buf := readMem(h, b.Base+off, int(n))
				if len(buf) < 8 {
					off += n
					continue
				}
				// 每 8 字节对齐扫描
				for i := 0; i+8 <= len(buf); i += 8 {
					var v uint64
					if p.LittleEndian() {
						v = binary.LittleEndian.Uint64(buf[i : i+8])
					} else {
						v = binary.BigEndian.Uint64(buf[i : i+8])
					}
					if v == target {
						s.PointersToPlayer = append(s.PointersToPlayer, b.Base+off+uintptr(i))
					}
				}
				off += n
			}
		}

		fn := filepath.Join(diagDir(),
			fmt.Sprintf("diag_%s_%s.json", p.Name(), time.Now().Format("150405")))
		data, _ := json.MarshalIndent(s, "", "  ")
		os.WriteFile(fn, data, 0644)
		fmt.Printf("  [diag] dumped %s: offset=0x%X, nearby=%d, ptr-to-player=%d\n",
			fn, s.Offset, len(s.NearbyPointers), len(s.PointersToPlayer))
	}()
}
