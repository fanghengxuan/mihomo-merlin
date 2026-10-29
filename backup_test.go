package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 导出 → 清空 → 恢复，设置与订阅都要回来，节点文件也要能写回
func TestBackupRestoreRoundTrip(t *testing.T) {
	src := newTestApp(t)
	src.mu.Lock()
	src.settings.MixedPort = 17890
	src.settings.BlockQuic = false
	src.settings.DNSEnable = true
	src.settings.DNSMode = "fake-ip"
	src.subs = []Sub{{
		ID: "s_test", Name: "机场A", URL: "https://example.com/sub?token=x",
		Interval: 6, Enabled: true, NodeCount: 2, Status: "ok",
		IncludeFilter: "香港|HK", ExcludeFilter: "过期",
	}}
	src.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(src.providerPath("s_test")), 0o755)
	_ = os.WriteFile(src.providerPath("s_test"), []byte(`{"proxies":[{"name":"香港-1","type":"ss"}]}`), 0o600)

	rec := httptest.NewRecorder()
	src.handleBackup(rec, httptest.NewRequest("GET", "/api/backup", nil))
	if rec.Code != 200 {
		t.Fatalf("导出失败：%d %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.Bytes()
	var bundle BackupBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("导出的 JSON 解析失败：%v", err)
	}
	if bundle.Kind != backupKind || len(bundle.Subs) != 1 || bundle.Providers["s_test"] == "" {
		t.Fatalf("备份内容不完整：kind=%s subs=%d providers=%d", bundle.Kind, len(bundle.Subs), len(bundle.Providers))
	}
	var exported Settings
	if err := json.Unmarshal(bundle.Settings, &exported); err != nil {
		t.Fatalf("settings 解析失败：%v", err)
	}
	if exported.DNSMode != "fake-ip" {
		t.Errorf("DNS 设置未进备份：%+v", exported)
	}

	// 恢复到全新实例
	dst := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/restore", strings.NewReader(string(raw)))
	rec2 := httptest.NewRecorder()
	dst.handleRestore(rec2, req)
	if rec2.Code != 200 {
		t.Fatalf("恢复失败：%d %s", rec2.Code, rec2.Body.String())
	}
	dst.mu.RLock()
	gotPort, gotDNS, gotSubs := dst.settings.MixedPort, dst.settings.DNSMode, len(dst.subs)
	var name, inc string
	if len(dst.subs) > 0 {
		name, inc = dst.subs[0].Name, dst.subs[0].IncludeFilter
	}
	dst.mu.RUnlock()
	if gotPort != 17890 || gotDNS != "fake-ip" || gotSubs != 1 {
		t.Fatalf("恢复结果不对：port=%d dns=%s subs=%d", gotPort, gotDNS, gotSubs)
	}
	if name != "机场A" || inc != "香港|HK" {
		t.Fatalf("订阅字段未恢复：name=%s include=%s", name, inc)
	}
	if _, err := os.Stat(dst.providerPath("s_test")); err != nil {
		t.Fatalf("节点文件未写回：%v", err)
	}
	if !strings.Contains(string(mustRead(t, dst.providerPath("s_test"))), "香港-1") {
		t.Error("节点文件内容不对")
	}
	t.Logf("✓ 备份/恢复往返成功：端口 %d，DNS %s，订阅 %d", gotPort, gotDNS, gotSubs)
}

// 非本插件备份 / 版本过新 都要明确拒绝
func TestRestoreRejectsForeignBundle(t *testing.T) {
	a := newTestApp(t)
	body := `{"kind":"something-else","format":1}`
	rec := httptest.NewRecorder()
	a.handleRestore(rec, httptest.NewRequest("POST", "/api/restore", strings.NewReader(body)))
	if rec.Code != 400 {
		t.Fatalf("外部文件应被拒绝，实际 %d", rec.Code)
	}
	body2 := `{"kind":"mihomo-merlin-backup","format":99}`
	rec2 := httptest.NewRecorder()
	a.handleRestore(rec2, httptest.NewRequest("POST", "/api/restore", strings.NewReader(body2)))
	if rec2.Code != 400 {
		t.Fatalf("过新版本应被拒绝，实际 %d", rec2.Code)
	}
	t.Log("✓ 非法备份被拒绝")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件失败 %s: %v", path, err)
	}
	return b
}
