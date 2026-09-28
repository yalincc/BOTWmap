// known_addrs.json 持久化：记住坐标槽相对平台锚块（Ryujinx=最大 guest DRAM、
// Cemu=最大 RW 块）的偏移，重启秒开快路径。锚块基址每次启动变、偏移稳定。
//
// 格式带 Pos（上次锁定坐标，内存序 X, alt, Z）——坐标先验锚，Cemu 结构扫描就近选槽用。
// 已知偏移只在平滑/移动确认后写入，防漂移坐标污染。
// 文件按平台分开（known_ryujinx.json / known_cemu.json）：不同模拟器的偏移不混用。

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Fix 4：偏移记忆条数上限——旧版无限追加，实测积累垃圾偏移，快路径投票被污染。
const knownCap = 10

type knownData struct {
	Block   string    `json:"block"`
	Offsets []string  `json:"offsets"`
	Pos     []float32 `json:"pos,omitempty"` // 上次锁定坐标（内存序 X, alt, Z）
	Updated string    `json:"updated"`
}

// knownPath 按当前平台分开文件，避免不同模拟器的偏移混用。
func knownPath() string {
	plat := currentPlatform()
	name := "unknown"
	if plat != nil {
		name = strings.ToLower(plat.Name())
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), fmt.Sprintf("known_%s.json", name))
}

func parseHex(v string) uintptr {
	s := strings.TrimPrefix(strings.TrimSpace(v), "0x")
	s = strings.TrimPrefix(s, "0X")
	n, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0
	}
	return uintptr(n)
}

func loadOffsets() []uintptr {
	var d knownData
	if data, err := os.ReadFile(knownPath()); err == nil {
		json.Unmarshal(data, &d)
	}
	var out []uintptr
	add := func(v uintptr) {
		if v > 0 && v < 0x100000000 {
			for _, x := range out {
				if x == v {
					return
				}
			}
			out = append(out, v)
		}
	}
	blk := parseHex(d.Block)
	for _, v := range d.Offsets {
		add(parseHex(v))
	}
	for _, v := range d.Offsets { // 兼容旧格式：绝对地址转偏移
		a := parseHex(v)
		if a > blk {
			add(a - blk)
		}
	}
	return out
}

// saveKnown 记录偏移 + 坐标先验。pos 为内存序 (X, alt, Z)。
// pos 为零值（未确认）时保留旧 pos，防止漂移坐标污染先验。
func saveKnown(addrs []uintptr, block uintptr, pos [3]float32) {
	seen := map[uintptr]bool{}
	var offs []string
	for _, a := range addrs {
		o := a
		if block != 0 && a > block {
			o = a - block
		}
		if o > 0 && o < 0x100000000 && !seen[o] {
			seen[o] = true
			offs = append(offs, hex8(o))
		}
	}

	if len(offs) > knownCap {
		offs = offs[:knownCap]
	}
	var p []float32
	if pos != [3]float32{} {
		p = []float32{pos[0], pos[1], pos[2]}
	} else if d0, err := loadKnownData(); err == nil && len(d0.Pos) == 3 {
		p = d0.Pos
	}
	d := knownData{
		Block:   hex12(block),
		Offsets: offs,
		Pos:     p,
		Updated: time.Now().Format("2006-01-02 15:04:05"),
	}
	buf, _ := json.MarshalIndent(d, "", "  ")
	os.WriteFile(knownPath(), buf, 0644)
}

// loadKnownData 读 known_<platform>.json（不存在/损坏返回零值）。
func loadKnownData() (knownData, error) {
	var d knownData
	data, err := os.ReadFile(knownPath())
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(data, &d)
	return d, err
}

// loadKnownBlock 返回上次会话的锚块基址（块世代检测参照）；无则返回 0。
func loadKnownBlock() uintptr {
	d, err := loadKnownData()
	if err != nil {
		return 0
	}
	return parseHex(d.Block)
}

// loadKnownPos 返回上次锁定的坐标（内存序 X, alt, Z）；无则返回 nil。
func loadKnownPos() []float32 {
	var d knownData
	if data, err := os.ReadFile(knownPath()); err == nil {
		json.Unmarshal(data, &d)
	}
	if len(d.Pos) != 3 {
		return nil
	}
	return []float32{d.Pos[0], d.Pos[1], d.Pos[2]}
}

func hex8(v uintptr) string {
	return "0x" + strings.ToUpper(strconv.FormatUint(uint64(v), 16))
}

func hex12(v uintptr) string {
	s := "0x" + strings.ToUpper(strconv.FormatUint(uint64(v), 16))
	for len(s) < 14 {
		s = "0x0" + s[2:]
	}
	return s
}
