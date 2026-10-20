// 本文件 = 用 Eden 的 GDB Stub 拿「模块加载表 / 内存映射表」。
//
// 为什么需要：第一轮实测发现 Eden 内存里同一个 NSO 有多份副本，
// 映射 gva = NSO文件偏移 + K 的 K 随副本而变 → 只靠内存扫描钉不死基址。
// 而 GDB Stub 能直接问 Eden「你加载了哪些模块、各自什么地址」——这是权威答案。
//
// 顺带说明：GDB Stub 开启后 Eden 会**停在断点等调试器**（游戏表现为卡在加载画面），
// 所以这只能当「离线校准工具」用，不能留在生产路径上。
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"botwmap/enavi/internal/gdbstub"
)

// cmdGdbInfo 拿模块表 + 内存映射表。
func cmdGdbInfo() {
	hr("GDB Stub：模块表 / 内存映射表")

	addr := *gdbAddr
	timeout := *timeout
	c, err := gdbstub.Dial(addr, timeout)
	if err != nil {
		printf("[info] 连接 %s 失败：%v\n", addr, err)
		return
	}
	defer c.Close()
	printf("[info] 已连接 %s\n\n", addr)

	// 1) 线程列表（读内存要用 pXX 指定线程）
	if r, err := c.Send("qfThreadInfo", timeout); err == nil {
		printf("[info] qfThreadInfo → %s\n", clipLine(r, 300))
	}
	thr := "1"
	if r, err := c.Send("qfThreadInfo", timeout); err == nil {
		// 形如 m1,2,3 或 l1
		s := strings.TrimSpace(r)
		if strings.HasPrefix(s, "m") {
			s = s[1:]
		} else if strings.HasPrefix(s, "l") {
			s = s[1:]
		}
		if i := strings.IndexByte(s, ','); i > 0 {
			s = s[:i]
		}
		if s != "" {
			thr = s
		}
	}
	printf("[info] 使用线程 %s\n\n", thr)

	// 2) 库 / 模块表
	printf("──── 模块表 (qXfer:libraries) ────\n")
	if r, err := c.Send("qXfer:libraries:read:0x1000;0x1000", timeout); err == nil {
		lines := parseXfer(r)
		if len(lines) == 0 {
			printf("  （空）%s\n", clipLine(r, 200))
		}
		for _, l := range lines {
			printf("  %s\n", l)
		}
	} else {
		printf("  失败：%v\n", err)
	}

	// 3) 内存映射表
	printf("\n──── 内存映射 (qXfer:memory:read:;;) ────\n")
	if r, err := c.Send("qXfer:memory:read::;0x4000", timeout); err == nil {
		lines := parseXfer(r)
		if len(lines) == 0 {
			printf("  （空）%s\n", clipLine(r, 200))
		}
		for i, l := range lines {
			if i >= 60 {
				printf("  ...（共 %d 行）\n", len(lines))
				break
			}
			printf("  %s\n", l)
		}
	} else {
		printf("  失败：%v\n", err)
	}

	// 4) 进程内命令：mo get info / mo get mappings
	for _, cmd := range []string{"mo get info", "mo get mappings"} {
		printf("\n──── %s ────\n", cmd)
		if r, err := c.Send(cmd, timeout); err == nil {
			for _, l := range splitLines(r) {
				if i := strings.IndexByte(l, '}'); i >= 0 {
					printf("  %s}\n", l[:i+1])
					continue
				}
				printf("  %s\n", clipLine(l, 200))
			}
		} else {
			printf("  失败：%v\n", err)
		}
	}

	// 5) 读线程寄存器（能看到 PC / SP / 通用寄存器 → 直接反映 guest 状态）
	printf("\n──── 线程寄存器 (g) ────\n")
	if r, err := c.Send(fmt.Sprintf("g%x", parseHexOr(thr, 1)), timeout); err == nil {
		for _, l := range splitLines(r) {
			printf("  %s\n", clipLine(l, 400))
		}
	} else {
		printf("  失败：%v\n", err)
	}

	// 6) 试探性读内存（必须带线程号 pXX，否则 E01）
	printf("\n──── 试探性读内存 ────\n")
	for _, gva := range []uintptr{0x7100000000, 0x140000000, 0x100000000, 0x800000000, 0x2000000000} {
		for _, form := range []string{"m%x,%x", "mp%x,%x,%x"} {
			var cmd string
			if form == "m%x,%x" {
				cmd = fmt.Sprintf("m%x,%x", gva, 16)
			} else {
				cmd = fmt.Sprintf("mp%x,%x,%x", parseHexOr(thr, 1), gva, 16)
			}
			r, err := c.Send(cmd, timeout)
			if err != nil {
				printf("  %s → 失败：%v\n", cmd, err)
				continue
			}
			if strings.HasPrefix(r, "E") {
				printf("  %-24s → %s（无映射）\n", cmd, r)
			} else {
				printf("  %-24s → %s ✓ 有映射\n", cmd, clipLine(r, 60))
			}
		}
	}
}

// parseXfer 解析 qXfer 的分块应答：首行是 "l<hex>"，其余是内容行，末行 "l"（结束）。
func parseXfer(reply string) []string {
	lines := splitLines(reply)
	if len(lines) == 0 {
		return nil
	}
	// 第一行是 "lXXXX"（长度），最后一行是 "l"
	if strings.HasPrefix(lines[0], "l") {
		lines = lines[1:]
	}
	var out []string
	for _, l := range lines {
		if l == "l" {
			break
		}
		out = append(out, l)
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseHexOr 把十进制线程号转成 GDB 用的十六进制（0x 前缀）。
func parseHexOr(s string, def int) int {
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return def
	}
	return int(v)
}

var _ = time.Now
