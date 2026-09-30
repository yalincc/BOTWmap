// sky_totk.go — TOTK 层级判定（天空 20 / 地上 18 / 地底 19）。
//
// 与 TOTKmap live-go sky.go / live-python server.py layer_of 完全同口径：
//   - 解析 area_sky.js（64 个大岛多边形）与 markers.js（layer=20 标记点）；
//   - gz<0 → 地底；gz≥900 → 天空（地面最高约 650）；多边形命中 → 天空；
//     gz≥300 且 600 内存在天空标记 → 天空；其余 → 地上。
//
// data 目录探测顺序：cwd/data → exe 同目录 data → cwd/../data → exe/../data。
// 开发时 cwd=xnavi 用 xnavi/data/；发布时 data 放 build/bin/data/（与 core 同目录）。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

type totkSkyPoly struct {
	Rings      [][][2]float64
	Xmin, Xmax float64
	Ymin, Ymax float64
}

var (
	totkSkyPolys    []totkSkyPoly
	totkSkyMarkers  [][2]float64 // (x, y)
	totkSkyDataInit bool
)

func totkDataDir() string {
	cwd, err := os.Getwd()
	if err == nil {
		if fi, e := os.Stat(filepath.Join(cwd, "data")); e == nil && fi.IsDir() {
			return filepath.Join(cwd, "data")
		}
	}
	if exe, err := os.Executable(); err == nil {
		for _, cand := range []string{
			filepath.Join(filepath.Dir(exe), "data"),
			filepath.Join(filepath.Dir(exe), "..", "data"),
		} {
			if fi, e := os.Stat(cand); e == nil && fi.IsDir() {
				return cand
			}
		}
	}
	if cwd != "" {
		if fi, e := os.Stat(filepath.Join(cwd, "..", "data")); e == nil && fi.IsDir() {
			return filepath.Join(cwd, "..", "data")
		}
	}
	return "data"
}

// totkExtractJSON 用正则从 JS 文件提取 window.X = [...] 的 JSON 文本。
func totkExtractJSON(text, pattern string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

func loadTotkSkyData() {
	if totkSkyDataInit {
		return
	}
	totkSkyDataInit = true
	dir := totkDataDir()

	// area_sky.js: window.TOTK_AREA_SKY=[{group, rings:[[[x,y],...]]}]
	if buf, err := os.ReadFile(filepath.Join(dir, "area_sky.js")); err == nil {
		body := totkExtractJSON(string(buf), `window\.TOTK_AREA_SKY\s*=\s*(\[.*?\]);`)
		if body != "" {
			var polys []struct {
				Rings [][][2]float64 `json:"rings"`
			}
			if json.Unmarshal([]byte(body), &polys) == nil {
				for _, p := range polys {
					sp := totkSkyPoly{Rings: p.Rings}
					sp.Xmin, sp.Xmax, sp.Ymin, sp.Ymax = 1e18, -1e18, 1e18, -1e18
					for _, r := range p.Rings {
						for _, pt := range r {
							if pt[0] < sp.Xmin {
								sp.Xmin = pt[0]
							}
							if pt[0] > sp.Xmax {
								sp.Xmax = pt[0]
							}
							if pt[1] < sp.Ymin {
								sp.Ymin = pt[1]
							}
							if pt[1] > sp.Ymax {
								sp.Ymax = pt[1]
							}
						}
					}
					totkSkyPolys = append(totkSkyPolys, sp)
				}
			}
		}
	}

	// markers.js: window.TOTK_MARKERS=[{id,layer,cat,name,x,y,...}]
	if buf, err := os.ReadFile(filepath.Join(dir, "markers.js")); err == nil {
		body := totkExtractJSON(string(buf), `window\.TOTK_MARKERS\s*=\s*(\[.*?\]);`)
		if body != "" {
			var mks []struct {
				Layer int     `json:"layer"`
				X     float64 `json:"x"`
				Y     float64 `json:"y"`
			}
			if json.Unmarshal([]byte(body), &mks) == nil {
				for _, m := range mks {
					if m.Layer == 20 {
						totkSkyMarkers = append(totkSkyMarkers, [2]float64{m.X, m.Y})
					}
				}
			}
		}
	}
}

// totkPip 射线法：点在多边形内。
func totkPip(x, y float64, pts [][2]float64) bool {
	inside := false
	j := len(pts) - 1
	for i := 0; i < len(pts); i++ {
		xi, yi := pts[i][0], pts[i][1]
		xj, yj := pts[j][0], pts[j][1]
		if ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}

func totkInSkyPoly(x, y float64) bool {
	if len(totkSkyPolys) == 0 {
		return false
	}
	for _, p := range totkSkyPolys {
		if x < p.Xmin || x > p.Xmax || y < p.Ymin || y > p.Ymax {
			continue
		}
		for _, r := range p.Rings {
			if totkPip(x, y, r) {
				return true
			}
		}
	}
	return false
}

func totkNearSkyMarker(x, y, rad float64) bool {
	if len(totkSkyMarkers) == 0 {
		return false
	}
	r2 := rad * rad
	for _, m := range totkSkyMarkers {
		dx, dy := m[0]-x, m[1]-y
		if dx*dx+dy*dy <= r2 {
			return true
		}
	}
	return false
}

// totkLayerName 位面号 → 中文名（日志用）。
func totkLayerName(l int) string {
	switch l {
	case 19:
		return "地底"
	case 20:
		return "天空"
	default:
		return "地面"
	}
}

// totkLayerOf 玩家位置（展示序 gx=X东, gy=-Y北, gz=真实高度）→ 数据层。
func totkLayerOf(gx, gy, gz float32) int {
	loadTotkSkyData()
	if gz < 0.0 {
		return 19 // 地底：海平面 0 以下
	}
	if gz >= 900.0 {
		return 20 // 高空必然天空（地面最高约 650）
	}
	if totkInSkyPoly(float64(gx), float64(gy)) {
		return 20
	}
	if gz >= 300.0 && totkNearSkyMarker(float64(gx), float64(gy), 600.0) {
		return 20
	}
	return 18
}
