// xnavi 单元测试：坐标校验、扫描命中、旋转矩阵、存档端序自适应、进度计算。
// 平台无关部分直接测；平台相关部分用 ryujinxPlatform{} 实例（存档路径真实）。

package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func f32(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

func f32BE(v float32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, math.Float32bits(v))
	return b
}

// ---- gameHud / 坐标校验 ----

func TestGameHud(t *testing.T) {
	// 内存序 (X=1794.6, alt=220.3, Z=1046.1) → 展示序 (gx=X, gy=alt, gz=Z)
	v := gameHud(1794.6, 220.3, 1046.1)
	if v == nil {
		t.Fatal("expected valid pos")
	}
	if math.Abs(float64(v[0]-1794.6)) > 0.01 || math.Abs(float64(v[1]-220.3)) > 0.01 || math.Abs(float64(v[2]-1046.1)) > 0.01 {
		t.Fatalf("hud mismatch: %v", v)
	}
	// 全零 → 无效
	if gameHud(0, 0, 0) != nil {
		t.Fatal("expected nil for zero pos")
	}
	// NaN → 无效（踩坑 #6）
	if gameHud(float32(math.NaN()), 220, 1046) != nil {
		t.Fatal("expected nil for NaN")
	}
	// 单轴近零且高度贴地 → 无效（按钮/未初始化槽）
	if gameHud(0.0, 1.0, 1523.4) != nil {
		t.Fatal("expected nil for button slot")
	}
	// 越界 → 无效
	if gameHud(9000, 220, 1046) != nil {
		t.Fatal("expected nil for out-of-range")
	}
}

// ---- scanChunk（小端路径，currentPlatform 为 nil 时走小端）----

func TestScanChunk(t *testing.T) {
	refs := [][3]float32{{100.5, 300, -200.25}}
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	copy(buf[1000:], append(append(append([]byte{}, f32(100.5)...), f32(300)...), f32(-200.25)...))
	var hits []Hit
	scanChunk(buf, 0x1000, 100.5-120, 100.5+120, 300-120, 300+120, -200.25-120, -200.25+120, 120*120, refs, nil, &hits)
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	found := false
	for _, h := range hits {
		if h.Addr == 0x1000+1000 && math.Abs(float64(h.X-100.5)) < 0.01 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected hit at offset 1000, got %+v", hits)
	}
}

// ---- rotOKBytes / rotOKF ----

func TestRotOK(t *testing.T) {
	// 正交矩阵（旋转 90° 绕 Z）：col1=(0,-1,0) col2=(1,0,0) col3=(0,0,1)
	mat := []float32{0, -1, 0, 1, 0, 0, 0, 0, 1}
	if !rotOKF(mat) {
		t.Fatal("expected orthogonal matrix detected")
	}
	bad := []float32{1, 2, 3, 4, 5, 6, 7, 8, 9}
	if rotOKF(bad) {
		t.Fatal("non-orthogonal matrix should not pass")
	}
	// NaN 矩阵必须拒绝
	nan := []float32{float32(math.NaN()), 0, 0, 0, 1, 0, 0, 0, 1}
	if rotOKF(nan) {
		t.Fatal("NaN matrix must be rejected")
	}
	// 缓冲内（小端）检测
	buf := make([]byte, 128)
	for i, v := range mat {
		copy(buf[16+i*4:], f32(v))
	}
	if !rotOKBytes(buf) {
		t.Fatal("expected orthogonal matrix detected in buffer")
	}
	buf2 := make([]byte, 128)
	for i, v := range bad {
		copy(buf2[16+i*4:], f32(v))
	}
	if rotOKBytes(buf2) {
		t.Fatal("non-orthogonal buffer should not pass")
	}
	// 大端缓冲检测
	buf3 := make([]byte, 128)
	for i, v := range mat {
		copy(buf3[16+i*4:], f32BE(v))
	}
	if !rotOKBytesBE(buf3) {
		t.Fatal("expected orthogonal matrix detected in BE buffer")
	}
}

// ---- 存档端序自适应 ----

// makeSave 构造最小有效 game_data.sav：头版本 + PLAYER_POSITION 三连块 + 合法坐标。
func makeSave(t *testing.T, little bool) []byte {
	t.Helper()
	buf := make([]byte, 0x0C+5*8) // 12B 头 + 5 组 hash/value
	put := func(o int, v uint32) {
		if little {
			binary.LittleEndian.PutUint32(buf[o:], v)
		} else {
			binary.BigEndian.PutUint32(buf[o:], v)
		}
	}
	// 头版本字段（两平台共用版本表）
	put(0, 0x24E2)
	put(4, 0)
	put(8, 0)
	// 0x0C 起 [hash][value] 扁平表：PLAYER_POSITION 三次（三坐标）+ PlayTime
	put(0x0C, playerPositionHash)
	put(0x10, math.Float32bits(1794.6))
	put(0x14, playerPositionHash)
	put(0x18, math.Float32bits(220.3))
	put(0x1C, playerPositionHash)
	put(0x20, math.Float32bits(1046.1))
	put(0x24, playtimeHash)
	put(0x28, 223696)
	return buf
}

func TestParseGameDataEndian(t *testing.T) {
	// 小端（Switch）
	lePath := filepath.Join(t.TempDir(), "game_data.sav")
	if err := os.WriteFile(lePath, makeSave(t, true), 0644); err != nil {
		t.Fatal(err)
	}
	entries, a := parseGameData(lePath)
	if a == nil {
		t.Fatal("little-endian save should parse")
	}
	if entries[playtimeHash] != 223696 {
		t.Fatalf("playtime mismatch: %d", entries[playtimeHash])
	}
	if math.Abs(float64(a.Pos[0]-1794.6)) > 0.01 || math.Abs(float64(a.Pos[1]-220.3)) > 0.01 || math.Abs(float64(a.Pos[2]-1046.1)) > 0.01 {
		t.Fatalf("LE pos mismatch: %v", a.Pos)
	}

	// 大端（Wii U / Cemu）
	bePath := filepath.Join(t.TempDir(), "game_data.sav")
	if err := os.WriteFile(bePath, makeSave(t, false), 0644); err != nil {
		t.Fatal(err)
	}
	_, a2 := parseGameData(bePath)
	if a2 == nil {
		t.Fatal("big-endian save should parse")
	}
	if math.Abs(float64(a2.Pos[0]-1794.6)) > 0.01 || math.Abs(float64(a2.Pos[1]-220.3)) > 0.01 || math.Abs(float64(a2.Pos[2]-1046.1)) > 0.01 {
		t.Fatalf("BE pos mismatch: %v", a2.Pos)
	}
}

// ---- 真实存档对照（Ryujinx）----

func TestParseSaveReal(t *testing.T) {
	root := filepath.Join(os.Getenv("APPDATA"), "Ryujinx", "bis", "user", "save")
	files := findSaveFiles(root, currentGame.SaveFileNames())
	if len(files) == 0 {
		t.Skip("no save files found")
	}
	rp := &ryujinxPlatform{}
	anchors := currentGame.SaveAnchors(rp)
	if len(anchors) == 0 {
		t.Fatal("no usable anchors")
	}
	primary := anchors[0]
	if primary.Playtime == 0 {
		t.Error("expected nonzero playtime")
	}
	if math.Abs(float64(primary.Pos[0])) > 6000 || math.Abs(float64(primary.Pos[2])) > 6000 || primary.Pos[1] < -100 || primary.Pos[1] > 2000 {
		t.Errorf("pos out of legal map range: got %v", primary.Pos)
	}
	entries, a := parseGameData(primary.Path)
	if a == nil {
		t.Fatal("primary save should parse")
	}
	if entries[korokCounterHash] == 0 {
		t.Log("korok counter = 0 (new save?)")
	}
}

// ---- TOTK 固定偏移共识仲裁 ----

func TestTotkPickOffset(t *testing.T) {
	g := &gameTotk{}
	mk := func(addr uintptr, x, y, z float32) OffsetCand {
		return OffsetCand{Addr: addr, Pos: [3]float32{x, y, z}}
	}
	// 多副本共识：两个偏移读到几乎相同坐标 → 锁定共识组
	cands := []OffsetCand{
		mk(0x100, 681.0, -1443.6, 1488.7),
		mk(0x200, 248.9, -820.2, 1443.9),
		mk(0x300, 681.4, -1444.1, 1488.2), // 与 0x100 共识
	}
	best, detail, ok := g.PickOffset(cands, nil)
	if !ok {
		t.Fatalf("expected consensus lock, got ok=false detail=%s", detail)
	}
	if best.Addr != 0x100 && best.Addr != 0x300 {
		t.Fatalf("expected consensus candidate, got addr=0x%X", best.Addr)
	}
	// 无共识：三个坐标都不同 → 拒绝锁定（避免锁错 NPC/静态数据）
	noise := []OffsetCand{
		mk(0x100, 681.0, -1443.6, 1488.7),
		mk(0x200, 248.9, -820.2, 1443.9),
		mk(0x300, 709.2, -1381.7, 1585.3),
	}
	if _, detail, ok := g.PickOffset(noise, nil); ok {
		t.Fatalf("expected refusal without consensus, got ok=true detail=%s", detail)
	}
	// 单候选（单偏移单块）：live-go 允许 bestN=1
	if _, _, ok := g.PickOffset(noise[:1], nil); !ok {
		t.Fatal("single candidate should lock")
	}
}

// ---- 进度 ----

func TestProgress(t *testing.T) {
	setPlatform(&ryujinxPlatform{})
	defer setPlatform(nil)
	w := newProgressWatcher()
	if !w.ok {
		t.Skip("no reference table embedded or no save?")
	}
	ok, body, _ := w.snapshot()
	if !ok {
		t.Skip("progress not ok (need a Ryujinx save present)")
	}
	var out struct {
		Points []ProgressPoint   `json:"points"`
		Counts map[string][2]int `json:"counts"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(out.Points) != 1078 {
		t.Errorf("points = %d, want 1078", len(out.Points))
	}
	wantTotals := map[string]int{"shrine": 136, "tower": 15, "korok": 900, "memory": 23, "beast": 4}
	for k, n := range wantTotals {
		c := out.Counts[k]
		if c[1] != n {
			t.Errorf("total[%s] = %d, want %d", k, c[1], n)
		}
		if c[0] > c[1] {
			t.Errorf("done[%s] %d > total %d", k, c[0], c[1])
		}
	}
}
