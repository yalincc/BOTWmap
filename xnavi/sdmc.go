// sdmc.go — Eden sdmc 目录解析与 pos.json 兜底信标读取。
//
// sdmc:/xnavi/ 在宿主的真实路径为 <edenDir>/user/sdmc/xnavi/。
// 路径解析模式同 platform_eden.go SaveRoots：env 覆盖 > 进程自省 > AppData。
// XNAVI_SDMC_DIR 覆盖供测试指向假目录。

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var (
	sdmcDirMu    sync.Mutex
	sdmcDirCache string
	sdmcDirPID   uint32
)

// edenSdmcXnaviDir 解析 xnavi 信标目录（带 per-pid 缓存）。
func edenSdmcXnaviDir(p *edenPlatform) string {
	if env := os.Getenv("XNAVI_SDMC_DIR"); env != "" {
		return filepath.Clean(env)
	}
	sdmcDirMu.Lock()
	defer sdmcDirMu.Unlock()
	if sdmcDirCache != "" && sdmcDirPID == p.PID() {
		return sdmcDirCache
	}
	var cands []string
	add := func(pp string) {
		if pp != "" {
			cands = append(cands, filepath.Clean(pp))
		}
	}
	// 进程自省（eden.exe 所在目录 → user/sdmc/xnavi）
	var edenExe string
	if p.h != 0 {
		edenExe = processExePath(p.h)
	} else if p.pid != 0 {
		edenExe = processExePathByPID(p.pid)
	}
	if edenExe != "" {
		dir := filepath.Dir(edenExe)
		add(filepath.Join(dir, "user", "sdmc", "xnavi"))
		add(filepath.Join(dir, "..", "user", "sdmc", "xnavi"))
	}
	// AppData 兜底
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		for _, e := range []string{"Eden", "eden", "yuzu", "Yuzu"} {
			add(filepath.Join(appdata, e, "user", "sdmc", "xnavi"))
		}
	}
	// 取第一个存在的目录；都不存在则取首个候选
	chosen := ""
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			chosen = c
			break
		}
	}
	if chosen == "" && len(cands) > 0 {
		chosen = cands[0]
	}
	sdmcDirCache = chosen
	sdmcDirPID = p.PID()
	return chosen
}

type posJSON struct {
	Magic     string  `json:"magic"`
	Schema    uint16  `json:"schema"`
	Build     uint16  `json:"build"`
	Seq       uint32  `json:"seq"`
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	Z         float32 `json:"z"`
	Yaw       float32 `json:"yaw"`
	State     uint32  `json:"state"`
	GameClock uint64  `json:"gameClock"`
	HP        float32 `json:"hp"`
	Stamina   float32 `json:"stamina"`
	Flags     uint32  `json:"flags"`
	TS        float64 `json:"ts"`
}

// readPosJSON 读 pos.json 兜底信标，返回 Beacon 与 unix 秒时间戳。
func readPosJSON(dir string) (Beacon, float64, bool) {
	if dir == "" {
		return Beacon{}, 0, false
	}
	data, err := os.ReadFile(filepath.Join(dir, "pos.json"))
	if err != nil {
		return Beacon{}, 0, false
	}
	var j posJSON
	if json.Unmarshal(data, &j) != nil {
		return Beacon{}, 0, false
	}
	if j.Magic != "XNAV" || j.Schema != beaconSchema {
		return Beacon{}, 0, false
	}
	b := Beacon{
		Schema: j.Schema, Build: j.Build, Seq: j.Seq,
		X: j.X, Y: j.Y, Z: j.Z, Yaw: j.Yaw, State: j.State,
		GameClock: j.GameClock, HP: j.HP, Stamina: j.Stamina, Flags: j.Flags,
	}
	return b, j.TS, true
}
