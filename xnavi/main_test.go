// xnavi 单元测试：坐标校验、扫描命中、旋转矩阵、存档端序自适应、进度计算。
// 平台无关部分直接测；平台相关部分用 ryujinxPlatform{} 实例（存档路径真实）。

package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// ---- 进度 ----

// ---- 存档空档过滤（台账 #25）+ 传送目的地匹配（#28）+ 动态参考中心 ----

// makeSaveWith 构造最小有效 game_data.sav：头版本 + PLAYER_POSITION 三连块
// + PlayTime + 若干额外条目（如神庙完成哈希）。
func makeSaveWith(t *testing.T, pos [3]float32, playtime uint32, extra map[uint32]uint32) []byte {
	t.Helper()
	n := 6 + len(extra) // 3 组坐标 + PlayTime + extra
	buf := make([]byte, 0x0C+n*8)
	put := func(o int, v uint32) { binary.LittleEndian.PutUint32(buf[o:], v) }
	put(0, 0x471E)
	put(0x0C, playerPositionHash)
	put(0x10, math.Float32bits(pos[0]))
	put(0x14, playerPositionHash)
	put(0x18, math.Float32bits(pos[1]))
	put(0x1C, playerPositionHash)
	put(0x20, math.Float32bits(pos[2]))
	put(0x24, playtimeHash)
	put(0x28, playtime)
	o := 0x2C
	for h, v := range extra {
		put(o, h)
		put(o+4, v)
		o += 8
	}
	return buf
}

func TestSaveAnchorsSkipsEmptySlot(t *testing.T) {
	hashes := shrineDoneHashes()
	if len(hashes) == 0 {
		t.Fatal("embedded progress table missing shrine hashes")
	}
	root := t.TempDir()
	write := func(slot string, data []byte, mod time.Time) {
		dir := filepath.Join(root, botwTitleID, "0000000000000008", slot)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "game_data.sav")
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	// slot 6：空档（Playtime=0），mtime 最新 —— 旧实现会取到它（初始台地锚点）
	write("6", makeSaveWith(t, [3]float32{-1026.5, 252.3, 1803.2}, 0, nil), now)
	// slot 1：正常档（有进度 + 神庙完成 1），mtime 更旧
	write("1", makeSaveWith(t, [3]float32{4286.6, 236.5, -2717.3}, 223696,
		map[uint32]uint32{hashes[0]: 1}), now.Add(-2*time.Hour))

	setEdenSaveDirOverride(root)
	defer setEdenSaveDirOverride("")
	got := currentGame.SaveAnchors(&edenPlatform{})
	if len(got) == 0 {
		t.Fatal("expected an anchor")
	}
	if math.Abs(float64(got[0].Pos[0]-4286.6)) > 0.01 {
		t.Fatalf("picked empty slot, pos=%v playtime=%d", got[0].Pos, got[0].Playtime)
	}
	if got[0].Playtime != 223696 {
		t.Fatalf("playtime mismatch: %d", got[0].Playtime)
	}

	// 全部是空档 → 仍须返回一个（不改变老行为下限，只是排优先级）
	root2 := t.TempDir()
	dir := filepath.Join(root2, botwTitleID, "0000000000000008", "7")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game_data.sav"),
		makeSaveWith(t, [3]float32{-1026.5, 252.3, 1803.2}, 0, nil), 0644); err != nil {
		t.Fatal(err)
	}
	setEdenSaveDirOverride(root2)
	if got := currentGame.SaveAnchors(&edenPlatform{}); len(got) != 1 {
		t.Fatalf("empty-only fallback should still return the newest slot, got %d", len(got))
	}
}

func TestMatchShrineTowerAndGate(t *testing.T) {
	// 塔/神庙参考点存的是地图像素：mx=2*gx+12000, my=2*gz+10000
	tower := ProgressPoint{T: "tower", Cn: "森林之塔", X: 2*4000.0 + 12000, Y: 2*(-3000.0) + 10000}
	gate := ProgressPoint{T: "shrine", Cn: "兹纳·卡伊神庙", X: 2*4500.0 + 12000, Y: 2*(-3100.0) + 10000}
	prev := progress
	progress = &progressWatcher{ok: true, refs: []ProgressPoint{tower, gate}}
	defer func() { progress = prev }()

	if p := matchShrine(4000, 200, -3000); p == nil || p.T != "tower" || p.Cn != "森林之塔" {
		t.Fatalf("tower match failed: %+v", p)
	}
	if p := matchShrine(4500, 200, -3100); p == nil || p.T != "shrine" || p.Cn != "兹纳·卡伊神庙" {
		t.Fatalf("shrine gate match failed: %+v", p)
	}
	// 半径外（>40m 像素半径）不应命中
	if p := matchShrine(4000+200, 200, -3000+200); p != nil {
		t.Fatalf("out-of-radius should not match: %+v", p)
	}
}

func TestEdenRefCenter(t *testing.T) {
	EdenClearRefCenter()
	defer EdenClearRefCenter()
	if _, ok := edenRefCenter(); ok {
		t.Fatal("should start empty")
	}
	// 非法坐标（伪槽 Y=2000 会被 gameValidTriple 拒？这里用原点簇）不得写入
	edenSetRefCenter([3]float32{0, 0, 0})
	if _, ok := edenRefCenter(); ok {
		t.Fatal("invalid coord must be rejected")
	}
	edenSetRefCenter([3]float32{4286.6, 236.5, -2717.3})
	if pos, ok := edenRefCenter(); !ok || math.Abs(float64(pos[0]-4286.6)) > 0.01 {
		t.Fatalf("set/read failed: %v %v", pos, ok)
	}
	// IfEmpty 在已有中心时不得覆盖（首次锁定语义）
	if edenSetRefCenterIfEmpty([3]float32{100, 200, 300}) {
		t.Fatal("IfEmpty must not overwrite existing center")
	}
	EdenClearRefCenter()
	if edenSetRefCenterIfEmpty([3]float32{100, 200, 300}) != true {
		t.Fatal("IfEmpty should set when empty")
	}
}

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

func TestWorldCoordOKAndRunLength(t *testing.T) {
	// 1. 合法坐标
	if !WorldCoordOK(1794.6, 220.3, 1046.1) {
		t.Fatal("expected valid world coord")
	}
	// 2. 轴退化坐标（参数表特征：X≈0 / alt≈1）必须被拒绝
	if WorldCoordOK(0.0, 1.0, 1500.0) {
		t.Fatal("axis degenerate coord (X=0, alt=1) should be rejected")
	}
	// 2b. (0, 0, Z) 或 (X, 0, 0) 退化标志位向量（骑乘/盾滑槽特征）必须被拒绝
	if WorldCoordOK(0.0, 0.0, 1500.0) {
		t.Fatal("(0, 0, Z) coord should be rejected")
	}
	if WorldCoordOK(1500.0, 0.0, 0.0) {
		t.Fatal("(X, 0, 0) coord should be rejected")
	}
	if WorldCoordOK(0.0, 1e-40, 1500.0) {
		t.Fatal("(0, subnormal, Z) coord should be rejected")
	}
	// 3. 原点噪声退化必须被拒绝
	if WorldCoordOK(0.1, 0.2, 0.1) {
		t.Fatal("near-origin noise coord should be rejected")
	}
	// 4. NaN / 越界必须被拒绝
	if WorldCoordOK(float32(math.NaN()), 100, 100) {
		t.Fatal("NaN coord should be rejected")
	}
	if WorldCoordOK(8000.0, 100, 100) {
		t.Fatal("out of bounds coord should be rejected")
	}

	// 5. RunLength 门禁自测：构造两个连续 0x40 的记录
	buf := make([]byte, 256)
	putRec := func(off int, x, y, z float32, qx, qy, qz, qw float32) {
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(x))
		binary.LittleEndian.PutUint32(buf[off+4:], math.Float32bits(y))
		binary.LittleEndian.PutUint32(buf[off+8:], math.Float32bits(z))
		binary.LittleEndian.PutUint32(buf[off+12:], math.Float32bits(qx))
		binary.LittleEndian.PutUint32(buf[off+16:], math.Float32bits(qy))
		binary.LittleEndian.PutUint32(buf[off+20:], math.Float32bits(qz))
		binary.LittleEndian.PutUint32(buf[off+24:], math.Float32bits(qw))
	}
	// 写入记录 0 与记录 1（相隔 0x40 = 64 字节）
	putRec(0, 100.0, 200.0, 300.0, 0, 0, 0, 1.0)
	putRec(64, 100.5, 200.0, 300.5, 0, 0, 0, 1.0)

	run := RunLength(buf, 0)
	if run < 2 {
		t.Fatalf("expected RunLength >= 2, got %d", run)
	}
}

func TestProcessAnchoredSaveResolution(t *testing.T) {
	// 1. parseCemuMlcPath 测试
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "settings.xml")
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<content>
    <mlc_path>D:\Emulators\Cemu\custom_mlc</mlc_path>
</content>`
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}
	mlc := parseCemuMlcPath(xmlPath)
	if mlc != filepath.Clean(`D:\Emulators\Cemu\custom_mlc`) {
		t.Fatalf("parseCemuMlcPath mismatch: got %q", mlc)
	}

	// 2. findSaveFiles 支持单文件直接传入
	singleSave := filepath.Join(tmpDir, "game_data.sav")
	if err := os.WriteFile(singleSave, makeSave(t, true), 0644); err != nil {
		t.Fatal(err)
	}
	found := findSaveFiles(singleSave, []string{"game_data.sav"})
	if len(found) != 1 || found[0] != singleSave {
		t.Fatalf("findSaveFiles single file mismatch: got %v", found)
	}

	// 3. P0 手动覆盖优先测试
	setCemuSaveDirOverride(tmpDir)
	defer setCemuSaveDirOverride("")
	cp := &cemuPlatform{}
	roots := cp.SaveRoots()
	if len(roots) != 1 || roots[0] != tmpDir {
		t.Fatalf("Cemu P0 override failed: got %v", roots)
	}

	setRyujinxSaveDirOverride(tmpDir)
	defer setRyujinxSaveDirOverride("")
	rp := &ryujinxPlatform{}
	rRoots := rp.SaveRoots()
	if len(rRoots) != 1 || rRoots[0] != tmpDir {
		t.Fatalf("Ryujinx P0 override failed: got %v", rRoots)
	}

	setEdenSaveDirOverride(tmpDir)
	defer setEdenSaveDirOverride("")
	ep := &edenPlatform{}
	eRoots := ep.SaveRoots()
	if len(eRoots) != 1 || eRoots[0] != tmpDir {
		t.Fatalf("Eden P0 override failed: got %v", eRoots)
	}

	// 4. saveSourceTag 判定测试
	tagOverride := saveSourceTag(cp, filepath.Join(tmpDir, "game_data.sav"))
	if tagOverride != "手动指定" {
		t.Fatalf("expected '手动指定', got %q", tagOverride)
	}

	setCemuSaveDirOverride("")
	tagAppData := saveSourceTag(cp, filepath.Join(os.Getenv("APPDATA"), "Cemu", "mlc01", "usr", "save", "00050000", "game_data.sav"))
	if tagAppData != "系统标准: AppData" {
		t.Fatalf("expected '系统标准: AppData', got %q", tagAppData)
	}
}

func TestSaveDownloadEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	saveFile := filepath.Join(tmpDir, "game_data.sav")
	saveBytes := makeSave(t, true)
	if err := os.WriteFile(saveFile, saveBytes, 0644); err != nil {
		t.Fatal(err)
	}

	progress = &progressWatcher{save: saveFile}
	defer func() { progress = nil }()

	srv := &server{}
	req := httptest.NewRequest("GET", "/save", nil)
	rr := httptest.NewRecorder()

	srv.handleSaveDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("expected application/octet-stream, got %s", ct)
	}
	if len(rr.Body.Bytes()) != len(saveBytes) {
		t.Fatalf("expected %d bytes, got %d", len(saveBytes), len(rr.Body.Bytes()))
	}
}
