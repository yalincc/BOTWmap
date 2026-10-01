// progress_totk.go — TOTK 进度解析（P2b，自 live-go progress_totk.go/2 移植）。
//
// 与 totk.yalin.site 前端 js/app.js syncProgressFromServer 逐字段同口径：
//   - /progress 返回 {ok, version, save, doneIds, mapped, counts, mtime}；
//     gen = "<path>:<mtime秒>"（/pos.progressGen 同源，网站据此自动重拉进度）
//   - progress.sav 解析：版本表 / HASH_TABLE_END(0x03c800) / 0xa3db7114 哨兵 / GUID 表；
//     murmur3 x86 32-bit（'Clear'/'Open' 等值 hash）
//   - 哈希参考表 totk_save_hashes.js（CompletismHashes，提取自 savegame-editors MIT）
//     与 explore_save_map.js（标点 ID → hash/GUID 映射）——go:embed 内嵌，单文件分发
//
// 存档隔离红线（三组合互不干扰）：本 watcher 只认 TOTK 的 progress.sav
// （文件名硬编码，不读 currentGame），与 BOTW 的 game_data.sav 无交集；
// Ryujinx 存档根 + GUI save-dir 覆盖，slot_00 优先、mtime 降序。
// 数据只在 currentGame == totk 时对外提供（server/monitor 分发），BOTW/Cemu 不受影响。

package main

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed data/totk_save_hashes.js
var totkHashesFS embed.FS

//go:embed data/explore_save_map.js
var totkExploreFS embed.FS

const (
	totkHashTableEnd = 0x03c800
	totkGuidSentinel = 0xa3db7114
)

var totkSaveGameVersions = []struct {
	Header, Meta uint32
	Size         int
	Version      string
}{
	{0x0046c3c8, 0x0003c050, 2307552, "v1.0"},
	{0x0047e0f4, 0x0003c088, 2307656, "v1.1.x/v1.2.x"},
	{0x0049e946, 0x0003c138, 2307856, "v1.4.x"},
}

type totkParsedSave struct {
	Ok        bool
	Version   string
	Error     string
	OffByHash map[uint32]int // hash -> 字节偏移（数据表定位）
	Guids     []string
	Data      []byte
}

// totkSaveFiles TOTK 存档发现（隔离红线：只找 progress.sav，与 BOTW game_data.sav 无交集）。
// Ryujinx 存档根 + GUI save-dir 覆盖（--save-dir=），与 BOTW 发现共用 override 出口。
func totkSaveFiles() []string {
	var out []string
	if saveDirOverride != "" {
		out = append(out, findSaveFiles(saveDirOverride, []string{"progress.sav"})...)
	}
	for _, root := range (&ryujinxPlatform{}).SaveRoots() {
		out = append(out, findSaveFiles(root, []string{"progress.sav"})...)
	}
	seen := map[string]bool{}
	var uniq []string
	for _, f := range out {
		if !seen[f] {
			seen[f] = true
			uniq = append(uniq, f)
		}
	}
	sort.Strings(uniq)
	return uniq
}

// totkFindLatestProgress 返回 (mtime秒, path)：mtime 降序 + slot_00 优先
// （Ryujinx 保存时所有槽 mtime 可能同刻）。
func totkFindLatestProgress() (float64, string) {
	type cand struct {
		mt   float64
		path string
	}
	var cands []cand
	for _, p := range totkSaveFiles() {
		if !strings.HasSuffix(strings.ToLower(p), "progress.sav") {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		cands = append(cands, cand{float64(fi.ModTime().UnixNano()) / 1e9, p})
	}
	if len(cands) == 0 {
		return 0, ""
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt > cands[j].mt })
	for _, c := range cands {
		if strings.Contains(c.path, "slot_00") {
			return c.mt, c.path
		}
	}
	return cands[0].mt, cands[0].path
}

// totkParseProgressSave 解析 progress.sav（与 js parse() 同口径）。
func totkParseProgressSave(path string) *totkParsedSave {
	data, err := os.ReadFile(path)
	if err != nil {
		return &totkParsedSave{Ok: false, Error: "read: " + err.Error()}
	}
	if len(data) < 12 || binary.LittleEndian.Uint32(data[0:4]) != 0x01020304 {
		return &totkParsedSave{Ok: false, Error: "bad header"}
	}
	header := binary.LittleEndian.Uint32(data[4:8])
	meta := binary.LittleEndian.Uint32(data[8:12])
	version := ""
	for _, v := range totkSaveGameVersions {
		if len(data) == v.Size && header == v.Header && meta == v.Meta {
			version = v.Version
			break
		}
	}
	if version == "" {
		return &totkParsedSave{Ok: false, Error: "unsupported version"}
	}
	offByHash := map[uint32]int{}
	guidsOffset := -1
	end := totkHashTableEnd
	if end > len(data) {
		end = len(data)
	}
	for j := 0x28; j < end-7; j += 8 {
		h := binary.LittleEndian.Uint32(data[j:])
		if h == totkGuidSentinel {
			guidsOffset = int(binary.LittleEndian.Uint32(data[j+4:]))
			break
		}
		offByHash[h] = j + 4
	}
	if guidsOffset <= 0 || guidsOffset >= len(data) {
		return &totkParsedSave{Ok: false, Error: "no guid table"}
	}
	guids := []string{}
	k := guidsOffset
	for k < len(data)-8 {
		lo := binary.LittleEndian.Uint32(data[k:])
		up := binary.LittleEndian.Uint32(data[k+4:])
		if lo == 0 && up == 0 {
			break
		}
		guids = append(guids, fmt.Sprintf("0x%08x%08x", up, lo))
		k += 8
	}
	return &totkParsedSave{Ok: true, Version: version, OffByHash: offByHash,
		Guids: guids, Data: data}
}

// totkMurmur3 MurmurHash3 x86 32-bit（与 murmurHash3.x86.hash32 完全一致）。
func totkMurmur3(key string, seed uint32) uint32 {
	data := []byte(key)
	n := len(data)
	h := seed & 0xFFFFFFFF
	i := 0
	for ; i+4 <= n; i += 4 {
		k := binary.LittleEndian.Uint32(data[i:])
		k = (k * 0xCC9E2D51) & 0xFFFFFFFF
		k = ((k<<15 | k>>17) & 0xFFFFFFFF) * 0x1B873593 & 0xFFFFFFFF
		h ^= k
		h = (((h<<13 | h>>19) & 0xFFFFFFFF) * 5 + 0xE6546B64) & 0xFFFFFFFF
	}
	var k uint32
	tail := data[i:]
	switch len(tail) {
	case 3:
		k ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k ^= uint32(tail[0])
		k = (k * 0xCC9E2D51) & 0xFFFFFFFF
		k = ((k<<15 | k>>17) & 0xFFFFFFFF) * 0x1B873593 & 0xFFFFFFFF
		h ^= k
	}
	h ^= uint32(n)
	h ^= h >> 16
	h = (h * 0x85EBCA6B) & 0xFFFFFFFF
	h ^= h >> 13
	h = (h * 0xC2B2AE35) & 0xFFFFFFFF
	h ^= h >> 16
	return h
}

// parseCompletismHashes 解析内嵌 totk_save_hashes.js 的 CompletismHashes。
func parseCompletismHashes() map[string][]string {
	out := map[string][]string{}
	buf, err := totkHashesFS.ReadFile("data/totk_save_hashes.js")
	if err != nil {
		return out
	}
	re := regexp.MustCompile(`(?s)([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\[(.*?)\n\s*\],?`)
	ms := re.FindAllStringSubmatch(string(buf), -1)
	for _, m := range ms {
		if len(m) < 3 {
			continue
		}
		key, body := m[1], m[2]
		var arr []string
		itemRe := regexp.MustCompile(`0x([0-9a-fA-F]+)|'([^']+)'|"([^"]+)"`)
		for _, im := range itemRe.FindAllStringSubmatch(body, -1) {
			if im[1] != "" {
				arr = append(arr, "0x"+im[1])
			} else if im[2] != "" {
				arr = append(arr, im[2])
			} else if im[3] != "" {
				arr = append(arr, im[3])
			}
		}
		out[key] = arr
	}
	return out
}

// totkExploreMap explore_save_map.js：标点 ID → hash/GUID 映射。
type totkExploreMap struct {
	Towers  map[string]string `json:"towers"`
	Tears   map[string]string `json:"tears"`
	Bubbuls map[string]string `json:"bubbuls"`
}

func parseTotkExploreMap() *totkExploreMap {
	buf, err := totkExploreFS.ReadFile("data/explore_save_map.js")
	if err != nil {
		return &totkExploreMap{}
	}
	re := regexp.MustCompile(`(?s)window\.TOTK_EXPLORE_MAP\s*=\s*(\{.*?\});`)
	m := re.FindStringSubmatch(string(buf))
	if m == nil {
		return &totkExploreMap{}
	}
	var em totkExploreMap
	if json.Unmarshal([]byte(m[1]), &em) != nil {
		return &totkExploreMap{}
	}
	return &em
}

// hval 读 hash 表对应值（与 js hval 同口径）。
func (p *totkParsedSave) hval(hashInt uint32) (uint32, bool) {
	off, ok := p.OffByHash[hashInt]
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint32(p.Data[off:]), true
}

// progressCount 值为 val 的 hash 个数（val 为空表示 1）。
func (p *totkParsedSave) progressCount(lst []string, val string) int {
	target := uint32(1)
	if val != "" {
		target = totkMurmur3(val, 0)
	}
	n := 0
	for _, h := range lst {
		hv, err := strconv.ParseUint(strings.TrimPrefix(h, "0x"), 16, 32)
		if err != nil {
			continue
		}
		if v, ok := p.hval(uint32(hv)); ok && v == target {
			n++
		}
	}
	return n
}

func (p *totkParsedSave) progressCountGuids(lst []string) int {
	n := 0
	for _, g := range lst {
		for _, gs := range p.Guids {
			if gs == g {
				n++
				break
			}
		}
	}
	return n
}

var totkDragonTears = []uint32{
	0x95eaf7f7, 0xd8f6148f, 0xea112a5c, 0x0ba4de99, 0x5a630ce5, 0x2146bc12,
	0x9061714b, 0x7cc0375a, 0xa14c6ed1, 0x587df5b0, 0x5279d33f, 0xc595c991,
}

// totkProgressCount done/total。
type totkProgressCount struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// totkProgressCounts 与 js collect() / server.py _progress_counts() 同口径。
func totkProgressCounts(p *totkParsedSave, C map[string][]string) map[string]totkProgressCount {
	out := map[string]totkProgressCount{}
	cnt := func(name string, lst []string, val string) {
		out[name] = totkProgressCount{p.progressCount(lst, val), len(lst)}
	}
	cg := func(name string, lst []string) {
		out[name] = totkProgressCount{p.progressCountGuids(lst), len(lst)}
	}
	tears := make([]string, 0, len(totkDragonTears))
	for _, t := range totkDragonTears {
		tears = append(tears, fmt.Sprintf("0x%08x", t))
	}
	cnt("鸟望台", C["TOWERS_FOUND"], "")
	cnt("龙之泪", tears, "")
	cnt("神庙", C["SHRINES_STATUS"], "Clear")
	cnt("树根", C["LIGHTROOTS_STATUS"], "Open")
	cnt("克洛格", C["KOROKS_HIDDEN"], "")
	cnt("双倍克洛格", C["KOROKS_CARRY"], "Clear")
	cg("魔犹伊遗失物", C["BUBBULS_GUIDS"])
	cnt("残旧的地图", C["TREASURE_MAPS_FOUND"], "")
	cg("贤者的遗志", C["SAGE_WILLS_FOUND"])
	sc := append(append([]string{}, C["SCHEMATICS_STONE_FOUND"]...), C["SCHEMATICS_YIGA_FOUND"]...)
	cnt("设计图石板", sc, "")
	cg("卡邦达立牌", C["ADDISON_COMPLETED"])
	for zh, key := range map[string]string{
		"独眼巨人": "BOSSES_HINOXES_DEFEATED", "岩石巨人": "BOSSES_TALUSES_DEFEATED",
		"莫尔德拉吉克": "BOSSES_MOLDUGAS_DEFEATED", "方块魔像": "BOSSES_FLUX_CONSTRUCT_DEFEATED",
		"巨霸伽马": "BOSSES_FROXS_DEFEATED", "古栗欧克": "BOSSES_GLEEOKS_DEFEATED",
		"地洞入口": "LOCATION_CHASMS_VISITED", "洞穴入口": "LOCATION_CAVES_VISITED",
		"井": "LOCATION_WELLS_VISITED",
	} {
		cnt(zh, C[key], "")
	}
	return out
}

// totkBuildDoneIds towers/tears hash==1、bubbuls guid 存在（与 server.py 同口径）。
func totkBuildDoneIds(p *totkParsedSave, em *totkExploreMap) ([]int, int) {
	var ids []int
	mapped := 0
	for _, tbl := range []map[string]string{em.Towers, em.Tears} {
		for mid, hv := range tbl {
			mapped++
			hvInt, err := strconv.ParseUint(strings.TrimPrefix(hv, "0x"), 16, 32)
			if err != nil {
				continue
			}
			if v, ok := p.hval(uint32(hvInt)); ok && v == 1 {
				if id, err := strconv.Atoi(mid); err == nil {
					ids = append(ids, id)
				}
			}
		}
	}
	for mid, guid := range em.Bubbuls {
		mapped++
		for _, gs := range p.Guids {
			if gs == guid {
				if id, err := strconv.Atoi(mid); err == nil {
					ids = append(ids, id)
				}
				break
			}
		}
	}
	sort.Ints(ids)
	return ids, mapped
}

// ---- TOTK watcher（与 BOTW progressWatcher 完全隔离）----

type totkProgressWatcher struct {
	mu         sync.RWMutex
	ok         bool
	body       []byte
	gen        string
	save       string
	mtime      float64
	init       bool
	completism map[string][]string
	emap       *totkExploreMap
}

var totkProgress *totkProgressWatcher

func newTotkProgressWatcher() *totkProgressWatcher {
	w := &totkProgressWatcher{}
	w.completism = parseCompletismHashes()
	w.emap = parseTotkExploreMap()
	w.refresh()
	return w
}

func (w *totkProgressWatcher) refresh() {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Q14 存档隔离：非 TOTK 模式（BOTW/Cemu）下本 watcher 完全待机
	if currentGame == nil || currentGame.Name() != "totk" {
		w.ok = false
		return
	}
	mt, path := totkFindLatestProgress()
	if path == "" {
		if !w.init {
			fmt.Println("  [progress-totk] no progress.sav yet - will watch for it")
		}
		w.ok = false
		w.init = true
		return
	}
	p := totkParseProgressSave(path)
	if !p.Ok {
		if !w.init {
			fmt.Printf("  [progress-totk] parse failed: %s\n", p.Error)
		}
		w.ok = false
		w.init = true
		return
	}
	ids, mapped := totkBuildDoneIds(p, w.emap)
	counts := totkProgressCounts(p, w.completism)
	obj := map[string]any{
		"ok":      true,
		"version": p.Version,
		"save":    path,
		"doneIds": ids,
		"mapped":  mapped,
		"counts":  counts,
		"mtime":   mt,
	}
	buf, _ := json.Marshal(obj)
	w.body = buf
	w.save = path
	w.mtime = mt
	w.gen = fmt.Sprintf("%s:%d", path, int64(math.Round(mt)))
	w.ok = true
	if !w.init {
		fmt.Printf("  [progress-totk] save=%s\n", path)
		fmt.Printf("  [progress-totk] doneIds=%d  towers/tears/bubbuls mapped=%d\n", len(ids), mapped)
		for _, k := range []string{"鸟望台", "神庙", "树根", "克洛格", "魔犹伊遗失物", "卡邦达立牌"} {
			if c, ok := counts[k]; ok {
				fmt.Printf("    %s %d/%d\n", k, c.Done, c.Total)
			}
		}
	}
	w.init = true
}

func (w *totkProgressWatcher) loop() {
	for {
		time.Sleep(2 * time.Second)
		mt, path := totkFindLatestProgress()
		w.mu.RLock()
		changed := path != w.save || mt != w.mtime
		w.mu.RUnlock()
		if changed {
			w.refresh()
		}
	}
}

func (w *totkProgressWatcher) snapshot() (bool, []byte, string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ok, w.body, w.gen
}

// totkSummarize status.json 用：TOTK 计数 → GUI 五槽口径
// （shrine=神庙 / tower=鸟望台 / korok=克洛格 / memory=龙之泪 / beast=树根，
// GUI 标签按 detectedGame 切换，见 App.vue gameLabels）。
func (w *totkProgressWatcher) summarize() (bool, map[string]any) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.ok {
		return false, nil
	}
	var obj struct {
		Counts map[string]totkProgressCount `json:"counts"`
		Save   string                       `json:"save"`
	}
	if json.Unmarshal(w.body, &obj) != nil {
		return false, nil
	}
	g := func(name string) [2]int {
		if c, ok := obj.Counts[name]; ok {
			return [2]int{c.Done, c.Total}
		}
		return [2]int{0, 0}
	}
	counts := map[string][2]int{
		"shrine": g("神庙"),
		"tower":  g("鸟望台"),
		"korok":  g("克洛格"),
		"memory": g("龙之泪"),
		"beast":  g("树根"),
	}
	return true, map[string]any{
		"counts": counts,
		"save":   obj.Save,
	}
}
