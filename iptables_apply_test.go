package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:1234": true,
		"[::1]:1234":     true,
		"192.168.1.2:80": false,
		"10.0.0.1:80":    false,
	}
	for addr, want := range cases {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestIptablesApplyAuthLoopbackOnly(t *testing.T) {
	app := &App{}
	app.settings.PanelPassword = "secret"

	called := false
	h := app.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// 本机防火墙钩子：不应要求登录
	req := httptest.NewRequest("POST", "/api/iptables/apply", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("loopback 请求被拦截：code=%d called=%v", rec.Code, called)
	}

	// 局域网来源：仍必须登录
	called = false
	req = httptest.NewRequest("POST", "/api/iptables/apply", nil)
	req.RemoteAddr = "192.168.1.2:5555"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("非回环请求未被鉴权：code=%d called=%v", rec.Code, called)
	}
}
