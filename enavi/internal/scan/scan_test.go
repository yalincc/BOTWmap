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
