package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mkTestSave 合成存档：12 字节头（偏移 0 放版本字段）+ 0x0C 起 [hash u32][value u32]。
func mkTestSave(t *testing.T, bigEndian bool, version uint32, entries map[uint32]uint32) []byte {
	t.Helper()
	buf := make([]byte, 0x0C+8*len(entries)+8) // 尾部多 8 字节填充无妨
	if bigEndian {
		binary.BigEndian.PutUint32(buf[0:], version)
	} else {
		binary.LittleEndian.PutUint32(buf[0:], version)
	}
	o := 0x0C
	for h, v := range entries {
		if bigEndian {
			binary.BigEndian.PutUint32(buf[o:], h)
			binary.BigEndian.PutUint32(buf[o+4:], v)
		} else {
			binary.LittleEndian.PutUint32(buf[o:], h)
			binary.LittleEndian.PutUint32(buf[o+4:], v)
		}
		o += 8
	}
	return buf
}

func TestDetectEndian(t *testing.T) {
	cases := []struct {
		name string
		be   bool
	}{
		{"switch_le", false},
		{"wiiu_be", true},
	}
	for _, c := range cases {
		buf := mkTestSave(t, c.be, 0x471B, map[uint32]uint32{})
		if got := detectEndian(buf); got == c.be {
			t.Errorf("%s: detectEndian=%v want %v", c.name, got, !c.be)
		}
	}
	// 未知版本兜底小端
	unknown := make([]byte, 16)
	binary.BigEndian.PutUint32(unknown[0:], 0xDEADBEEF)
	if !detectEndian(unknown) {
		t.Errorf("unknown version should fall back to little endian")
	}
}

func TestParseCemuSave(t *testing.T) {
	entries := map[uint32]uint32{
		0x73C29681: 3600, // PlayTime 1h
		0x12345678: 2,    // 完成
		0x11111111: 0,    // 未完成
	}
	for i := 0; i < 120; i++ { // 满足 ≥100 条结构校验
		entries[uint32(0x20000000+i)] = uint32(i)
	}
	for _, c := range []struct {
		name string
		be   bool
	}{
		{"le", false},
		{"be", true},
	} {
		dir := t.TempDir()
		p := filepath.Join(dir, "game_data.sav")
		if err := os.WriteFile(p, mkTestSave(t, c.be, 0x471B, entries), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := parseCemuSave(p)
		if err != nil {
			t.Fatalf("%s: parse err %v", c.name, err)
		}
		if got[0x73C29681] != 3600 || got[0x12345678] != 2 || got[0x11111111] != 0 {
			t.Errorf("%s: parse mismatch: %v", c.name, got)
		}
	}
	// 条目过少报错
	dir := t.TempDir()
	p := filepath.Join(dir, "game_data.sav")
	os.WriteFile(p, mkTestSave(t, true, 0x471B, map[uint32]uint32{0x1: 1, 0x2: 2}), 0644)
	if _, err := parseCemuSave(p); err == nil {
		t.Errorf("too-few-entries save should error")
	}
}

func TestLatestCemuSave(t *testing.T) {
	dir := t.TempDir()
	for i, slot := range []string{"0", "1", "2"} {
		d := filepath.Join(dir, slot)
		os.MkdirAll(d, 0755)
		p := filepath.Join(d, "game_data.sav")
		os.WriteFile(p, mkTestSave(t, true, 0x471B, map[uint32]uint32{uint32(i): 1}), 0644)
		// 人为设置 mtime：slot2 最新
		mt := time.Now().Add(-time.Duration(len(slot)) * time.Hour)
		os.Chtimes(p, mt, mt)
	}
	files := findCemuSaveFilesDir(dir)
	best, err := latestCemuSave(files)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(best)) != "2" {
		t.Errorf("latest should be slot 2, got %s", best)
	}
}

// findCemuSaveFilesDir 与 findCemuSaveFiles 同逻辑，测试用固定根。
func findCemuSaveFilesDir(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "game_data.sav" {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestRealSaveDiscovery 真机探测（PROBE=1 时运行）：验证候选根能找到本机 Cemu 存档并选最新槽。
func TestRealSaveDiscovery(t *testing.T) {
	if os.Getenv("PROBE") != "1" {
		t.Skip("real-environment probe; set PROBE=1 to run")
	}
	files := findCemuSaveFiles()
	t.Logf("found %d save files under %d candidate roots", len(files), len(cemuSaveTitleRoots()))
	for _, r := range cemuSaveTitleRoots() {
		t.Logf("  root: %s", r)
	}
	for _, p := range files {
		fi, _ := os.Stat(p)
		t.Logf("  %s  (mtime %v)", p, fi.ModTime().Format("2006-01-02 15:04:05"))
	}
	best, err := latestCemuSave(files)
	if err != nil {
		t.Fatalf("latestCemuSave: %v", err)
	}
	fi, _ := os.Stat(best)
	t.Logf("LATEST: %s (mtime %v)", best, fi.ModTime().Format("2006-01-02 15:04:05"))
	entries, err := parseCemuSave(best)
	if err != nil {
		t.Fatalf("parse latest: %v", err)
	}
	t.Logf("latest entries=%d playtime=%dh", len(entries), entries[playtimeHash]/3600)
}
