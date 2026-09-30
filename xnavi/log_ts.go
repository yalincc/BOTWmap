// log_ts.go — 所有 stdout 输出统一加时间戳前缀。
//
// 背景：GUI 前端按 "YYYY-MM-DD HH:mm:ss.SSS" 前缀解析 core 日志（App.vue 正则），
// core 原本裸输出导致 GUI 时间列显示 "--:--:--"，且诊断包无时间线。
// 方案：包装 os.Stdout，每行行首自动插入时间戳，所有打印点零改动。
// 格式与 GUI 正则匹配：2006-01-02 15:04:05.000 （GUI 取 slice(11) 显示 HH:mm:ss.SSS）。
package main

import (
	"io"
	"os"
	"sync"
	"time"
)

type tsWriter struct {
	w           io.Writer
	mu          sync.Mutex
	atLineStart bool
}

func (t *tsWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []byte
	for _, b := range p {
		if t.atLineStart {
			out = append(out, time.Now().Format("2006-01-02 15:04:05.000 ")...)
			t.atLineStart = false
		}
		out = append(out, b)
		if b == '\n' {
			t.atLineStart = true
		}
	}
	if _, err := t.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// enableLogTimestamps 在 main 最开头调用，让 fmt.Printf/Println 全部带时间戳。
// os.Stdout 是 *os.File 具体类型无法直接替换，用 pipe 中转：
// os.Stdout → pipe 写端 → 后台协程读 → 逐行加时间戳 → 写回真实 stdout（GUI 重定向的目标）。
func enableLogTimestamps() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	real := os.Stdout
	os.Stdout = w
	go func() {
		ts := &tsWriter{w: real, atLineStart: true}
		buf := make([]byte, 8192)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				ts.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
}
