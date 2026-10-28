package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 无主的节点文件与原始缓存应被清掉，在用的必须保留
func TestCleanupRemovesOrphans(t *testing.T) {
	a := newTestApp(t)
	a.subs = []Sub{{ID: "s_keep", Name: "在用"}}

	mk := func(rel string, size int) string {
		p := filepath.Join(a.dataDir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	keepProv := mk("providers/s_keep.json", 1024)
	deadProv := mk("providers/s_dead.json", 4096)
	keepCache := mk("cache/s_keep.sub", 512)
	deadCache := mk("cache/s_dead.sub", 8192)
	tmpFile := mk("rules/chnroute.txt.tmp", 100)

	freed, steps := a.CleanupStorage()

	for _, p := range []string{deadProv, deadCache, tmpFile} {
		if fileExists(p) {
			t.Errorf("应删除却仍存在: %s", p)
		}
	}
	for _, p := range []string{keepProv, keepCache} {
		if !fileExists(p) {
			t.Errorf("不应删除却在清掉: %s", p)
		}
	}
	if freed < 4096+8192 {
		t.Errorf("释放量偏小: %d", freed)
	}
	if len(steps) < 3 {
		t.Errorf("清理明细过少: %v", steps)
	}
	t.Logf("✓ 清理无主文件：释放 %s，%d 项", humanBytes(freed), len(steps))
}

// 日志超限要截断，保留尾部
func TestCleanupTruncatesLogs(t *testing.T) {
	a := newTestApp(t)
	logDir := filepath.Join(a.dataDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	p := filepath.Join(logDir, "mihomo.log")
	big := strings.Repeat("x", int(maxLogBytes)+1024)
	if err := os.WriteFile(p, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	freed, _ := a.CleanupStorage()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > logKeepBytes+1024 {
		t.Errorf("日志没有截断干净: %d", fi.Size())
	}
	if freed < int64(maxLogBytes)/2 {
		t.Errorf("释放量偏小: %d", freed)
	}
	t.Logf("✓ 日志截断：剩 %s", humanBytes(fi.Size()))
}

// 内核备份只保留 1 份
func TestCleanupKeepsOneBackup(t *testing.T) {
	a := newTestApp(t)
	dir := a.kernelDir()
	_ = os.MkdirAll(dir, 0o755)
	names := []string{"mihomo.backup-100", "mihomo.backup-200", "mihomo.backup-300"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), make([]byte, 2048), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.CleanupStorage()

	left := 0
	for _, n := range names {
		if fileExists(filepath.Join(dir, n)) {
			left++
		}
	}
	if left != 1 {
		t.Fatalf("应只保留 1 份备份，实际 %d", left)
	}
	if !fileExists(filepath.Join(dir, "mihomo.backup-300")) {
		t.Errorf("应保留最新的那一份")
	}
	t.Logf("✓ 内核备份保留 1 份")
}

// 存储报告要能统计出各项占用
func TestStorageReport(t *testing.T) {
	a := newTestApp(t)
	a.subs = []Sub{{ID: "s_keep", Name: "在用"}}
	_ = os.MkdirAll(filepath.Join(a.dataDir, "providers"), 0o755)
	_ = os.WriteFile(filepath.Join(a.dataDir, "providers", "s_keep.json"), make([]byte, 1234), 0o644)

	rep := a.storageReport()
	if rep.Total < 1234 {
		t.Errorf("总占用统计偏小: %d", rep.Total)
	}
	if rep.Free <= 0 {
		t.Errorf("剩余空间读取失败: %d", rep.Free)
	}
	var found bool
	for _, it := range rep.Items {
		if it.Name == "节点文件" && it.Bytes >= 1234 {
			found = true
		}
	}
	if !found {
		t.Errorf("缺少节点文件统计: %+v", rep.Items)
	}
	t.Logf("✓ 存储报告：共 %s，剩余 %s", humanBytes(rep.Total), humanBytes(rep.Free))
}
