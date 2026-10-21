// Package version 版本与横幅（导航程序版本独立，不跟网页版）。
package version

import "fmt"

const (
	// Name 程序名。
	Name = "Enavi"
	// Version 语义与 xnavi 一致：主.板块.迭代；未发布不占位，M 阶段用后缀标注。
	Version = "0.1.1"
)

// Banner 单行横幅，日志第一行固定输出，便于认版本。
func Banner() string {
	return fmt.Sprintf("%s %s（Eden / BOTW 只读定位伴侣）", Name, Version)
}
