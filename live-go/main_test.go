package main

import (
	"encoding/binary"
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

func TestDecodePos(t *testing.T) {
	// (X=1794.6, alt=220.3, Z=1046.1) → 返回 (gx, gz, alt)
	buf := append(append(append([]byte{}, f32(1794.6)...), f32(220.3)...), f32(1046.1)...)
	v := decodePos(buf)
	if v == nil {
		t.Fatal("expected valid pos")
	}
	if math.Abs(float64(v[0]-1794.6)) > 0.01 || math.Abs(float64(v[1]-1046.1)) > 0.01 || math.Abs(float64(v[2]-220.3)) > 0.01 {
		t.Fatalf("decode mismatch: %v", v)
	}
	// 全零 → 无效
	zero := make([]byte, 12)
	if decodePos(zero) != nil {
		t.Fatal("expected nil for zero pos")
	}
}

func TestScanBytes(t *testing.T) {
	refs := [][3]float32{{100.5, 300, -200.25}}
	// 构造 4KB 缓冲，填充随机值，在固定位置写入目标三元组
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	// 目标位置：offset 1000 起 (x=100.5, y=300, z=-200.25)
	copy(buf[1000:], append(append(append([]byte{}, f32(100.5)...), f32(300)...), f32(-200.25)...))
	var hits []Hit
	scanBytes(buf, 0x1000, refs, 120, &hits)
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
	// 窗口外坐标不命中
	copy(buf[2000:], append(append(append([]byte{}, f32(9000)...), f32(300)...), f32(-200.25)...))
	var hits2 []Hit
	scanBytes(buf, 0x1000, refs, 120, &hits2)
	for _, h := range hits2 {
		if h.Addr == 0x1000+2000 {
			t.Fatal("out-of-window coord should not hit")
		}
	}
}

func TestRotOKBytes(t *testing.T) {
	buf := make([]byte, 128)
	// 在 offset 16 处放一个正交矩阵（旋转 90° 绕 Z）
	// col1=(0,-1,0) col2=(1,0,0) col3=(0,0,1)
	mat := []float32{
		0, -1, 0,
		1, 0, 0,
		0, 0, 1,
	}
	for i, v := range mat {
		copy(buf[16+i*4:], f32(v))
	}
	if !rotOKBytes(buf) {
		t.Fatal("expected orthogonal matrix detected")
	}
	// 非正交矩阵
	buf2 := make([]byte, 128)
	bad := []float32{1, 2, 3, 4, 5, 6, 7, 8, 9}
	for i, v := range bad {
		copy(buf2[16+i*4:], f32(v))
	}
	if rotOKBytes(buf2) {
		t.Fatal("non-orthogonal matrix should not pass")
	}
}

func TestParseSaveReal(t *testing.T) {
	// 真实存档对照 Python 基准：playtime=223431, pos=(1794.6, 220.3, 1046.1)
	root := filepath.Join(os.Getenv("APPDATA"), "Ryujinx", "bis", "user", "save")
	files := findSaveFiles(root)
	if len(files) == 0 {
		t.Skip("no save files found")
	}
	anchors := readSaveAnchors()
	if len(anchors) == 0 {
		t.Fatal("no usable anchors")
	}
	primary := anchors[0]
	if primary.Playtime != 223431 {
		t.Errorf("playtime mismatch: got %d want 223431", primary.Playtime)
	}
	if math.Abs(float64(primary.Pos[0]-1794.6)) > 0.5 || math.Abs(float64(primary.Pos[1]-220.3)) > 0.5 || math.Abs(float64(primary.Pos[2]-1046.1)) > 0.5 {
		t.Errorf("pos mismatch: got %v want (1794.6, 220.3, 1046.1)", primary.Pos)
	}
	// korok counter 对照
	entries, a := parseSave(primary.Path)
	if a == nil || entries[korokCounterHash] != 75 {
		t.Errorf("korok counter mismatch: got %d want 75", entries[korokCounterHash])
	}
}
