// Package gdbstub 是最小化的 GDB Remote Serial Protocol 客户端。
//
// ⚠️ 用途边界（重要）：Enavi 定位**不用** GDB Stub 做实时读取通道。
// 原因是 yuzu 系实现的 stub 在处理请求时会挂起被调试的 guest CPU，
// 10Hz 读取等于每 100ms 冻一次游戏（M0-R1 实测项，待用本包测响应延迟佐证）。
// 本包只在 M0 阶段做三件事：
//   1. 确认 Eden 的 stub 是否真的开了（TCP 是否可连）；
//   2. 读出它支持的包（qSupported），确认能不能 m 读内存；
//   3. 量一次往返延迟，作为"能不能当实时通道"的证据。
package gdbstub

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Client 一个 RSP 连接。
type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial 连接 GDB Stub（Eden 默认 localhost:6543）。
func Dial(addr string, timeout time.Duration) (*Client, error) {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c, r: bufio.NewReader(c)}, nil
}

// Close 断开。
func (c *Client) Close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

// Send 发送一个 RSP 包并等待应答，返回应答 payload（去掉了 $ #xx 与 ack）。
func (c *Client) Send(payload string, timeout time.Duration) (string, error) {
	if err := c.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	pkt := "$" + payload + "#" + checksum(payload)
	if _, err := c.conn.Write([]byte(pkt)); err != nil {
		return "", fmt.Errorf("写包失败: %w", err)
	}
	return c.readPacket()
}

// readPacket 跳过 +/- ack，读一个 $...#xx 包。
func (c *Client) readPacket() (string, error) {
	// 先吃掉 ack
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			return "", fmt.Errorf("读应答失败: %w", err)
		}
		if b == '+' {
			continue
		}
		if b == '-' {
			return "", fmt.Errorf("对端返回 NAK（包格式被拒）")
		}
		if b == '$' {
			break
		}
		// 其它字节（部分实现会先发通知包），忽略
	}
	var sb strings.Builder
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			return "", fmt.Errorf("读包体失败: %w", err)
		}
		if b == '#' {
			break
		}
		sb.WriteByte(b)
	}
	// 吃掉两字节校验
	ck := make([]byte, 2)
	if _, err := c.r.Read(ck); err != nil {
		return "", fmt.Errorf("读校验失败: %w", err)
	}
	// 回 ack
	_, _ = c.conn.Write([]byte{'+'})
	return sb.String(), nil
}

// MemRead 读 guest 内存（m 包），返回原始字节。
// 若对端返回错误包（E01 等），原样作为 error 回传，便于台账记录。
func (c *Client) MemRead(gva uintptr, n int, timeout time.Duration) ([]byte, error) {
	reply, err := c.Send(fmt.Sprintf("m%x,%x", gva, n), timeout)
	if err != nil {
		return nil, err
	}
	if reply == "" || strings.HasPrefix(reply, "E") {
		return nil, fmt.Errorf("对端返回错误包 %q", reply)
	}
	return hex.DecodeString(reply)
}

func checksum(s string) string {
	var sum byte
	for i := 0; i < len(s); i++ {
		sum += s[i]
	}
	return fmt.Sprintf("%02x", sum)
}

// Probe 跑一遍 M0 探测，返回可打印的结论行。
func Probe(addr string, gva uintptr, timeout time.Duration) []string {
	var out []string
	t0 := time.Now()
	c, err := Dial(addr, timeout)
	if err != nil {
		return append(out, fmt.Sprintf("[gdb] 连接 %s 失败: %v（Eden: General > Debug > Enable GDB Stub）", addr, err))
	}
	defer c.Close()
	out = append(out, fmt.Sprintf("[gdb] TCP 连接成功 %s（%v）", addr, time.Since(t0).Round(time.Millisecond)))

	if r, err := c.Send("qSupported:multiprocess+;swbreak+;hwbreak+", timeout); err == nil {
		out = append(out, "[gdb] qSupported → "+clip(r, 200))
	} else {
		out = append(out, "[gdb] qSupported 失败: "+err.Error())
	}
	if r, err := c.Send("?", timeout); err == nil {
		out = append(out, "[gdb] ? （停止原因）→ "+clip(r, 80))
	} else {
		out = append(out, "[gdb] ? 失败: "+err.Error())
	}

	// 往返延迟：连续 5 次 m 读，取中位数 —— 判断能否当实时通道的关键证据
	if gva != 0 {
		var ds []time.Duration
		for i := 0; i < 5; i++ {
			t := time.Now()
			if _, err := c.MemRead(gva, 16, timeout); err != nil {
				out = append(out, fmt.Sprintf("[gdb] m%x,10 失败: %v", gva, err))
				break
			}
			ds = append(ds, time.Since(t))
		}
		if len(ds) > 0 {
			out = append(out, fmt.Sprintf("[gdb] m 包往返 %d 次: %v（含 guest CPU 停顿，≥5ms 即不适合 10Hz）", len(ds), ds))
		}
	} else {
		if r, err := c.Send("m0,10", timeout); err == nil {
			out = append(out, "[gdb] m0,10 → "+clip(r, 80))
		} else {
			out = append(out, "[gdb] m0,10 失败（地址非法属正常，只证明通道可用）: "+err.Error())
		}
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(共" + strconv.Itoa(len(s)) + "字符)"
}
