package main

import (
	"math"
	"testing"
)

func TestBE32(t *testing.T) {
	b := []byte{0x3F, 0x80, 0x00, 0x00}
	if got := beU32(b); got != 0x3F800000 {
		t.Fatalf("beU32 = %08X, want 3F800000", got)
	}
	if got := math.Float32frombits(beU32(b)); got != 1.0 {
		t.Fatalf("f32 = %v, want 1.0", got)
	}
	// 0x45BB8000 ≈ 5999.0（坐标量级）
	b2 := []byte{0x45, 0xBB, 0x80, 0x00}
	if got := math.Float32frombits(beU32(b2)); math.Abs(float64(got)-5999.0) > 1 {
		t.Fatalf("f32 = %v, want ~5999", got)
	}
}

func TestToF32BE(t *testing.T) {
	buf := []byte{0x3F, 0x80, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00}
	dst := make([]float32, 4)
	n := toF32BE(buf, dst)
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if dst[0] != 1.0 || dst[1] != 2.0 {
		t.Fatalf("decoded = %v, want [1 2]", dst[:2])
	}
}

func TestPosRangeOK(t *testing.T) {
	valid := [][3]float32{{1000, 50, 2000}, {-5000, 300, 2}, {6999, 4999, -6999}}
	for _, v := range valid {
		if !posRangeOK(v[0], v[1], v[2]) {
			t.Fatalf("expected valid: %v", v)
		}
	}
	invalid := [][3]float32{
		{0, 0, 0},          // 未初始化
		{1, 2, 3},          // 近零簇
		{1000, 50, 4.6e-44}, // denormal 垃圾
		{8000, 50, 2000},   // 超界
		{1000, 6000, 2000}, // 高度超界
		{1000, -1000, 2000},
	}
	for _, v := range invalid {
		if posRangeOK(v[0], v[1], v[2]) {
			t.Fatalf("expected invalid: %v", v)
		}
	}
}

func TestDecodePosF(t *testing.T) {
	// 内存序 (X=1000, alt=50, Z=2000) → 展示序 (gx=1000, gz=2000, alt=50)
	d := decodePosF([3]float32{1000, 50, 2000})
	if d == nil || d[0] != 1000 || d[1] != 2000 || d[2] != 50 {
		t.Fatalf("decodePosF = %v", d)
	}
	if decodePosF([3]float32{0, 0, 0}) != nil {
		t.Fatal("zero pos should be invalid")
	}
}

func TestRotOKF(t *testing.T) {
	// 单位矩阵（正交归一）
	m := make([]float32, 9)
	m[0], m[4], m[8] = 1, 1, 1
	if !rotOKF(m) {
		t.Fatal("identity matrix should pass")
	}
	// 非归一
	m2 := append([]float32{}, m...)
	m2[0] = 2
	if rotOKF(m2) {
		t.Fatal("non-normalized column should fail")
	}
	// 非正交
	m3 := append([]float32{}, m...)
	m3[3] = 1
	if rotOKF(m3) {
		t.Fatal("non-orthogonal matrix should fail")
	}
}
