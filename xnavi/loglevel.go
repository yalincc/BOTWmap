// loglevel.go — 核心日志等级约定。
//
// 定位相关日志统一三级，便于一眼判断状态：
//
//	[ok]    成功取得定位（完全锁定、信标生效、进出神庙锚定等）
//	[info]  常规过程（扫描、探测、传送、静止保持、管道启动等）
//	[warn]  降级或告警（信标不可用回退、未装模组、锁定失效重扫、模拟器断开等）
//
// 内存探测与模组注入两条路径使用同一套等级与措辞，便于横向对照。

package main

import "fmt"

func logOK(format string, args ...any) {
	fmt.Printf("  [ok] "+format+"\n", args...)
}

func logInfo(format string, args ...any) {
	fmt.Printf("  [info] "+format+"\n", args...)
}

func logWarn(format string, args ...any) {
	fmt.Printf("  [warn] "+format+"\n", args...)
}
