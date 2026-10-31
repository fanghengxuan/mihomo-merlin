package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 用户主动停过（没有 enabled 标记）时，看门狗不该把内核拉起来
func TestAutoRecoverSkippedWhenUserStopped(t *testing.T) {
	a := newTestApp(t)
	a.settings.AutoStart = true
	a.engine.maybeAutoRecover(time.Now())

	if st := a.engine.RecoverState(); st.Attempts != 0 {
		t.Fatalf("不该尝试恢复，实际 attempts=%d", st.Attempts)
	}
	if a.engine.Running() {
		t.Fatal("不该把内核拉起来")
	}
	t.Log("✓ 手动停止后不会被复活")
}

// 关掉「开机自启」后也不做自动恢复（用户明确不想要它常驻）
func TestAutoRecoverSkippedWhenAutoStartDisabled(t *testing.T) {
	a := newTestApp(t)
	setEngineWanted(a.dataDir, true)
	a.settings.AutoStart = false
	a.engine.maybeAutoRecover(time.Now())
	if st := a.engine.RecoverState(); st.Attempts != 0 {
		t.Fatalf("关了自启不该恢复，实际 attempts=%d", st.Attempts)
	}
	t.Log("✓ 关闭自启后不自动恢复")
}

// 期望运行却没在跑：尝试恢复，失败后记录错误并按退避推迟下次
func TestAutoRecoverAttemptsAndBackoff(t *testing.T) {
	a := newTestApp(t)
	setEngineWanted(a.dataDir, true)
	a.settings.AutoStart = true
	// 没有订阅 → Start 必然失败，正好验证失败分支
	now := time.Now()
	a.engine.maybeAutoRecover(now)

	st := a.engine.RecoverState()
	if st.Attempts != 1 {
		t.Fatalf("应尝试 1 次，实际 %d", st.Attempts)
	}
	if st.LastErr == "" {
		t.Error("失败时应记录错误原因")
	}
	if st.NextAt <= now.Unix() {
		t.Errorf("下次重试时间应晚于现在：%d vs %d", st.NextAt, now.Unix())
	}

	// 冷却期内再调一次不应重复尝试
	a.engine.maybeAutoRecover(now.Add(10 * time.Second))
	if got := a.engine.RecoverState().Attempts; got != 1 {
		t.Fatalf("冷却期内不应再次尝试，实际 %d", got)
	}

	// 过了退避时间再试一次 → 次数增加
	a.engine.maybeAutoRecover(now.Add(2 * time.Minute))
	if got := a.engine.RecoverState().Attempts; got != 2 {
		t.Fatalf("退避结束后应再次尝试，实际 %d", got)
	}
	t.Logf("✓ 自动恢复带退避：尝试 %d 次，错误=%s", gotAttempts(t, a), truncateStr(st.LastErr, 40))
}

func gotAttempts(t *testing.T, a *App) int {
	t.Helper()
	return a.engine.RecoverState().Attempts
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// enabled 标记的读写要能持久化（面板重启后据此决定是否自启）
func TestEngineWantedMarker(t *testing.T) {
	dir := t.TempDir()
	if engineWanted(dir) {
		t.Fatal("初始不应有标记")
	}
	setEngineWanted(dir, true)
	if !engineWanted(dir) {
		t.Fatal("写入后应读到标记")
	}
	if _, err := os.Stat(filepath.Join(dir, "enabled")); err != nil {
		t.Fatalf("标记文件不存在：%v", err)
	}
	setEngineWanted(dir, false)
	if engineWanted(dir) {
		t.Fatal("清除后不应再读到标记")
	}
	t.Log("✓ enabled 标记读写正确")
}

// 重启失败不能丢掉「期望运行」标记，否则再也不会被拉起
func TestRestartKeepsWantedOnFailure(t *testing.T) {
	a := newTestApp(t)
	setEngineWanted(a.dataDir, true) // 之前是在运行的

	// 没有订阅 → Start 必然失败
	if err := a.engine.Restart(); err == nil {
		t.Fatal("没有订阅时应该报错")
	}
	if !engineWanted(a.dataDir) {
		t.Fatal("重启失败后仍应保留运行意图（enabled 标记）")
	}
	if a.engine.Running() {
		t.Fatal("重启失败后不该处于运行状态")
	}
	t.Log("✓ 重启失败仍保留运行意图，看门狗可以继续拉起")
}
