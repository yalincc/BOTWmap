package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// TestIntegration 端到端验收：真实游戏进程 + 真实存档 + 全部 HTTP 端点。
// 需要 Savage Wind 正在运行且已进入游戏；未运行时自动跳过。
func TestIntegration(t *testing.T) {
	var e processEntry32W
	t.Logf("sizeof(PROCESSENTRY32W) = %d", unsafe.Sizeof(e))

	if findProcessPID("savagewind.exe") == 0 {
		t.Skip("Savage Wind 未运行，跳过集成验收")
	}

	loc = newLocator("savagewind.exe")
	for i := 0; i < 12; i++ {
		loc.tick()
		time.Sleep(100 * time.Millisecond)
	}
	st := loc.snapshot()
	t.Logf("定位: ok=%v verified=%v pid=%d offset=0x%X  X=%.3f alt=%.3f Z=%.3f note=%q",
		st.OK, st.Verified, st.PID, st.Offset, st.GX, st.GY, st.GZ, st.Note)
	if !st.OK || !st.Verified {
		t.Fatal("固定偏移定位失败")
	}
	if st.GX == 0 && st.GZ == 0 {
		t.Fatal("坐标为零")
	}

	saves.refresh()
	ok, body, gen := saves.snapshot()
	t.Logf("进度: ok=%v bytes=%d gen=%s save=%s", ok, len(body), gen, currentSavePath())
	if !ok {
		t.Fatal("存档解析失败")
	}
	var pj struct {
		Points []progressPoint     `json:"points"`
		Counts map[string][2]int   `json:"counts"`
	}
	if json.Unmarshal(body, &pj) != nil || len(pj.Points) == 0 {
		t.Fatal("/progress 缺 points 数组（网页会判定格式不符）")
	}
	t.Logf("参考点 %d 个，统计类别 %d 项", len(pj.Points), len(pj.Counts))

	srv := httptest.NewServer(&apiServer{})
	defer srv.Close()

	get := func(path string) (int, []byte) {
		r, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b
	}

	code, b := get("/pos?who=web")
	var pos map[string]any
	_ = json.Unmarshal(b, &pos)
	t.Logf("/pos -> %d app=%v ok=%v gx=%v mx=%v progressGen=%v",
		code, pos["app"], pos["ok"], pos["gx"], pos["mx"], pos["progressGen"])
	if code != 200 || pos["app"] != "xnavi" || pos["ok"] != true {
		t.Fatal("/pos 契约不符（网页端口发现会失联）")
	}

	code, b = get("/progress")
	if code != 200 || !strings.Contains(string(b), `"points"`) {
		t.Fatalf("/progress 异常: %d %s", code, b[:min(80, len(b))])
	}
	t.Logf("/progress -> %d, %d bytes", code, len(b))

	code, b = get("/save")
	t.Logf("/save -> %d, %d bytes, head=%x", code, len(b), b[:min(8, len(b))])
	if code != 200 || len(b) < 1000000 {
		t.Fatal("/save 未返回完整 game_data.sav")
	}

	code, b = get("/status")
	if code != 200 || !strings.Contains(string(b), "xnavi-pc") {
		t.Fatal("/status 状态页异常")
	}
	t.Logf("/status -> %d, %d bytes", code, len(b))

	code, b = get("/botw/")
	t.Logf("/botw/ -> %d, %d bytes", code, len(b))

	code, b = get("/config")
	t.Logf("/config -> %d", code)
	code, b = get("/rescan")
	t.Logf("/rescan -> %d %s", code, strings.TrimSpace(string(b)))
}

func TestShrineScaleAndHold(t *testing.T) {
	// 测试 isShrineScale 判定
	shrineInside := [3]float32{12.5, -15.0, 30.2}
	if !isShrineScale(shrineInside) {
		t.Fatalf("神庙内部坐标识别失败: %+v", shrineInside)
	}

	worldPos := [3]float32{850.0, 131.0, 200.0}
	if isShrineScale(worldPos) {
		t.Fatalf("大世界坐标被误判为神庙内部: %+v", worldPos)
	}

	// 测试状态机构建
	loc := newLocator("dummy")
	loc.hasPrev = true
	loc.prevPos = worldPos

	// 突变进入神庙尺度且距离 > 100m
	if dist3(shrineInside, loc.prevPos) <= 100.0 {
		t.Fatal("大世界到神庙突变距离计算异常")
	}
}

func TestExactCoordHit3D(t *testing.T) {
	// 构造一段模拟内存，并在中间埋入一个三维坐标
	buf := make([]byte, 1024)
	targetX := float32(888.473)
	targetAlt := float32(131.277)
	targetZ := float32(156.709)

	offset := 256
	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(targetX))
	binary.LittleEndian.PutUint32(buf[offset+4:], math.Float32bits(targetAlt))
	binary.LittleEndian.PutUint32(buf[offset+8:], math.Float32bits(targetZ))

	// 输入带有 F10 一位小数四舍五入的屏幕坐标 (888.5, 131.3, 156.7)
	hit, found := exactCoordHit3D(buf, 888.5, 131.3, 156.7)
	if !found || hit != uintptr(offset) {
		t.Fatalf("手动校准三维定向搜索匹配失败: found=%v hit=%d, want=%d", found, hit, offset)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
