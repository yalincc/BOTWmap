//go:build !windows

// appid_other.go — 非 Windows 平台的空实现（任务栏身份与图标是 Windows 特性）。

package main

func setAppUserModelID(id string) {}

func setWindowIcon() {}
