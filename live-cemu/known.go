// known_addrs.json 持久化：记住坐标槽相对 memory_base（guest 0x00000000）的偏移。
// Cemu 重启后 memory_base 变化、偏移不变 → 秒开快路径。

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const knownFile = "known_addrs.json"

// Fix 4：偏移记忆条数上限，只保留最近 knownCap 条（防垃圾偏移污染投票）。
const knownCap = 10

type knownData struct {
	Block   string    `json:"block"`
	Offsets []string  `json:"offsets"`
	Pos     []float32 `json:"pos,omitempty"` // 上次锁定坐标（内存序 X, alt, Z）——坐标先验锚
	Updated string    `json:"updated"`
}

func knownPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), knownFile)
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
	// 坐标先验只在平滑确认后写入：pos 为零值（未确认）时保留旧 pos，防止漂移坐标污染先验。
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

// loadKnownData 读 known_addrs.json（不存在/损坏返回零值）。
func loadKnownData() (knownData, error) {
	var d knownData
	data, err := os.ReadFile(knownPath())
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(data, &d)
	return d, err
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
