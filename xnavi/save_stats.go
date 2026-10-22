// save_stats.go — 存档统计扩展：任务 4 项 / 收集 5 项 / 讨伐 3 项。
//
// 背景：网页探索度面板共 18 项。核心原本只算 5 类（神庙/塔/克洛格/回忆/神兽），
// 其余 12 项一直靠网页端的 /save 管道解析。本文件让核心自己把这 12 项补齐，
// 数据来源与网页完全同一批（crc32(flagName) 命中即完成）。
//
// 数据表 data/save_stats.json 由 source/build_save_stats.py 从网页字典派生，
// 只保留 hash 集合与分类，不含坐标，约 23KB。**该文件禁止手改，网页字典变更须重跑脚本。**
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data/save_stats.json
var saveStatsFS embed.FS

// 扩展统计的 12 个类别（顺序即内部位掩码位序）
var saveStatsKeys = []string{
	"quest_main",  // 0 主线挑战
	"quest_side",  // 1 支线任务
	"quest_shrine",// 2 神庙挑战
	"quest_dlc",   // 3 DLC 任务
	"chest",       // 4 宝箱
	"weapon",      // 5 武器获得
	"pb_equip",    // 6 图鉴·装备
	"pb_mat",      // 7 图鉴·材料
	"pb_enemy",    // 8 图鉴·怪物
	"boss_hinox",  // 9 西诺克斯
	"boss_talus",  // 10 岩石巨人
	"boss_molduga",// 11 莫尔德拉吉克
}

const saveStatsN = 12

type saveStatsTable struct {
	Meta struct {
		Counts map[string]int `json:"counts"`
	} `json:"meta"`
	Quest struct {
		Main   []uint32 `json:"main"`
		Side   []uint32 `json:"side"`
		Shrine []uint32 `json:"shrine"`
		DLC    []uint32 `json:"dlc"`
	} `json:"quest"`
	Chest   []uint32 `json:"chest"`
	Weapon  []uint32 `json:"weapon"`
	PBEquip []uint32 `json:"pbEquip"`
	PBMat   []uint32 `json:"pbMat"`
	PBEnemy []uint32 `json:"pbEnemy"`
	Boss    struct {
		Hinox   []uint32 `json:"hinox"`
		Talus   []uint32 `json:"talus"`
		Molduga []uint32 `json:"molduga"`
	} `json:"boss"`
}

var (
	saveStatsOnce  sync.Once
	saveStatsMasks map[uint32]uint16 // hash -> 类别位掩码
	saveStatsTotal [saveStatsN]int   // 各类别分母
	saveStatsOK    bool
)

// loadSaveStats 惰性载入内嵌字典，构建 hash→位掩码 索引。
func loadSaveStats() {
	saveStatsOnce.Do(func() {
		buf, err := saveStatsFS.ReadFile("data/save_stats.json")
		if err != nil {
			fmt.Printf("  [stats] embedded save_stats.json unreadable: %v\n", err)
			return
		}
		var t saveStatsTable
		if err := json.Unmarshal(buf, &t); err != nil {
			fmt.Printf("  [stats] embedded save_stats.json invalid: %v\n", err)
			return
		}
		groups := [saveStatsN][]uint32{
			t.Quest.Main, t.Quest.Side, t.Quest.Shrine, t.Quest.DLC,
			t.Chest, t.Weapon, t.PBEquip, t.PBMat, t.PBEnemy,
			t.Boss.Hinox, t.Boss.Talus, t.Boss.Molduga,
		}
		saveStatsMasks = make(map[uint32]uint16, 2048)
		for idx, list := range groups {
			saveStatsTotal[idx] = len(list)
			bit := uint16(1) << uint(idx)
			for _, h := range list {
				saveStatsMasks[h] |= bit
			}
		}
		saveStatsOK = true
		fmt.Printf("  [stats] 扩展统计表：任务 %d/%d/%d/%d，收集 %d/%d/%d/%d/%d，讨伐 %d/%d/%d\n",
			saveStatsTotal[0], saveStatsTotal[1], saveStatsTotal[2], saveStatsTotal[3],
			saveStatsTotal[4], saveStatsTotal[5], saveStatsTotal[6], saveStatsTotal[7], saveStatsTotal[8],
			saveStatsTotal[9], saveStatsTotal[10], saveStatsTotal[11])
	})
}

// countSaveStats 遍历一次存档扁平表，统计 12 项扩展探索度。
// 返回 map[类别名][2]int{done, total}；字典缺失时返回 nil。
func countSaveStats(entries map[uint32]uint32) map[string][2]int {
	loadSaveStats()
	if !saveStatsOK || len(entries) == 0 {
		return nil
	}
	var done [saveStatsN]int
	for h, v := range entries {
		if v == 0 {
			continue
		}
		mask, ok := saveStatsMasks[h]
		if !ok {
			continue
		}
		for i := 0; i < saveStatsN; i++ {
			if mask&(1<<uint(i)) != 0 {
				done[i]++
			}
		}
	}
	out := make(map[string][2]int, saveStatsN)
	for i, name := range saveStatsKeys {
		out[name] = [2]int{done[i], saveStatsTotal[i]}
	}
	return out
}
