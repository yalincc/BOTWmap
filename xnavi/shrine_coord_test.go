// shrine_coord_test.go — 神庙本地尺度坐标校验测试。
// 背景（2026-11-19 真机）：进庙后读空 20s 掉锁——试炼场本地坐标可能
// 位于原点/轴上，大世界校验的近零剔除把合法位置误杀。

package main

import (
	"math"
	"testing"
)

func TestShrineCoordOK(t *testing.T) {
	// 试炼场内合法位置（含大世界校验会误杀的近零/轴上坐标）
	valid := [][3]float32{
		{0.5, 2.0, -3.0}, // 本地原点附近：WorldCoordOK 拒绝，神庙必须接受
		{0.0, 5.0, 12.0}, // 轴上（x=0）
		{12.0, 5.0, 0.0}, // 轴上（z=0）
		{0.0, 0.0, 25.0}, // (0,0,Z)：庙内沿轴走廊合法
		{-120.0, 30.0, 80.0},
		{299.0, 49.0, -299.0},
	}
	for _, p := range valid {
		if !ShrineCoordOK(p[0], p[1], p[2]) {
			t.Fatalf("shrine-local coord should be accepted: %v", p)
		}
	}
	// 精确零向量（演员槽被清零）必须拒绝
	if ShrineCoordOK(0, 0, 0) {
		t.Fatal("exact zero vector must be rejected")
	}
	// NaN / Inf 必须拒绝
	if ShrineCoordOK(float32(math.NaN()), 1, 1) {
		t.Fatal("NaN must be rejected")
	}
	if ShrineCoordOK(float32(math.Inf(1)), 1, 1) {
		t.Fatal("Inf must be rejected")
	}
	// 对照：同样的近零坐标大世界校验确实拒绝（证明误杀存在且已被旁路）
	if WorldCoordOK(0.5, 2.0, -3.0) {
		t.Fatal("sanity: WorldCoordOK should reject near-origin (shrine bypass needed)")
	}
}
