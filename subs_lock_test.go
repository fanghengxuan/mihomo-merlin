package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const slowClashSub = `proxies:
  - {name: 慢节点, type: ss, server: 127.0.0.1, port: 8388, cipher: aes-128-gcm, password: x}
`

// slowSubServer 收到请求后先通知 entered，再挂起 delay 返回订阅。
func slowSubServer(t *testing.T, delay time.Duration, entered chan<- struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		time.Sleep(delay)
		_, _ = w.Write([]byte(slowClashSub))
	}))
}

// 订阅刷新在网络上等待时，/api/subs 必须立即返回，而不是被 a.mu 挡住。
func TestListSubsNotBlockedByRefresh(t *testing.T) {
	a := newBackendTestApp(t)
	entered := make(chan struct{}, 1)
	srv := slowSubServer(t, 800*time.Millisecond, entered)
	defer srv.Close()

	a.subs = []Sub{{ID: "s_slow", Name: "慢订阅", URL: srv.URL, Enabled: true, Status: "never"}}

	done := make(chan error, 1)
	go func() { done <- a.refreshSubByID("s_slow") }()

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("刷新请求未到达测试服务器")
	}

	start := time.Now()
	rec := httptest.NewRecorder()
	a.handleListSubs(rec, httptest.NewRequest("GET", "/api/subs", nil))
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("/api/subs 被刷新阻塞了 %v，应 <200ms", elapsed)
	}
	if rec.Code != 200 {
		t.Fatalf("/api/subs 状态码 %d", rec.Code)
	}
	if err := <-done; err != nil {
		t.Fatalf("刷新失败: %v", err)
	}
}

// 删除与刷新并发时，删除必须等同一条订阅的刷新结束，并清掉 provider 文件。
func TestDeleteWaitsRefreshAndRemovesProvider(t *testing.T) {
	a := newBackendTestApp(t)
	entered := make(chan struct{}, 1)
	srv := slowSubServer(t, 500*time.Millisecond, entered)
	defer srv.Close()

	a.subs = []Sub{{ID: "s_del", Name: "待删除", URL: srv.URL, Enabled: true, Status: "never"}}

	refreshDone := make(chan error, 1)
	go func() { refreshDone <- a.refreshSubByID("s_del") }()

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("刷新请求未到达测试服务器")
	}

	delDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest("DELETE", "/api/subs/s_del", nil)
		req.SetPathValue("id", "s_del")
		rec := httptest.NewRecorder()
		a.handleDeleteSub(rec, req)
		delDone <- rec.Code
	}()

	<-refreshDone
	code := <-delDone
	if code != 200 {
		t.Fatalf("删除返回 %d", code)
	}
	if _, err := os.Stat(a.providerPath("s_del")); !os.IsNotExist(err) {
		t.Fatalf("删除后 provider 文件仍存在: %v", err)
	}
}

// 并发读订阅列表与修改全局间隔不应有数据竞争（subGlobalInterval 已移除）。
func TestListSubsConcurrentSettingsNoRace(t *testing.T) {
	a := newBackendTestApp(t)
	a.subs = []Sub{{ID: "s_1", Name: "A", Interval: 0, Enabled: true}}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			a.handleListSubs(rec, httptest.NewRequest("GET", "/api/subs", nil))
		}()
		go func(n int) {
			defer wg.Done()
			a.mu.Lock()
			a.settings.SubInterval = n % 24
			a.mu.Unlock()
		}(i)
	}
	wg.Wait()
}

// 前端若把脱敏 URL（含 ***）回传，后端应视为未修改，不能覆盖真实地址。
func TestUpdateSubIgnoresMaskedURL(t *testing.T) {
	a := newBackendTestApp(t)
	realURL := "https://example.com/sub?token=secret"
	a.subs = []Sub{{ID: "s_mask", Name: "掩码测试", URL: realURL, Enabled: true, Status: "ok"}}

	body := `{"url":"https://example.com/sub?token=***"}`
	req := httptest.NewRequest("PUT", "/api/subs/s_mask", strings.NewReader(body))
	req.SetPathValue("id", "s_mask")
	rec := httptest.NewRecorder()
	a.handleUpdateSub(rec, req)

	if rec.Code != 200 {
		t.Fatalf("更新返回 %d：%s", rec.Code, rec.Body.String())
	}
	a.mu.RLock()
	got := a.subs[0].URL
	a.mu.RUnlock()
	if got != realURL {
		t.Fatalf("脱敏 URL 覆盖了真实地址：%q", got)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
}
