package main

import (
	"errors"
	"testing"
)

func TestClassifyReachStatus(t *testing.T) {
	cases := []struct {
		code int
		err  error
		want string
	}{
		{204, nil, "ok"},
		{200, nil, "ok"},
		{302, nil, "ok"},
		{401, nil, "restricted"},
		{403, nil, "restricted"},
		{451, nil, "restricted"},
		{500, nil, "fail"},
		{0, errors.New("dial tcp: i/o timeout"), "timeout"},
		{0, errors.New("context deadline exceeded"), "timeout"},
		{0, errors.New("connection refused"), "fail"},
	}
	for _, c := range cases {
		if got := classifyReachStatus(c.code, c.err); got != c.want {
			t.Errorf("code=%d err=%v 期望 %s，实际 %s", c.code, c.err, c.want, got)
		}
	}
}

// 内核未运行时：七个目标全部 unknown，且接口仍 200
func TestReachabilityUnknownWhenEngineDown(t *testing.T) {
	a := newTestApp(t)
	data := a.collectReachability()
	if len(data) != len(reachabilityTargets) {
		t.Fatalf("目标数不对: %d", len(data))
	}
	for _, d := range data {
		if d.Status != "unknown" {
			t.Errorf("%s 应为 unknown，实际 %s", d.ID, d.Status)
		}
		if d.Err == "" {
			t.Errorf("%s 应带错误说明", d.ID)
		}
	}
	t.Logf("✓ 内核未运行时 %d 个目标均为 unknown", len(data))
}

// 关闭连接的接口在内核未运行时返回 ok=false 而不是 5xx
func TestCloseConnectionWhenEngineDown(t *testing.T) {
	e := newE2E(t)
	var r struct {
		OK  bool   `json:"ok"`
		Err string `json:"err"`
	}
	if code := e.call("DELETE", "/api/connections", nil, &r); code != 200 || r.OK || r.Err == "" {
		t.Fatalf("关闭全部连接应返回 200 + ok=false：code=%d %+v", code, r)
	}
	if code := e.call("DELETE", "/api/connections/abc", nil, &r); code != 200 || r.OK {
		t.Fatalf("关闭单条连接应返回 200 + ok=false：code=%d %+v", code, r)
	}
	t.Logf("✓ 内核未运行时关闭连接返回 ok=false（%s）", r.Err)
}
