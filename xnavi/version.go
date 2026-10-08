// xnavi 版本号 — 三合一统一版本（v3.0.0 起步，2026-10-07 拍板）。
//
// 版本统一规则（用户确认）：导航核心（xnavi-core）与 GUI 壳（xnavi-gui）
// 共享同一版本号 v3.0.0，不再各自解耦。旧程序（navicemu / BOTWNavi-Eden /
// live-go BotwNavi）已废弃归档，不再维护独立版本。
package main

// CoreVersion 核心程序版本（GUI 侧 guiVersion 必须与此一致，见 xnavi-gui/app.go）。
const CoreVersion = "v3.0.1"
