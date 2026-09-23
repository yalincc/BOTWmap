// actorinfo.go：ActorInfo（actor 原型/名字对象）全局缓存。
//
// M4' 后发现的坑：NPC 名字对象（字符串 +0x4C = 4B actor hash）大多驻留在
// 0x016A/0x020C 等资源区，而实例对象（hash 引用点）在 0x016B 运行时区。
// 旧实现只在 0x016B 区扫名字 → 名字对象不在该区的 NPC（如 Npc_HatenoVillage014 茨琪米）
// hash 未知 → 反查不到 → 地图缺失。
//
// 本模块：启动时 + 每 60s 全内存扫一次名字对象（低频、后台），结果原子缓存；
// 每帧扫描只反查 0x016B 实例区，不再逐帧扫名字。

package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	actorInfoMu    sync.RWMutex
	actorHashName  map[uint32]string // hash → actor 名
	actorInfoAge   time.Time
	actorInfoReady bool
)

// refreshActorInfo 全内存（全部已提交块）收集 Npc_ 名字对象的 hash。
// 完成前不替换旧缓存（首次为 nil 时扫描线程自旋等待）。
func refreshActorInfo(h uintptr) {
	t0 := time.Now()
	blocks := guestBlocks(h, minBlockMB)
	infos := make(map[uint32]string)
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, n := range scanActorStringsRange(h, b.Base, b.Base+b.Size) {
			if !strings.HasPrefix(n.Name, "Npc_") {
				continue // 只要 NPC
			}
			if strings.ContainsAny(n.Name, ":/") {
				continue // 资源路径（.msbt/.sbfres 等）不是 actor 名
			}
			if seen[n.Name] {
				continue
			}
			buf := readMem(h, n.Addr, 0x60)
			if buf == nil {
				continue
			}
			hv := *(*uint32)(unsafe.Pointer(&buf[0x4C]))
			if hv < 0x34400000 || hv >= 0x34500000 {
				continue // 不是 ActorInfo（普通内嵌字符串），不占坑，继续找同名副本
			}
			seen[n.Name] = true
			infos[hv] = n.Name
		}
	}
	actorInfoMu.Lock()
	actorHashName = infos
	actorInfoAge = time.Now()
	actorInfoReady = true
	actorInfoMu.Unlock()
	fmt.Printf("[actorinfo] refreshed: %d Npc hashes in %s\n", len(infos), time.Since(t0).Round(time.Millisecond))
}

// actorHashSet 返回当前缓存 hash 集合 + 副本（读锁内拷贝引用即可，map 只读）。
func actorHashSnapshot() (map[uint32]string, bool) {
	actorInfoMu.RLock()
	defer actorInfoMu.RUnlock()
	if !actorInfoReady {
		return nil, false
	}
	return actorHashName, true
}

// needActorInfoRefresh 缓存过期（>60s）或未就绪时返回 true。
func needActorInfoRefresh() bool {
	actorInfoMu.RLock()
	defer actorInfoMu.RUnlock()
	if !actorInfoReady {
		return true
	}
	return time.Since(actorInfoAge) > 60*time.Second
}
