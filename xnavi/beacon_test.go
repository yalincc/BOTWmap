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
	if !tryBeaconTiers(0, &edenPlatform{}) {
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
	if tryBeaconTiers(0, &edenPlatform{}) {
		t.Error("stale pos.json should not hit tier2")
	}
}
