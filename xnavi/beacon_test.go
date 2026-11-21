// beacon_test.go — 信标协议宿主层单元测试 + 文件兜底仿真。
// 内存信标读取需真实 Eden 进程，另行联机验证；此处覆盖纯函数与
// 不依赖进程的文件通道（XNAVI_SDMC_DIR 指向临时假目录）。

package main

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeBeaconBytes 按 beacon.h 布局编码 64B 并填 CRC（与 guest 侧一致）。
func makeBeaconBytes(b Beacon) []byte {
	buf := make([]byte, beaconSize)
	binary.LittleEndian.PutUint32(buf[0:4], beaconMagic)
	binary.LittleEndian.PutUint16(buf[4:6], b.Schema)
	binary.LittleEndian.PutUint16(buf[6:8], b.Build)
	binary.LittleEndian.PutUint32(buf[8:12], b.Seq)
	binary.LittleEndian.PutUint32(buf[12:16], math.Float32bits(b.X))
	binary.LittleEndian.PutUint32(buf[16:20], math.Float32bits(b.Y))
	binary.LittleEndian.PutUint32(buf[20:24], math.Float32bits(b.Z))
	binary.LittleEndian.PutUint32(buf[24:28], math.Float32bits(b.Yaw))
	binary.LittleEndian.PutUint32(buf[28:32], b.State)
	binary.LittleEndian.PutUint64(buf[32:40], b.GameClock)
	binary.LittleEndian.PutUint32(buf[40:44], b.ActorPtrHash)
	binary.LittleEndian.PutUint32(buf[44:48], crc32.ChecksumIEEE(buf[0:beaconCRCLen]))
	binary.LittleEndian.PutUint32(buf[48:52], math.Float32bits(b.HP))
	binary.LittleEndian.PutUint32(buf[52:56], math.Float32bits(b.Stamina))
	binary.LittleEndian.PutUint32(buf[56:60], b.Flags)
	return buf
}

func sampleBeacon() Beacon {
	return Beacon{Schema: beaconSchema, Build: 1, Seq: 100,
		X: 3757.7, Y: 342.8, Z: 2157.6, Yaw: 1.23, State: stOverworld,
		GameClock: 123456, ActorPtrHash: 0xDEAD, HP: 12, Stamina: 3, Flags: 1}
}

func TestParseBeaconRoundtrip(t *testing.T) {
	b := sampleBeacon()
	got, ok := ParseBeacon(makeBeaconBytes(b))
	if !ok {
		t.Fatal("valid beacon rejected")
	}
	if got.X != b.X || got.Y != b.Y || got.Z != b.Z || got.Seq != b.Seq ||
		got.State != b.State || got.Build != b.Build || got.GameClock != b.GameClock ||
		got.HP != b.HP || got.Flags != b.Flags {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, b)
	}
}

func TestParseBeaconRejects(t *testing.T) {
	valid := makeBeaconBytes(sampleBeacon())
	cases := map[string][]byte{
		"short":     valid[:40],
		"badmagic":  append([]byte{0, 0, 0, 0}, valid[4:]...),
		"badschema": func() []byte { c := append([]byte{}, valid...); binary.LittleEndian.PutUint16(c[4:6], 99); return c }(),
		"badcrc":    func() []byte { c := append([]byte{}, valid...); c[44] ^= 0xFF; return c }(),
	}
	for name, buf := range cases {
		if _, ok := ParseBeacon(buf); ok {
			t.Errorf("%s: expected reject, got accept", name)
		}
	}
}

func TestBeaconCoordOKByState(t *testing.T) {
	// 神庙本地坐标（近原点）：WorldCoordOK 会误杀，ShrineCoordOK 应放行。
	local := Beacon{State: stShrine, X: 0.5, Y: 2.0, Z: -3.0}
	if !beaconCoordOK(local) {
		t.Error("shrine local coord rejected")
	}
	if WorldCoordOK(local.X, local.Y, local.Z) {
		t.Error("precondition: WorldCoordOK should reject near-origin local coord")
	}
	// 大世界合法坐标。
	world := Beacon{State: stOverworld, X: 3757.7, Y: 342.8, Z: 2157.6}
	if !beaconCoordOK(world) {
		t.Error("world coord rejected")
	}
	// NaN 一律拒绝。
	nan := Beacon{State: stOverworld, X: float32(math.NaN()), Y: 100, Z: 100}
	if beaconCoordOK(nan) {
		t.Error("NaN accepted")
	}
}

func writePos(t *testing.T, dir string, b Beacon, ts float64) {
	t.Helper()
	j := posJSON{Magic: "XNAV", Schema: b.Schema, Build: b.Build, Seq: b.Seq,
		X: b.X, Y: b.Y, Z: b.Z, Yaw: b.Yaw, State: b.State, GameClock: b.GameClock,
		HP: b.HP, Stamina: b.Stamina, Flags: b.Flags, TS: ts}
	data, _ := json.Marshal(j)
	if err := os.WriteFile(filepath.Join(dir, "pos.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTryBeaconTiersFileFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XNAVI_SDMC_DIR", dir)
	// 新鲜 pos.json → tier2 命中（h=0 使 tier1 RAM 干净 miss）。
	writePos(t, dir, sampleBeacon(), float64(time.Now().UnixNano())/1e9)
	if !tryBeaconTiers(0, &edenPlatform{}, false) {
		t.Fatal("fresh pos.json should hit tier2")
	}
	state.mu.RLock()
	src, ok := state.source, state.ok
	state.mu.RUnlock()
	if !ok || src != "beacon-json" {
		t.Fatalf("state: ok=%v source=%q want beacon-json", ok, src)
	}
	// 陈旧 pos.json（mtime 远超 3s）→ tier2 miss → 落 tier3（返回 false）。
	writePos(t, dir, sampleBeacon(), float64(time.Now().UnixNano())/1e9)
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(filepath.Join(dir, "pos.json"), old, old); err != nil {
		t.Fatal(err)
	}
	if tryBeaconTiers(0, &edenPlatform{}, false) {
		t.Error("stale pos.json should not hit tier2")
	}
}

// 游戏未就绪时不启动"缺模组"计时，避免启动阶段误判；游戏就绪后才开始计时。
func TestBeaconMissingDetectionNeedsGameReady(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XNAVI_SDMC_DIR", dir)
	beaconProbeSince = time.Time{}
	beaconMissingWarned = false

	// 游戏未就绪：不应开始计时。
	tryBeaconTiers(0, &edenPlatform{}, false)
	if !beaconProbeSince.IsZero() {
		t.Error("probe timer must not start while game is not ready")
	}
	// 游戏就绪：开始计时（尚未到告警阈值，不应告警）。
	tryBeaconTiers(0, &edenPlatform{}, true)
	if beaconProbeSince.IsZero() {
		t.Error("probe timer should start once game is ready")
	}
	if beaconMissingWarned {
		t.Error("must not warn before the grace period elapses")
	}
}

// 重置信标侧状态跟踪（测试之间互不干扰）。
func resetBeaconTrack() {
	beaconWorldPos = [3]float32{}
	beaconHasWorld = false
	beaconInTrial = false
	beaconLocalPos = [3]float32{}
	beaconHasLocal = false
	beaconLastMove = time.Time{}
	beaconStillTold = false
	beaconNoAnchor = false
}

// 神庙/试炼场是独立场景，游戏给出的是场内局部坐标（近原点）。宿主必须把它们
// 锚定到最后已知的世界坐标（入口），而不是当作世界坐标发布 —— 否则红点会跳到
// 地图原点并在场内持续漂移。
func TestPublishBeaconHoldsEntranceInTrial(t *testing.T) {
	resetBeaconTrack()

	// 大世界：正常跟随。
	publishBeacon(Beacon{X: 3757.7, Y: 342.8, Z: 2157.6, State: stOverworld}, "beacon")
	state.mu.RLock()
	gx, gy, gz, src := state.gx, state.gy, state.gz, state.source
	state.mu.RUnlock()
	if src != "beacon" || gx != 3757.7 || gy != 342.8 || gz != 2157.6 {
		t.Fatalf("world publish: src=%q pos=(%.1f,%.1f,%.1f)", src, gx, gy, gz)
	}

	// 进入试炼场：坐标变成场内局部值，state 声明为神庙。
	publishBeacon(Beacon{X: 0, Y: 20, Z: 68, State: stShrine}, "beacon")
	state.mu.RLock()
	gx, gy, gz, src = state.gx, state.gy, state.gz, state.source
	state.mu.RUnlock()
	if src != "beacon-shrine" {
		t.Fatalf("trial publish source = %q, want beacon-shrine", src)
	}
	if gx != 3757.7 || gy != 342.8 || gz != 2157.6 {
		t.Fatalf("trial must hold entrance (3757.7,342.8,2157.6), got (%.1f,%.1f,%.1f)", gx, gy, gz)
	}

	// 场内移动：局部坐标变化不得带动红点。
	publishBeacon(Beacon{X: 40, Y: 22, Z: 90, State: stShrine}, "beacon")
	state.mu.RLock()
	gx, gy, gz = state.gx, state.gy, state.gz
	state.mu.RUnlock()
	if gx != 3757.7 || gy != 342.8 || gz != 2157.6 {
		t.Fatalf("trial movement must not move the dot, got (%.1f,%.1f,%.1f)", gx, gy, gz)
	}

	// 回到大世界：恢复实时跟随。
	publishBeacon(Beacon{X: 3760.0, Y: 344.0, Z: 2160.0, State: stOverworld}, "beacon")
	state.mu.RLock()
	gx, gy, gz, src = state.gx, state.gy, state.gz, state.source
	state.mu.RUnlock()
	if src != "beacon" || gx != 3760.0 || gy != 344.0 || gz != 2160.0 {
		t.Fatalf("back to world: src=%q pos=(%.1f,%.1f,%.1f)", src, gx, gy, gz)
	}
}

// 没有任何大世界坐标时，绝不能把场内局部坐标发布出去（否则红点跳到原点）。
func TestPublishBeaconTrialWithoutAnchor(t *testing.T) {
	resetBeaconTrack()
	publishBeacon(Beacon{X: 0, Y: 20, Z: 68, State: stShrine}, "beacon")
	state.mu.RLock()
	ok, gx, gz, src := state.ok, state.gx, state.gz, state.source
	state.mu.RUnlock()
	if ok && gx == 0 && gz == 68 {
		t.Fatalf("trial-local coord published without anchor: (%.1f,%.1f) src=%q", gx, gz, src)
	}
}
