package main

import (
	"testing"
)

// 有订阅但配置残缺时，同样回退到最小配置而不是报「没有订阅」
func TestDryRunFallsBackWhenConfigUnavailable(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.subs = nil
	a.mu.Unlock()

	if err := a.appReloadDryRun(a.mihomoPath); err != nil {
		t.Fatalf("appReloadDryRun 应回退到最小配置并成功，实际：%v", err)
	}
	t.Log("✓ 无订阅时 dry-run 使用最小配置通过")
}
