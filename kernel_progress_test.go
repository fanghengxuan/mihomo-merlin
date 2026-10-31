package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 下载大文件时应按块回调进度（用于进度条），并拿到完整内容
func TestGetWithFallbackProgressReports(t *testing.T) {
	payload := make([]byte, 300*1024) // 300KB
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	orig := kernelMirrors
	defer func() { kernelMirrors = orig }()
	kernelMirrors = nil

	a := newTestApp(t)
	var calls int
	var lastRead int64
	b, _, err := a.getWithFallbackProgress(srv.URL, 1<<20, 5*time.Second, func(read, total int64) {
		calls++
		lastRead = read
	})
	if err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	if len(b) != len(payload) {
		t.Fatalf("内容长度不对：%d vs %d", len(b), len(payload))
	}
	if calls == 0 {
		t.Fatal("没有回调任何进度")
	}
	if lastRead != int64(len(payload)) {
		t.Fatalf("最终读到 %d，应为 %d", lastRead, len(payload))
	}
	t.Logf("✓ 下载进度回调 %d 次，最后 %d 字节", calls, lastRead)
}
