package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 未启动内核时，节点列表应从订阅节点文件里读出来（JSON 与 YAML 两种格式）
func TestPreviewNodesFromProviderFiles(t *testing.T) {
	a := newTestApp(t)

	// 1) JSON 格式（链接列表转换的结果）
	jsonSub := Sub{ID: "s_json", Name: "机场A", Enabled: true}
	if err := os.MkdirAll(filepath.Dir(a.providerPath(jsonSub.ID)), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonBody := `{"proxies":[{"name":"香港 01","type":"ss"},{"name":"日本 02","type":"trojan"}]}`
	if err := os.WriteFile(a.providerPath(jsonSub.ID), []byte(jsonBody), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2) YAML 格式（机场原始配置透传）
	yamlSub := Sub{ID: "s_yaml", Name: "机场B", Enabled: true}
	yamlBody := `mixed-port: 7890
proxies:
    - { name: '剩余流量：1 GB', type: anytls, server: 1.1.1.1, port: 443, password: p }
    - { name: 新加坡 01, type: anytls, server: 2.2.2.2, port: 443, password: p }
    - { name: vless 日本, type: vless, server: 3.3.3.3, port: 443, uuid: u }
`
	if err := os.WriteFile(a.providerPath(yamlSub.ID), []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}

	a.subs = []Sub{jsonSub, yamlSub}

	groups := a.previewGroups()
	if len(groups) != 4 { // 主分组 + 自动选择 + 两个订阅分组
		t.Fatalf("预览分组数应为 4，实际 %d", len(groups))
	}

	var found int
	for _, g := range groups {
		switch g.Name {
		case "📦 机场A":
			found++
			if len(g.Nodes) != 2 || g.Nodes[0].Name != "[机场A] 香港 01" {
				t.Errorf("机场A 节点不对: %+v", g.Nodes)
			}
		case "📦 机场B":
			found++
			if len(g.Nodes) != 3 {
				t.Fatalf("机场B 应有 3 个节点，实际 %d", len(g.Nodes))
			}
			// 伪节点仍应在列表里可见（预览不做过滤），但名字要正确
			if !strings.Contains(g.Nodes[0].Name, "剩余流量") {
				t.Errorf("首个节点名字不对: %q", g.Nodes[0].Name)
			}
		case groupMain:
			found++
			if len(g.Nodes) != 4 { // 自动选择 + 两个订阅 + DIRECT
				t.Errorf("主分组应有 4 项，实际 %d", len(g.Nodes))
			}
		case groupAuto:
			found++
			if len(g.Nodes) != 5 {
				t.Errorf("自动选择应包含全部 5 个节点，实际 %d", len(g.Nodes))
			}
		}
	}
	if found != 4 {
		t.Errorf("缺少分组，实际匹配 %d 个", found)
	}
	t.Logf("✓ 未启动预览：%d 组，共 %d 个节点", len(groups), a.previewNodeCount())
}

// 没有节点文件时预览应为空，而不是报错
func TestPreviewEmptyWhenNoProviders(t *testing.T) {
	a := newTestApp(t)
	a.subs = []Sub{{ID: "s_missing", Name: "空", Enabled: true}}
	if groups := a.previewGroups(); len(groups) != 0 {
		t.Fatalf("没有节点文件时应返回空，实际 %d 组", len(groups))
	}
	t.Logf("✓ 无节点文件时预览为空")
}
