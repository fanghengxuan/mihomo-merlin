package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeProvider(t *testing.T, a *App, id, content string) string {
	t.Helper()
	p := a.providerPath(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 未运行内核时，节点预览应命中缓存；provider 文件变化后重新解析。
func TestPreviewGroupsCacheByFileSignature(t *testing.T) {
	a := newBackendTestApp(t)
	a.subs = []Sub{{ID: "s_p", Name: "预览", Enabled: true}}
	p := writeProvider(t, a, "s_p", `{"proxies":[{"name":"n1","type":"ss"}]}`)

	g1 := a.previewGroups()
	g2 := a.previewGroups()
	if len(g1) == 0 || len(g2) == 0 {
		t.Fatal("预览分组为空")
	}
	if &g1[0] != &g2[0] {
		t.Fatal("第二次调用没有命中缓存（切片底层数组不同）")
	}

	// 改文件并把 mtime 往后挪，确保签名变化。
	if err := os.WriteFile(p, []byte(`{"proxies":[{"name":"n1","type":"ss"},{"name":"n2","type":"vmess"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, future, future)

	g3 := a.previewGroups()
	var auto *groupDTO
	for i := range g3 {
		if g3[i].Name == groupAuto {
			auto = &g3[i]
			break
		}
	}
	if auto == nil {
		t.Fatal("缺少自动选择分组")
	}
	names := make([]string, 0, len(auto.Nodes))
	for _, n := range auto.Nodes {
		names = append(names, n.Name)
	}
	if len(auto.Nodes) != 2 || !strings.Contains(strings.Join(names, ","), "n2") {
		t.Fatalf("文件变化后预览未更新：%v", names)
	}
}
