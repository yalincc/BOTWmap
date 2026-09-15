// 存档解析：从 Ryujinx 存档目录找 game_data.sav，取主锚点（最高 playtime 槽）
// 的玩家坐标。格式：12 字节头 + 从 0x0C 起 [hash u32][value u32] 扁平表，
// 玩家坐标是三个连续的 8 字节块（同 hash 重复三次）：[id][f0][id][f1][id][f2]。

package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	playerPositionHash = 0xA40BA103 // PLAYER_POSITION
	playtimeHash       = 0x73C29681 // PlayTime
	korokCounterHash   = 0x8A94E07A // HiddenKorok_Number
)

// SaveAnchor 一条存档的解析结果：内存三元组 = (X east, Y altitude, Z south+)。
type SaveAnchor struct {
	Path     string
	Playtime uint32
	Koroks   uint32
	Pos      [3]float32 // (X, alt, Z)
}

func saveRoot() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		appdata = os.Getenv("USERPROFILE") + "\\AppData\\Roaming"
	}
	return filepath.Join(appdata, "Ryujinx", "bis", "user", "save")
}

func findSaveFiles(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "game_data.sav") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func sane(x, alt, z float32) bool {
	return x > -7000 && x < 7000 && z > -7000 && z < 7000 &&
		alt > -600 && alt < 5000 && (abs32(x) > 1 || abs32(z) > 1)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// parseSave 解析单个存档：返回 hash 表 + 玩家坐标 + 元信息。
func parseSave(path string) (map[uint32]uint32, *SaveAnchor) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	entries := map[uint32]uint32{}
	o := 0x0C
	for o+8 <= len(data) {
		h := binary.LittleEndian.Uint32(data[o:])
		entries[h] = binary.LittleEndian.Uint32(data[o+4:])
		o += 8
	}

	var pos [3]float32
	hasPos := false
	for i := 0x0C; i+24 <= len(data); i += 8 {
		if binary.LittleEndian.Uint32(data[i:]) == playerPositionHash {
			pos = [3]float32{
				math.Float32frombits(binary.LittleEndian.Uint32(data[i+4:])),
				math.Float32frombits(binary.LittleEndian.Uint32(data[i+12:])),
				math.Float32frombits(binary.LittleEndian.Uint32(data[i+20:])),
			}
			hasPos = true
			break
		}
	}
	if !hasPos || !sane(pos[0], pos[1], pos[2]) {
		return entries, nil
	}
	return entries, &SaveAnchor{
		Path:     path,
		Playtime: entries[playtimeHash],
		Koroks:   entries[korokCounterHash],
		Pos:      pos,
	}
}

// readSaveAnchors 读取所有可用存档，按 playtime 降序（同位置去重，保留最高 playtime）。
func readSaveAnchors() []*SaveAnchor {
	files := findSaveFiles(saveRoot())
	var out []*SaveAnchor
	seen := map[[3]int32]int{}
	for _, p := range files {
		_, a := parseSave(p)
		if a == nil {
			continue
		}
		key := [3]int32{int32(round1(a.Pos[0])), int32(round1(a.Pos[2])), int32(round1(a.Pos[1]))}
		if i, ok := seen[key]; ok {
			if a.Playtime > out[i].Playtime { // 同位置多个槽：保留游玩时间最高者
				out[i] = a
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Playtime > out[j].Playtime })
	return out
}

// round1 四舍五入到 1 位小数（×10 取整）。
func round1(v float32) float32 {
	return float32(math.Round(float64(v)*10) / 10)
}
