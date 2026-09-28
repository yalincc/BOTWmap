// savefile_cemu.go — Cemu（Wii U 版）存档发现与解析
//
// 格式（老大 2026-09-28 v1.4.0 实测 + 本机存档验证）：
//   - 文件名 game_data.sav，与 Switch 同一扁平结构：12 字节头 + 0x0C 起 [hash u32][value u32]
//   - 仅字节序不同：Wii U（Cemu）= 大端；按头部版本字段自动识别（版本表两平台共用）
//   - 主档选择：BotW 轮转写入 6 个槽位（0-5），每次保存写下一个槽 → 按 mtime 取最新，
//     槽号固定不可靠（本机实测：通关神庙后最新槽 shrine+1，旧槽无）
//   - 完成判定：对照参考表 progress_points.json 的 done_hash（神庙通关等用 done_hash，
//     勿用基础 hash）

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 存档格式版本号（Switch/Wii U 共用，仅字节序相反；与 BOTWmap v1.4.0 一致）
var validSaveVersions = []uint32{0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E}

const (
	saveRootEnv = "CEMUNAVI_SAVE_ROOT" // 手动指定：mlc01/usr/save/00050000 所在目录
	saveName    = "game_data.sav"
	playtimeHash = 0x73C29681 // PlayTime（参考）
)

// cemuSaveTitleRoots 返回候选的「mlc01/usr/save/00050000」根目录（按序探测）。
func cemuSaveTitleRoots() []string {
	var roots []string
	add := func(p string) {
		if p != "" {
			roots = append(roots, p)
		}
	}
	if v := os.Getenv(saveRootEnv); v != "" {
		add(v)
	}
	// %APPDATA%\Cemu\mlc01（本机实测路径）
	if app := os.Getenv("APPDATA"); app != "" {
		add(filepath.Join(app, "Cemu", "mlc01", "usr", "save", "00050000"))
	}
	// exe 同目录 mlc01（Cemu 便携版）
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), "mlc01", "usr", "save", "00050000"))
	}
	// 常见安装目录
	for _, base := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), "Documents", "Cemu"),
		`C:\Cemu`,
		`D:\Cemu`,
		`E:\Cemu`,
	} {
		add(filepath.Join(base, "mlc01", "usr", "save", "00050000"))
	}
	return roots
}

// findCemuSaveFiles 在候选根下递归找 game_data.sav（TitleID 不区分，101C9300/9400/9500 通用）。
func findCemuSaveFiles() []string {
	var out []string
	for _, root := range cemuSaveTitleRoots() {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && strings.EqualFold(d.Name(), saveName) {
				out = append(out, p)
			}
			return nil
		})
	}
	// 去重（多候选根可能重叠）
	seen := map[string]bool{}
	var uniq []string
	for _, p := range out {
		if !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	sort.Strings(uniq)
	return uniq
}

// detectEndian 按头部版本字段判定：返回 true=小端（Switch），false=大端（Wii U/Cemu）。
// 兜底默认小端（与 BOTWmap v1.4.0 一致）。
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

// parseCemuSave 解析单个存档 → 扁平表 entries（hash → value），字节序自适应。
func parseCemuSave(path string) (map[uint32]uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	little := detectEndian(data)
	entries := make(map[uint32]uint32, len(data)/8)
	o := 0x0C
	if little {
		for o+8 <= len(data) {
			h := binary.LittleEndian.Uint32(data[o:])
			entries[h] = binary.LittleEndian.Uint32(data[o+4:])
			o += 8
		}
	} else {
		for o+8 <= len(data) {
			h := binary.BigEndian.Uint32(data[o:])
			entries[h] = binary.BigEndian.Uint32(data[o+4:])
			o += 8
		}
	}
	if len(entries) < 100 {
		return entries, fmt.Errorf("save structure abnormal (%d entries < 100): %s", len(entries), path)
	}
	return entries, nil
}

// latestCemuSave 按文件 mtime 返回最新存档（BotW 轮转槽位，mtime 即当前进度）。
func latestCemuSave(files []string) (string, error) {
	var best string
	var bestT time.Time
	for _, p := range files {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if best == "" || fi.ModTime().After(bestT) {
			best, bestT = p, fi.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no game_data.sav found under Cemu save roots; set CEMUNAVI_SAVE_ROOT if needed")
	}
	return best, nil
}

// saveDirLabel 输出存档位置（日志用）。
func saveDirLabel(path string) string {
	root := ""
	for _, r := range cemuSaveTitleRoots() {
		if r != "" && strings.HasPrefix(path, r) {
			root = r
			break
		}
	}
	if root != "" {
		return strings.TrimPrefix(path, root+string(filepath.Separator))
	}
	return path
}
