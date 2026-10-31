package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// /api/status 在 TTL 内不应重复请求内核 /version 与 /configs。
func TestStatusCoreCacheTTL(t *testing.T) {
	a := newBackendTestApp(t)

	var versionCalls, configCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			versionCalls.Add(1)
			_, _ = w.Write([]byte(`{"version":"v-test"}`))
		case "/configs":
			configCalls.Add(1)
			_, _ = w.Write([]byte(`{"mode":"rule","mixed-port":7890,"redir-port":3333}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a.clash = NewClashClient(srv.Listener.Addr().String(), "")
	a.engine.running = true

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		a.handleStatus(rec, httptest.NewRequest("GET", "/api/status", nil))
		if rec.Code != 200 {
			t.Fatalf("status 返回 %d", rec.Code)
		}
	}
	if got := versionCalls.Load(); got != 1 {
		t.Fatalf("/version 被调用 %d 次，TTL 内期望 1 次", got)
	}
	if got := configCalls.Load(); got != 1 {
		t.Fatalf("/configs 被调用 %d 次，TTL 内期望 1 次", got)
	}

	// 过期后应重新拉取。
	a.coreMu.Lock()
	a.coreVersionAt = time.Now().Add(-2 * time.Minute)
	a.coreCfgAt = time.Now().Add(-11 * time.Second)
	a.coreMu.Unlock()

	rec := httptest.NewRecorder()
	a.handleStatus(rec, httptest.NewRequest("GET", "/api/status", nil))
	if got := versionCalls.Load(); got != 2 {
		t.Fatalf("/version 过期后调用 %d 次，期望 2 次", got)
	}
	if got := configCalls.Load(); got != 2 {
		t.Fatalf("/configs 过期后调用 %d 次，期望 2 次", got)
	}
}
