package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// 日志必须原地截断：inode 不变，正在写入的进程空间才会真正释放。
func TestTruncateFileKeepTailInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mihomo.log")
	tail := bytes.Repeat([]byte("B"), 1024)
	prefix := bytes.Repeat([]byte("A"), (2<<20)-len(tail))
	if err := os.WriteFile(path, append(prefix, tail...), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	inoBefore := before.Sys().(*syscall.Stat_t).Ino

	freed, err := truncateFileKeepTail(path, int64(len(tail)))
	if err != nil {
		t.Fatalf("截断失败: %v", err)
	}
	wantFreed := before.Size() - int64(len(tail))
	if freed != wantFreed {
		t.Fatalf("释放字节数 %d，期望 %d", freed, wantFreed)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("截断后 stat 失败: %v", err)
	}
	inoAfter := after.Sys().(*syscall.Stat_t).Ino
	if inoBefore != inoAfter {
		t.Fatalf("inode 变了（%d → %d），说明用了 rename", inoBefore, inoAfter)
	}
	if after.Size() != int64(len(tail)) {
		t.Fatalf("截断后大小 %d，期望 %d", after.Size(), len(tail))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !bytes.Equal(got, tail) {
		t.Fatal("保留的不是文件末尾内容")
	}
	if fileExists(path + ".tail") {
		t.Fatal("留下了 .tail 临时文件")
	}
}

func TestTruncateFileKeepTailSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.log")
	content := []byte("short")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	freed, err := truncateFileKeepTail(path, 1024)
	if err != nil {
		t.Fatalf("截断失败: %v", err)
	}
	if freed != 0 {
		t.Fatalf("小文件不应释放空间，实际 %d", freed)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, content) {
		t.Fatal("小文件内容被改动")
	}
}
