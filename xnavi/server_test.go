package main

// 接口契约回归测试（协议 v1 §3，2026-10-01 定稿）：
// /pos 与 /progress 的字段集与类型是 xnavi ↔ 两个地图站的跨程序契约，
// 任何一端改动都应先让本文件变红——杜绝"对接对不上半天排查"复发。
// 进度 watcher 的 body 以注入方式构造（HTTP 边界契约锁定；
// 解析正确性由真机冒烟覆盖：TOTK 神庙 8/152 / BOTW 104/136）。

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
	origGame, origProgress, origTotk := currentGame, progress, totkProgress
	defer func() { currentGame, progress, totkProgress = origGame, origProgress, origTotk }()

	currentGame = &gameTotk{}
	state.mu.Lock()
	state.ok = true
	state.gx, state.gy, state.gz = 709.2, -1381.7, 1585.3
	state.mx, state.my = 1381.7, 709.2
	state.verified = true
	state.mu.Unlock()
	progress = &progressWatcher{ok: true, body: []byte(`{}`), gen: "gen-botw"}
	totkProgress = &totkProgressWatcher{ok: true, body: []byte(`{}`), gen: "gen-totk"}

	s := &server{}
	rec := httptest.NewRecorder()
	s.handlePos(rec, httptest.NewRequest(http.MethodGet, "/pos", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/pos status = %d", rec.Code)
	}
	// 协议 §2.1：ok/mx/my/gx/gy/gz/verified/progressGen/target/game 必备（layer/age/copies/source 为既有扩展）
	assertHasKeys(t, rec.Body.Bytes(),
		"ok", "mx", "my", "gx", "gy", "gz", "verified", "progressGen", "target", "game")

	// game 与 progressGen 口径一致：TOTK 模式 → game=totk 且 gen 来自 totk watcher
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m["game"] != "totk" {
		t.Errorf("game = %v, want totk", m["game"])
	}
	if m["progressGen"] != "gen-totk" {
		t.Errorf("progressGen = %v, want gen-totk（TOTK watcher 分发）", m["progressGen"])
	}
}

func TestContractProgressBotwShape(t *testing.T) {
	origGame, origProgress, origTotk := currentGame, progress, totkProgress
	defer func() { currentGame, progress, totkProgress = origGame, origProgress, origTotk }()

	// BOTW 口径（协议 §2.2）：ok/points/counts/generated（+source/spot_int）
	currentGame = &gameBotw{}
	progress = &progressWatcher{
		ok: true,
		body: []byte(`{"ok":true,"points":[{"t":"shrine","hash":1,"done_hash":2,"en":"a","cn":"甲","x":1.0,"y":2.0,"done":true}],"counts":{"shrine":[1,136]},"spot_int":0,"generated":"2026-10-01 12:00:00","source":"xnavi"}`),
		gen:  "gen-botw",
	}
	totkProgress = nil

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

func TestContractProgressTotkShape(t *testing.T) {
	origGame, origProgress, origTotk := currentGame, progress, totkProgress
	defer func() { currentGame, progress, totkProgress = origGame, origProgress, origTotk }()

	// TOTK 口径（协议 §2.2）：ok/version/save/doneIds/mapped/counts/mtime；doneIds 为 int 数组
	currentGame = &gameTotk{}
	progress = &progressWatcher{ok: true, body: []byte(`{}`)}
	totkProgress = &totkProgressWatcher{
		ok: true,
		body: []byte(`{"ok":true,"version":"1.1.x","save":"progress.sav","doneIds":[119,135,2884],"mapped":118,"counts":{"shrine":[8,152]},"mtime":1759300000}`),
		gen:  "gen-totk",
	}

	s := &server{}
	rec := httptest.NewRecorder()
	s.handleProgress(rec, httptest.NewRequest(http.MethodGet, "/progress", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/progress status = %d", rec.Code)
	}
	assertHasKeys(t, rec.Body.Bytes(), "ok", "version", "save", "doneIds", "mapped", "counts", "mtime")
	var m struct {
		DoneIds []int `json:"doneIds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("bad TOTK body: %v", err)
	}
	if len(m.DoneIds) == 0 {
		t.Fatal("doneIds empty or not an int array")
	}
}

func TestContractGameDispatchConsistency(t *testing.T) {
	// 协议 §3.4：/pos.game 与 /progress 口径一致（botw→points 口径 / totk→doneIds 口径）
	origGame, origProgress, origTotk := currentGame, progress, totkProgress
	defer func() { currentGame, progress, totkProgress = origGame, origProgress, origTotk }()

	progress = &progressWatcher{ok: true, body: []byte(`{"ok":true,"points":[],"generated":"x"}`), gen: "gb"}
	totkProgress = &totkProgressWatcher{ok: true, body: []byte(`{"ok":true,"doneIds":[1],"mtime":1}`), gen: "gt"}
	s := &server{}

	currentGame = &gameBotw{}
	rec := httptest.NewRecorder()
	s.handleProgress(rec, httptest.NewRequest(http.MethodGet, "/progress", nil))
	body := rec.Body.String()
	if hasJSONKey(body, "doneIds") || !hasJSONKey(body, "points") {
		t.Errorf("game=botw must serve points口径, got: %s", body)
	}

	currentGame = &gameTotk{}
	rec2 := httptest.NewRecorder()
	s.handleProgress(rec2, httptest.NewRequest(http.MethodGet, "/progress", nil))
	body2 := rec2.Body.String()
	if hasJSONKey(body2, "points") || !hasJSONKey(body2, "doneIds") {
		t.Errorf("game=totk must serve doneIds口径, got: %s", body2)
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
