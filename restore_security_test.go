package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testProxy struct {
	*httptest.Server
	Port int
}

func newTestProxyServer(t *testing.T, body []byte) *testProxy {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	t.Cleanup(srv.Close)
	return &testProxy{Server: srv, Port: port}
}

// 备份里的订阅 ID 不能用来做路径穿越；只允许写入备份里真实存在的订阅。
func TestRestoreRejectsPathTraversalSubID(t *testing.T) {
	a := newBackendTestApp(t)

	bundle := &BackupBundle{
		Kind:     backupKind,
		Format:   1,
		Settings: json.RawMessage(`{}`),
		Subs: []Sub{
			{ID: "../../evil", Name: "evil"},
			{ID: "s_ok", Name: "ok"},
		},
		Providers: map[string]string{
			"../../evil":  `{"proxies":[]}`,
			"s_ok":        `{"proxies":[]}`,
			"not_in_subs": `{"proxies":[]}`,
		},
	}
	if err := a.restoreBackup(bundle); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}

	escaped := filepath.Join(a.dataDir, "..", "evil.json")
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Fatalf("路径穿越文件被写入: %s", escaped)
	}
	if _, err := os.Stat(a.providerPath("not_in_subs")); !os.IsNotExist(err) {
		t.Fatal("不在订阅列表里的 provider 被写入")
	}
	if _, err := os.Stat(a.providerPath("s_ok")); err != nil {
		t.Fatalf("合法 provider 未写入: %v", err)
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.subs) != 2 {
		t.Fatalf("订阅数 %d，期望 2", len(a.subs))
	}
	for _, s := range a.subs {
		if !validSubID(s.ID) {
			t.Fatalf("恢复后仍存在非法 ID: %q", s.ID)
		}
	}
}

// 通过本机代理成功拉取订阅时，日志里的 URL 必须打码。
func TestProxyFetchLogMasksToken(t *testing.T) {
	a := newBackendTestApp(t)

	proxy := newTestProxyServer(t, []byte(slowClashSub))
	port := proxy.Port
	a.settings.MixedPort = port
	a.settings.SubProxyMode = "proxy"

	rawURL := "http://example.invalid/sub?token=SECRET-TOKEN"
	if _, _, err := a.fetchSubscription(rawURL, "测试"); err != nil {
		t.Fatalf("通过代理拉取失败: %v", err)
	}

	a.hub.lmu.RLock()
	var joined []string
	for _, e := range a.hub.logs {
		joined = append(joined, e.Payload)
	}
	a.hub.lmu.RUnlock()
	logText := strings.Join(joined, "\n")
	if strings.Contains(logText, "SECRET-TOKEN") {
		t.Fatalf("日志泄露了订阅 token：\n%s", logText)
	}
	if !strings.Contains(logText, "***") {
		t.Fatalf("日志里没有打码后的订阅地址：\n%s", logText)
	}
}
