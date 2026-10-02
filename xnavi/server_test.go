package main

// 接口契约回归测试（协议 v1 §3，2026-10-01 定稿）：
// /pos 与 /progress 的字段集与类型是 xnavi ↔ 两个地图站的跨程序契约，
// 任何一端改动都应先让本文件变红——杜绝"对接对不上半天排查"复发。
// 进度 watcher 的 body 以注入方式构造（HTTP 边界契约锁定；
// 解析正确性由真机冒烟覆盖：BOTW 104/136）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func topLevelKeys(t *testing.T, body []byte) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("response is not a JSON object: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertHasKeys(t *testing.T, body []byte, want ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, k := range topLevelKeys(t, body) {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("contract missing field %q in response: %s", k, body)
		}
	}
}

func TestContractPosFieldSet(t *testing.T) {
	origGame, origProgress := currentGame, progress
	defer func() { currentGame, progress = origGame, origProgress }()

	currentGame = &gameBotw{}
	state.mu.Lock()
	state.ok = true
	state.gx, state.gy, state.gz = 709.2, -1381.7, 1585.3
	state.mx, state.my = 1381.7, 709.2
	state.verified = true
	state.mu.Unlock()
	progress = &progressWatcher{ok: true, body: []byte(`{}`), gen: "gen-botw"}

	s := &server{}
	rec := httptest.NewRecorder()
	s.handlePos(rec, httptest.NewRequest(http.MethodGet, "/pos", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/pos status = %d", rec.Code)
	}
	// 协议 §2.1：ok/mx/my/gx/gy/gz/verified/progressGen/target/game 必备（layer/age/copies/source 为既有扩展）
	assertHasKeys(t, rec.Body.Bytes(),
		"ok", "mx", "my", "gx", "gy", "gz", "verified", "progressGen", "target", "game")

	// xnavi 只服务 BOTW：game=botw 且 gen 来自 BOTW watcher
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m["game"] != "botw" {
		t.Errorf("game = %v, want botw", m["game"])
	}
	if m["progressGen"] != "gen-botw" {
		t.Errorf("progressGen = %v, want gen-botw", m["progressGen"])
	}
}

func TestContractProgressBotwShape(t *testing.T) {
	origGame, origProgress := currentGame, progress
	defer func() { currentGame, progress = origGame, origProgress }()

	// BOTW 口径（协议 §2.2）：ok/points/counts/generated（+source/spot_int）
	currentGame = &gameBotw{}
	progress = &progressWatcher{
		ok: true,
		body: []byte(`{"ok":true,"points":[{"t":"shrine","hash":1,"done_hash":2,"en":"a","cn":"甲","x":1.0,"y":2.0,"done":true}],"counts":{"shrine":[1,136]},"spot_int":0,"generated":"2026-10-01 12:00:00","source":"xnavi"}`),
		gen:  "gen-botw",
	}

	s := &server{}
	rec := httptest.NewRecorder()
	s.handleProgress(rec, httptest.NewRequest(http.MethodGet, "/progress", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/progress status = %d", rec.Code)
	}
	assertHasKeys(t, rec.Body.Bytes(), "ok", "points", "counts", "generated")
	var m struct {
		Points []struct {
			T    string  `json:"t"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
			Done bool    `json:"done"`
		} `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("bad BOTW body: %v", err)
	}
	if len(m.Points) == 0 {
		t.Fatal("points empty")
	}
	p0 := m.Points[0]
	if p0.T == "" || p0.X == 0 && p0.Y == 0 {
		t.Errorf("points[0] missing t/x/y: %+v", p0)
	}
}

func hasJSONKey(body, key string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
