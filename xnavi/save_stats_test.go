package main

import (
	"encoding/json"
	"os"
	"testing"
)

// TestSaveStatsDump 用真实存档验证扩展统计（任务/收集/讨伐 12 项）。
// 用法：XNAVI_TEST_SAVE=<path> go test -run TestSaveStatsDump -v
// 期望值由 source/build_save_stats.py 同一口径的 Python 参考实现产出，用于双实现对拍。
func TestSaveStatsDump(t *testing.T) {
	p := os.Getenv("XNAVI_TEST_SAVE")
	if p == "" {
		t.Skip("未设置 XNAVI_TEST_SAVE")
	}
	entries, _ := parseGameData(p)
	if len(entries) == 0 {
		t.Fatalf("存档解析失败: %s", p)
	}
	got := countSaveStats(entries)
	if got == nil {
		t.Fatal("扩展统计表未载入")
	}
	b, _ := json.MarshalIndent(got, "", "  ")
	if out := os.Getenv("XNAVI_TEST_OUT"); out != "" {
		if err := os.WriteFile(out, b, 0644); err != nil {
			t.Fatalf("写出结果失败: %v", err)
		}
	}
	t.Logf("entries=%d\n%s", len(entries), string(b))
}
