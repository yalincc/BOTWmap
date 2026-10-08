// savefile.go — 存档发现与解析（双平台统一）。
//
// 合并自 live-go/savefile.go（Switch 小端）与 live-cemu/savefile_cemu.go（Wii U 大端）：
//   - 格式：game_data.sav，12 字节头 + 0x0C 起 [hash u32][value u32] 扁平表；
//     玩家坐标 = PLAYER_POSITION hash 连续三个 8 字节块（同 hash 重复三次）
//   - 字节序：Switch=小端，Wii U=大端，按头部版本字段自动识别（版本表两平台共用）
//   - 主档策略：Ryujinx=最高 playtime 槽（同位置去重）；Cemu=最新 mtime 槽
//     （BotW 轮转写入 6 个槽位，槽号固定不可靠，Cemu 实测按 mtime 取最新）

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
	botwTitleID        = "01007EF00011E000" // BOTW Switch 标题 ID（Eden 存档根过滤用）
)

// validSaveVersions 存档格式版本号（Switch/Wii U 共用，仅字节序相反）。
var validSaveVersions = []uint32{0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E}

// SaveAnchor 一条存档的解析结果：内存三元组 = (X east, Y altitude, Z south+)。
type SaveAnchor struct {
	Path     string
	Playtime uint32
	Koroks   uint32
	Pos      [3]float32 // (X, alt, Z) 内存序
}

// detectEndian 按头部版本字段判定：返回 true=小端（Switch），false=大端（Wii U/Cemu）。
// 兜底默认小端。
func detectEndian(data []byte) bool {
	if len(data) >= 4 {
		vLE := binary.LittleEndian.Uint32(data[0:4])
		vBE := binary.BigEndian.Uint32(data[0:4])
		for _, v := range validSaveVersions {
			if v == vLE {
				return true
			}
			if v == vBE {
				return false
			}
		}
	}
	return true
}

// parseGameData 解析单个存档 → hash 表 + 玩家坐标 + 元信息（字节序自适应）。
func parseGameData(path string) (map[uint32]uint32, *SaveAnchor) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	little := detectEndian(data)
	u32 := binary.BigEndian.Uint32
	if little {
		u32 = binary.LittleEndian.Uint32
	}
	entries := map[uint32]uint32{}
	o := 0x0C
	for o+8 <= len(data) {
		h := u32(data[o:])
		entries[h] = u32(data[o+4:])
		o += 8
	}

	var pos [3]float32
	hasPos := false
	for i := 0x0C; i+24 <= len(data); i += 8 {
		if u32(data[i:]) == playerPositionHash {
			pos = [3]float32{
				math.Float32frombits(u32(data[i+4:])),
				math.Float32frombits(u32(data[i+12:])),
				math.Float32frombits(u32(data[i+20:])),
			}
			hasPos = true
			break
		}
	}
	if !hasPos || !gameValidTriple(pos[0], pos[1], pos[2]) {
		return entries, nil
	}
	return entries, &SaveAnchor{
		Path:     path,
		Playtime: entries[playtimeHash],
		Koroks:   entries[korokCounterHash],
		Pos:      pos,
	}
}

// findSaveFiles 在根目录下递归找当前游戏存档文件（不区分大小写）。
// 文件名清单来自游戏适配层（game.SaveFileNames），BOTW 为 game_data.sav。
func findSaveFiles(root string, names []string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			for _, n := range names {
				if strings.EqualFold(d.Name(), n) {
					out = append(out, p)
					break
				}
			}
		}
		return nil
	})
	return out
}

// allSaveFiles 遍历当前平台所有存档根候选（去重、排序）。
// 平台手动路径（--<平台>-save-dir，GUI 设置）由各平台 SaveRoots 最优先返回；
// Eden 存档根可能混入其他 Switch 游戏存档（同目录多个 title），按 BOTW 标题 ID
// 过滤；Cemu（Wii U）/Ryujinx 存档目录本就只含 BOTW，不过滤（零影响）。
func allSaveFiles(p Platform) []string {
	names := currentGame.SaveFileNames()
	var out []string
	for _, root := range p.SaveRoots() {
		files := findSaveFiles(root, names)
		if p.Name() == "Eden" {
			var ef []string
			for _, f := range files {
				if strings.Contains(strings.ToUpper(f), botwTitleID) {
					ef = append(ef, f)
				}
			}
			files = ef
		}
		out = append(out, files...)
	}
	seen := map[string]bool{}
	var uniq []string
	for _, f := range out {
		if !seen[f] {
			seen[f] = true
			uniq = append(uniq, f)
		}
	}
	sort.Strings(uniq)
	return uniq
}

// round1 四舍五入到 1 位小数（×10 取整）。
func round1(v float32) float32 {
	return float32(math.Round(float64(v)*10) / 10)
}
