// game_botw.go — BOTW 游戏语义（Eden 版）：神庙判据、位面。
//
// BOTW 与 TOTK 在 Eden 下的游戏层差异集中在本文件（阶段二抽 Game 接口时
// 直接平移）：坐标轴序在 locate.go decodePos、存档在 savefile_botw.go、
// 进度在 progress_botw.go、地图口径在 watch.go state 更新处。

package main

// botwShrineExit 神庙内部坐标上限：|X|,|Z| 超过此值 = 世界尺度，小于 = 神庙
// 内部（照 xnavi gameShrineExit=300 / live-cemu shrineExit=300，双端已验证）。
const botwShrineExit = 300.0

// isShrinePos 判定神庙/洞穴本地场景：BOTW 神庙为独立场景，玩家坐标切到以
// 神庙原点为中心的本地坐标系（|X|<300 && |Z|<300）。进入判定由状态机叠加
// "世界尺度单步跳变"条件（watch.go enteredShrine），避免海拉鲁中心原点带
// （|X|<300,|Z|<300 的合法世界坐标）被误判进神庙。
func isShrinePos(p [3]float32) bool {
	return abs32(p[0]) < botwShrineExit && abs32(p[2]) < botwShrineExit
}

// layerOf BOTW 无多层位面（地图是单层大地图），恒 0。
func layerOf(gx, gy, gz float32) int { return 0 }
