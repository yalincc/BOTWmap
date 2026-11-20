// eden_motion_test.go — 候选运动学甄别排序测试。
// 背景（2026-11-18 真机）：Eden 动画/参数表能造出 Run=33 的长链，
// 真玩家 RingBuffer 被 ±300 哨兵污染 Run 常只有 2~7，纯 Run 排序
// 会锁到静态组导致"主角未确认走动"无限提示。

package main

import "testing"

func TestEdenMotionPredicates(t *testing.T) {
	var zero edenMotion
	if zero.changed() || zero.sustained() {
		t.Fatal("zero motion must be neither changed nor sustained")
	}
	// 单次小位移：changed 但非 sustained
	one := edenMotion{maxD: 0.1, changes: 1}
	if !one.changed() || one.sustained() {
		t.Fatal("single small step: changed=true sustained=false expected")
	}
	// 两次变化但累计位移不足：非 sustained
	two := edenMotion{maxD: 0.2, changes: 2}
	if two.sustained() {
		t.Fatal("two changes with maxD<0.3 must not be sustained")
	}
	// 持续平移：sustained
	walk := edenMotion{maxD: 1.5, changes: 3}
	if !walk.changed() || !walk.sustained() {
		t.Fatal("walking motion must be changed and sustained")
	}
}

func TestEdenMotionKeyMatchesGrouping(t *testing.T) {
	// 0.1m 网格：组内微小差异必须落到同一键
	a := edenMotionKey([3]float32{100.01, 200.01, 300.01})
	b := edenMotionKey([3]float32{100.04, 200.04, 300.04})
	if a != b {
		t.Fatalf("same 0.1m cell must share key: %s vs %s", a, b)
	}
	c := edenMotionKey([3]float32{100.1, 200.0, 300.0})
	if a == c {
		t.Fatal("different cells must have different keys")
	}
}

// TestGroupEdenHitsMotionRanking 复现真机故障场景：
// 静态组长 Run（动画表）vs 真身短 Run（哨兵污染）——有运动学证据时真身必须排第一。
func TestGroupEdenHitsMotionRanking(t *testing.T) {
	playerP := [3]float32{-785.2, 198.6, 450.2}
	staticP := [3]float32{300.0, -300.0, -300.0}
	hits := []EdenHit{
		{Addr: 0x1000, P: staticP, Run: 33, Quat: true, Rot: true, Dist: 1200},
		{Addr: 0x2000, P: playerP, Run: 2, Quat: true, Rot: true, Dist: 5},
	}
	// 无运动学证据：保持旧行为（长 Run 静态组在前）——Ryujinx 兼容路径
	old := GroupEdenHits(hits, nil)
	if len(old) != 2 || old[0].Addr != 0x1000 {
		t.Fatalf("legacy ranking must keep long-Run group first, got %+v", old)
	}
	// 有运动学证据：真身持续平移，必须反超静态组
	motion := map[string]edenMotion{
		edenMotionKey(playerP): {first: playerP, last: playerP, maxD: 1.2, changes: 3},
		edenMotionKey(staticP): {first: staticP, last: staticP},
	}
	ranked := GroupEdenHits(hits, motion)
	if len(ranked) != 2 || ranked[0].Addr != 0x2000 {
		t.Fatalf("moving player must outrank static long-Run group, got %+v", ranked)
	}
	// 双方都持续平移时退回 Run 排序
	motionBoth := map[string]edenMotion{
		edenMotionKey(playerP): {maxD: 1.2, changes: 3},
		edenMotionKey(staticP): {maxD: 2.0, changes: 4},
	}
	ranked2 := GroupEdenHits(hits, motionBoth)
	if ranked2[0].Addr != 0x1000 {
		t.Fatalf("both sustained: fall back to Run ranking, got %+v", ranked2)
	}
	// 单次变化 > 无变化（即便 Run 更短）
	motionOnce := map[string]edenMotion{
		edenMotionKey(playerP): {maxD: 0.1, changes: 1},
		edenMotionKey(staticP): {},
	}
	ranked3 := GroupEdenHits(hits, motionOnce)
	if ranked3[0].Addr != 0x2000 {
		t.Fatalf("single-change group must outrank frozen group, got %+v", ranked3)
	}
}

// TestGroupEdenHitsMotionNilCompat 空运动学 map 等价于 nil（不改变旧排序）。
func TestGroupEdenHitsMotionNilCompat(t *testing.T) {
	hits := []EdenHit{
		{Addr: 0x1000, P: [3]float32{300, -300, -300}, Run: 33, Quat: true},
		{Addr: 0x2000, P: [3]float32{-785.2, 198.6, 450.2}, Run: 2, Quat: true},
	}
	a := GroupEdenHits(hits, nil)
	b := GroupEdenHits(hits, map[string]edenMotion{})
	if len(a) != len(b) || a[0].Addr != b[0].Addr {
		t.Fatal("empty motion map must behave like nil")
	}
}