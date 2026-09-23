// module: live_float
// status: M2 最小窗口验证
// WebView2 嵌入：edge.NewChromium + Embed(hwnd) → Navigate(float.html)。

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

func embedWebView(f *floatWindow, hwnd uintptr) {
	c := edge.NewChromium()
	c.SetErrorCallback(func(err error) {
		log.Printf("[WebView2] 错误: %v", err)
	})
	c.DataPath = userDataPath()
	f.chrome = c

	if !c.Embed(hwnd) {
		log.Printf("[WebView2] Embed 失败，浮窗无法显示")
		return
	}
	log.Printf("[WebView2] 环境+控制器就绪")

	c.Navigate(fmt.Sprintf("http://127.0.0.1:%d/float.html", SrvPort))
	c.SetSize(edge.Rect{Left: 0, Top: 0, Right: f.w, Bottom: f.h})
	// 透明默认背景：页面 body opacity 半透明时露出桌面（而非黑/白底），实现整体透明度
	c.SetBackgroundColour(0, 0, 0, 0)
	_ = c.Show()
}

func resizeWebView(c *edge.Chromium, w, h int32) {
	if c == nil {
		return
	}
	c.SetSize(edge.Rect{Left: 0, Top: 0, Right: w, Bottom: h})
}

func userDataPath() string {
	return filepath.Join(os.Getenv("AppData"), "LiveFloatNavi")
}
