package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 内核 /traffic 只给速率，累计值来自 /connections —— hub 要做兜底
func TestTrafficTotalsFallback(t *testing.T) {
	a := newTestApp(t)
	a.hub.SetTotals(123456, 654321)

	last, _ := a.hub.LastTraffic()
	if last.UpTotal != 123456 || last.DownTotal != 654321 {
		t.Fatalf("累计值未合并：%+v", last)
	}
	t.Logf("✓ 累计值兜底：up=%d down=%d", last.UpTotal, last.DownTotal)
}

// 流量流必须把累计值带给前端（概览「累计」读的是 upTotal/downTotal）
func TestTrafficStreamIncludesTotals(t *testing.T) {
	e := newE2E(t)
	e.app.hub.SetTotals(111, 222)

	req := httptest.NewRequest("GET", "/api/traffic/stream", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		e.app.handleTraffic(rec, req)
		close(done)
	}()
	time.Sleep(300 * time.Millisecond)
	body := rec.Body.String()
	if !strings.Contains(body, `"upTotal"`) || !strings.Contains(body, "111") {
		t.Fatalf("流量流缺少累计值：%s", body)
	}
	t.Logf("✓ 流量流带累计值：%s", strings.Split(strings.TrimSpace(body), "\n")[0])
}
