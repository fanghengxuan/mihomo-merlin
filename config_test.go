package main

import (
	"os"
	"path/filepath"
	"testing"
)

// DNS 段按设置生成；fake-ip 模式要带网段与过滤表
func TestBuildConfigDNSSection(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.settings.DNSEnable = true
	a.settings.DNSMode = "fake-ip"
	a.settings.DNSNameserver = "223.5.5.5, 119.29.29.29"
	a.settings.DNSFallback = "1.1.1.1"
	a.settings.DNSFakeIPRange = "198.19.0.1/16"
	a.settings.Sniffer = true
	a.subs = []Sub{{ID: "s1", Name: "机场A", URL: "https://example.com", Enabled: true, NodeCount: 1}}
	a.mu.Unlock()
	writeFakeProvider(t, a, "s1")

	cfg, err := a.BuildConfig()
	if err != nil {
		t.Fatalf("生成配置失败：%v", err)
	}
	dns, ok := cfg["dns"].(map[string]any)
	if !ok {
		t.Fatal("开启 DNS 后配置里应有 dns 段")
	}
	if dns["enhanced-mode"] != "fake-ip" {
		t.Errorf("模式不对：%v", dns["enhanced-mode"])
	}
	if dns["fake-ip-range"] != "198.19.0.1/16" {
		t.Errorf("fake-ip 网段不对：%v", dns["fake-ip-range"])
	}
	if _, ok := dns["fake-ip-filter"]; !ok {
		t.Error("fake-ip 模式应带 fake-ip-filter")
	}
	ns, ok := dns["nameserver"].([]any)
	if !ok || len(ns) != 2 {
		t.Errorf("nameserver 解析不对：%v", dns["nameserver"])
	}

	// 关闭时不应出现 dns 段
	a.mu.Lock()
	a.settings.DNSEnable = false
	a.mu.Unlock()
	cfg2, _ := a.BuildConfig()
	if _, ok := cfg2["dns"]; ok {
		t.Error("关闭 DNS 后不应生成 dns 段")
	}
	t.Log("✓ DNS 段生成/关闭都正确")
}

// 订阅过滤要落到 proxy-provider 的 filter / exclude-filter
func TestProviderFilterInConfig(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.subs = []Sub{
		{ID: "s1", Name: "机场A", Enabled: true, NodeCount: 1, IncludeFilter: "香港|HK", ExcludeFilter: "过期"},
		{ID: "s2", Name: "机场B", Enabled: true, NodeCount: 1},
	}
	a.mu.Unlock()
	writeFakeProvider(t, a, "s1")
	writeFakeProvider(t, a, "s2")

	cfg, err := a.BuildConfig()
	if err != nil {
		t.Fatalf("生成配置失败：%v", err)
	}
	provs := cfg["proxy-providers"].(map[string]any)
	p1 := provs["s1"].(map[string]any)
	if p1["filter"] != "香港|HK" || p1["exclude-filter"] != "过期" {
		t.Errorf("过滤未写入 provider：%v", p1)
	}
	p2 := provs["s2"].(map[string]any)
	if _, ok := p2["filter"]; ok {
		t.Error("没设置过滤的订阅不应带 filter")
	}
	t.Log("✓ 订阅过滤写入 proxy-provider")
}

// writeFakeProvider 造一个 provider 文件，让订阅被认为「可用」
func writeFakeProvider(t *testing.T, a *App, id string) {
	t.Helper()
	p := a.providerPath(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(p, []byte(`{"proxies":[{"name":"n1","type":"ss"}]}`), 0o600); err != nil {
		t.Fatalf("写 provider 失败: %v", err)
	}
}
