// npcnames.go：actor 名 → 游戏内显示名映射（BOTW）。
//
// 两层数据（均为权威源）：
//   1) 英文：npcnames.json —— 社区整理全量 NPC actor↔显示名表（GBAtemp Trainer 帖）
//   2) 中文：npcnames_zh.json —— 官方 romfs Msg_CNzh.product\ActorType\NPC.msbt 解析
//      （BOTWmap\data\msbt_zh\NPC.json 导出，428 条），如 Npc_HatenoVillage022=万作
// 社区表部分 actor 名带 "_Name" 后缀（Npc_Road_008_Name），内存实际为 Npc_Road_008；
// 加载时对 "_Name" 结尾键自动注册去后缀别名。
// 未收录的 Label/LabelEn 为空，前端回退显示 actor 名。

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	npcLabel   map[string]string // 中文显示名（优先）
	npcLabelEn map[string]string // 英文显示名
)

// loadNpcNames 读取 npcnames.json + npcnames_zh.json（exe 所在目录）。失败不影响启动。
func loadNpcNames() {
	dir := filepath.Dir(os.Args[0])
	// 英文
	if m, err := loadJSONMap(dir, "npcnames.json"); err != nil {
		fmt.Printf("[npcnames] %v (english labels disabled)\n", err)
	} else {
		npcLabelEn = aliasMap(m)
		fmt.Printf("[npcnames] en: %d entries → %d keys\n", len(m), len(npcLabelEn))
	}
	// 中文（覆盖优先）
	if m, err := loadJSONMap(dir, "npcnames_zh.json"); err != nil {
		fmt.Printf("[npcnames] %v (chinese labels disabled)\n", err)
	} else {
		npcLabel = aliasMap(m)
		fmt.Printf("[npcnames] zh: %d entries → %d keys\n", len(m), len(npcLabel))
	}
}

// loadJSONMap 读取 key→value JSON。
func loadJSONMap(dir, name string) (map[string]string, error) {
	p := filepath.Join(dir, name)
	buf, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("load %s failed: %v", p, err)
	}
	var m map[string]string
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, fmt.Errorf("parse %s failed: %v", p, err)
	}
	return m, nil
}

// aliasMap 对 "_Name" 结尾的键注册去后缀别名。
func aliasMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)*2)
	for k, v := range m {
		out[k] = v
		if len(k) > len("_Name") && strings.HasSuffix(k, "_Name") {
			out[strings.TrimSuffix(k, "_Name")] = v
		}
	}
	return out
}

// labelOf 返回中文显示名；未知返回 ""（前端回退 actor 名）。
func labelOf(name string) string {
	if npcLabel != nil {
		if v, ok := npcLabel[name]; ok {
			return v
		}
	}
	if npcLabelEn != nil {
		if v, ok := npcLabelEn[name]; ok {
			return v
		}
	}
	return ""
}

// labelEnOf 返回英文显示名（对照用）；未知返回 ""。
func labelEnOf(name string) string {
	if npcLabelEn != nil {
		return npcLabelEn[name]
	}
	return ""
}
