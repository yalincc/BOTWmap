package locate

import (
	"testing"

	"botwmap/enavi/internal/scan"
)

// TestIsShrineScale 固化神庙本地尺度的物理判据（ENAVI-HANDBOOK §3.5）。
//
//	神庙试炼场：|X|<100 && |Z|<100 && Y<50   → 挂起
//	平原之塔  ：X≈-215, Z≈250, Y≈250        → 不挂起（Z 超 100 且 Y 超 50）
//	平原遗迹  ：|X|<300, |Z|<300, Y≈100~130  → 不挂起（Y 超 50）
func TestIsShrineScale(t *testing.T) {
	cases := []struct {
		name string
		v    scan.Vec3
		want bool
	}{
		{"地下神庙", scan.Vec3{X: 5, Y: 12, Z: -8}, true},
		{"神庙深处", scan.Vec3{X: -30, Y: 2, Z: 45}, true},
		{"平原之塔（不误判）", scan.Vec3{X: -215, Y: 250, Z: 250}, false},
		{"中央平原遗迹（不误判）", scan.Vec3{X: 120, Y: 115, Z: -200}, false},
		{"大世界地表", scan.Vec3{X: 1662, Y: 116, Z: 1921}, false},
	}
	for _, c := range cases {
		if got := isShrineScale(c.v); got != c.want {
			t.Errorf("%s: isShrineScale() = %v, want %v", c.name, got, c.want)
		}
	}
}
