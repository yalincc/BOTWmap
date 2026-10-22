package scan

import (
	"math"
	"testing"
)

// TestWorldCoordOK 固化 2026-10-21 实测事故的两条判据：
//  1. 参数表退化坐标 (107.7, 1.0, 0.0) 必须被拒（Valid() 挡不住它）；
//  2. 原点附近的退化坐标必须被拒；
//  3. 真实玩家坐标（含整数坐标）必须放行。
func TestWorldCoordOK(t *testing.T) {
	cases := []struct {
		name string
		v    Vec3
		want bool
	}{
		// —— 必须拒绝 ——
		{"参数表退化坐标 X轴 alt=1", Vec3{X: 107.7, Y: 1.0, Z: 0.0}, false},
		{"参数表退化坐标 Z轴 alt=1", Vec3{X: 0.0, Y: 1.0, Z: 107.7}, false},
		{"全零", Vec3{X: 0, Y: 0, Z: 0}, false},
		{"原点附近", Vec3{X: 0.1, Y: -0.1, Z: 0.0}, false},
		{"原点附近 负", Vec3{X: -0.0, Y: -0.0, Z: 0.0}, false},
		{"X=Z=0 且 alt=1", Vec3{X: 0, Y: 1, Z: 0}, false},
		{"NaN 高度", Vec3{X: 100.0, Y: float32(nan()), Z: 200.0}, false},
		{"退化坐标 (0, 0, Z) 骑乘标志位", Vec3{X: 0.0, Y: 0.0, Z: 500.0}, false},
		{"退化坐标 (X, 0, 0)", Vec3{X: 500.0, Y: 0.0, Z: 0.0}, false},
		{"NaN X", Vec3{X: float32(nan()), Y: 100.0, Z: 200.0}, false},
		{"Inf Z", Vec3{X: 100.0, Y: 100.0, Z: float32(posInf())}, false},
		{"X 越界", Vec3{X: 8000.0, Y: 100.0, Z: 200.0}, false},
		{"高度越界", Vec3{X: 100.0, Y: 6000.0, Z: 200.0}, false},

		// —— 必须放行 ——
		{"真实玩家", Vec3{X: 1662.5, Y: 116.7, Z: 1921.6}, true},
		{"真实玩家 整数坐标", Vec3{X: -717.0, Y: 128.8, Z: 618.0}, true},
		{"真实玩家 整数 X/Z 非1高度", Vec3{X: 100.0, Y: 116.0, Z: 200.0}, true},
		{"神庙本地尺度", Vec3{X: 5.0, Y: 12.0, Z: -8.0}, true},
	}
	for _, c := range cases {
		if got := c.v.WorldCoordOK(); got != c.want {
			t.Errorf("%s: WorldCoordOK() = %v, want %v (pos=%v)", c.name, got, c.want, c.v)
		}
	}
}

// TestValidStillLoose 确认 Valid() 没被改严（它是公开 API，别处还在用）。
// (107.7, 1.0, 0.0) 在 Valid() 下必须是 true —— 这正是它挡不住、需要 WorldCoordOK 的原因。
func TestValidStillLoose(t *testing.T) {
	if !(Vec3{X: 107.7, Y: 1.0, Z: 0.0}).Valid() {
		t.Fatal("Valid() 不该被改严：Valid 是宽松判据，严格判据是 WorldCoordOK")
	}
	if (Vec3{X: 0, Y: 0, Z: 0}).Valid() {
		t.Error("Valid() 应拒绝全零")
	}
}

func nan() float64    { return math.NaN() }
func posInf() float64 { return math.Inf(1) }

// TestIsQuatUnit 固化「单位模长四元数」判据。
//
// ★ 为什么不用「w ∈ (0.9, 1.0]」：
//   w = cos(θ/2)，玩家一转身 w 就离开 (0.9,1.0]（实测 w = -0.311）。
//   方向无关的判据是 w²+x²+y²+z² ≈ 1。
func TestIsQuatUnit(t *testing.T) {
	mk := func(w, x, y, z float32) []byte {
		b := make([]byte, 16)
		v := [4]float32{w, x, y, z}
		for i, f := range v {
			bits := math.Float32bits(f)
			b[i*4] = byte(bits)
			b[i*4+1] = byte(bits >> 8)
			b[i*4+2] = byte(bits >> 16)
			b[i*4+3] = byte(bits >> 24)
		}
		return b
	}
	// 玩家实测朝向（w=0, x=-0.0516, y=-0.9986, z=-0.0080）→ 模长 1
	if !IsQuatUnit(mk(0, -0.0516, -0.9986, -0.0080), 0) {
		t.Error("玩家实测四元数应判为单位模长")
	}
	// w=-0.311 的转身姿态：模长仍应为 1
	if !IsQuatUnit(mk(-0.311, 0.5487, 0.5487, 0.5487), 0) {
		t.Error("w=-0.311 的转身姿态模长仍≈1，应判为单位模长")
	}
	// 明显不是四元数
	if IsQuatUnit(mk(0, 0, 0, 0), 0) {
		t.Error("全零不应判为单位模长")
	}
}

// TestIsAxisMatrix 固化「轴矩阵要排除」这条判据。
//
// 实测踩过：引擎里的方向轴/骨骼基向量是 0,1,0 / -1,0,0 / 0,0,1 这种整齐排列，
// **完全满足** Rot3OK 的全部正交判据，但把它当「Actor 旋转矩阵」会锁到死数据。
func TestIsAxisMatrix(t *testing.T) {
	mk := func(v [9]float32) []byte {
		b := make([]byte, 36)
		for i, f := range v {
			bits := math.Float32bits(f)
			b[i*4] = byte(bits)
			b[i*4+1] = byte(bits >> 8)
			b[i*4+2] = byte(bits >> 16)
			b[i*4+3] = byte(bits >> 24)
		}
		return b
	}
	// 实测误判的那个轴矩阵
	axis := [9]float32{0, 1, 0, -1, 0, 0, 0, 0, 1}
	if !IsAxisMatrix(mk(axis), 0) {
		t.Error("0,1,0 / -1,0,0 / 0,0,1 应判为轴矩阵")
	}
	// 真实 Actor 的任意旋转矩阵：元素是"乱"的
	real := [9]float32{-0.25, -0.50, -1.00, 0.99, 0.00, 0.06, 0.00, 0.98, -0.20}
	if IsAxisMatrix(mk(real), 0) {
		t.Error("任意旋转矩阵不应判为轴矩阵")
	}
}

// TestRotWindowBytes 固化「读够 80 字节才能断言有没有矩阵」。
//
// 矩阵最远在 +0x2C，要读完 3x3 需要 +0x2C+36 = 80 字节。
// 读 64 字节时会漏看 +0x20 处的矩阵 → 把活着的 Actor 误判成死的。
func TestRotWindowBytes(t *testing.T) {
	if RotWindowBytes != 80 {
		t.Errorf("RotWindowBytes = %d, want 80", RotWindowBytes)
	}
	// TransformLoose 在 64 字节缓冲上必须拒绝（而不是"找不到矩阵"）
	small := make([]byte, 64)
	if _, _, ok := TransformLoose(small, 0); ok {
		t.Error("64 字节缓冲不足以判定矩阵，TransformLoose 不应返回 ok")
	}
}
