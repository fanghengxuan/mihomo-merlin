package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------- 通用工具 ----------

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// run 带超时执行命令，返回合并后的输出。
// 超时后必须杀掉子进程（含其子进程组），否则 `mihomo -t`、iptables 等
// 子进程会一直挂在路由器上，反复调用后耗尽内存。
func run(timeout time.Duration, name string, args ...string) (string, error) {
	c := command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	select {
	case err := <-done:
		return buf.String(), err
	case <-time.After(timeout):
		killProcessGroup(c)
		select {
		case <-done:
			return buf.String(), fmt.Errorf("执行超时（%s）", timeout)
		case <-time.After(2 * time.Second):
			// 进程仍未回收，不再读取 buf（可能与 copy goroutine 竞争）。
			return "", fmt.Errorf("执行超时（%s），进程未能退出", timeout)
		}
	}
}

func killProcessGroup(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	pid := c.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = c.Process.Kill()
	}
}

// tail 截取日志尾部，便于回传前端。
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func nvramGet(key string) string {
	if _, err := os.Stat("/usr/sbin/nvram"); err != nil {
		if _, err2 := os.Stat("/sbin/nvram"); err2 != nil {
			return ""
		}
	}
	out, err := run(3*time.Second, "nvram", "get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// memUsageMB 从 /proc/<pid>/status 取 VmRSS。
func memUsageMB(pid int) float64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb int
			if _, err := fmt.Sscanf(line, "VmRSS: %d kB", &kb); err == nil {
				return float64(kb) / 1024.0
			}
		}
	}
	return 0
}

// lanSubnet 猜测局域网网段，用于 iptables 放行。
func lanSubnet() []string {
	var out []string
	if ip := nvramGet("lan_ipaddr"); ip != "" {
		if net.ParseIP(ip).To4() != nil {
			parts := strings.Split(ip, ".")
			if len(parts) == 4 {
				out = append(out, fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2]))
			}
		}
	}
	if len(out) == 0 {
		out = append(out, "192.168.50.0/24")
	}
	return out
}

// ringBuffer 保存最近 N 条日志。
type ringBuffer struct {
	mu   sync.Mutex
	max  int
	data []string
}

func newRingBuffer(n int) *ringBuffer { return &ringBuffer{max: n} }

func (r *ringBuffer) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, s)
	if len(r.data) > r.max {
		r.data = r.data[len(r.data)-r.max:]
	}
}

func (r *ringBuffer) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.data))
	copy(out, r.data)
	return out
}

// copyFile 简单文件复制（用于安装脚本里的备份场景）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// command 构造 exec.Cmd（保留环境变量，便于在路由器上复用 PATH）。
func command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// removeIfExists 删除文件，不存在时忽略。
func removeIfExists(p string) error {
	err := os.Remove(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// humanBytes 把字节数变成可读文本，用于进度提示。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
