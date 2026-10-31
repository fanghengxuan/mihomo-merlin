package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunNormalCommand(t *testing.T) {
	out, err := run(5*time.Second, "sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("正常命令失败: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("输出不含 hello：%q", out)
	}
}

func TestRunTimeoutReturnsPromptly(t *testing.T) {
	start := time.Now()
	_, err := run(200*time.Millisecond, "sh", "-c", "sleep 10")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("超时命令应返回错误")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("错误信息应说明超时：%v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("超时后 %v 才返回，说明没有及时杀掉进程", elapsed)
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := fmt.Sprintf("echo $$ > %q; sleep 30", pidFile)

	_, err := run(300*time.Millisecond, "sh", "-c", script)
	if err == nil {
		t.Fatal("超时命令应返回错误")
	}

	b, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("读取子进程 pid 失败: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
	if convErr != nil {
		t.Fatalf("pid 非法: %v", convErr)
	}
	// 进程组被杀后，pid 不应再存在。
	if killErr := syscall.Kill(pid, 0); killErr == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("超时后子进程 %d 仍然存活", pid)
	}
}
