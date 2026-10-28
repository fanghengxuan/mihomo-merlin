package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 未启动内核时也要能看节点：直接从订阅的节点文件里读名字与类型。
// （内核跑起来后，节点列表以内核 /proxies 为准）

var proxyTypeRe = regexp.MustCompile(`type:\s*['"]?([a-zA-Z0-9_-]+)`)

type previewProxy struct {
	Name string
	Type string
}

// loadProviderProxies 解析节点文件，兼容两种存储格式：
//   - JSON（链接列表转换后的结果）：{"proxies":[{"name":…,"type":…}]}
//   - YAML（机场原始 Clash 配置的透传内容）：proxies: 下面的列表
func loadProviderProxies(path string) []previewProxy {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(b), "\uFEFF"))
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "{") {
		var doc struct {
			Proxies []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"proxies"`
		}
		if json.Unmarshal([]byte(trimmed), &doc) == nil {
			out := make([]previewProxy, 0, len(doc.Proxies))
			for _, p := range doc.Proxies {
				if p.Name == "" {
					continue
				}
				out = append(out, previewProxy{Name: p.Name, Type: p.Type})
			}
			return out
		}
	}

	// YAML：取 proxies: 块里的每个序列项
	var out []previewProxy
	inProxies := false
	var itemLines []string
	flush := func() {
		if len(itemLines) == 0 {
			return
		}
		joined := strings.Join(itemLines, " ")
		name := ""
		if m := nodeNameRe.FindStringSubmatch(joined); m != nil {
			name = firstNonEmpty(m[1], m[2], strings.TrimSpace(m[3]))
		}
		typ := ""
		if m := proxyTypeRe.FindStringSubmatch(joined); m != nil {
			typ = m[1]
		}
		if name != "" {
			out = append(out, previewProxy{Name: name, Type: typ})
		}
		itemLines = nil
	}

	for _, raw := range strings.Split(trimmed, "\n") {
		line := strings.TrimRight(raw, "\r")
		topLevel := line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
		if topLevel {
			flush()
			inProxies = strings.HasPrefix(strings.TrimSpace(line), "proxies:")
			continue
		}
		if !inProxies {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "- ") || strings.TrimSpace(line) == "-" {
			flush()
			itemLines = []string{line}
			continue
		}
		if len(itemLines) > 0 {
			itemLines = append(itemLines, line)
		}
	}
	flush()
	return out
}

// previewGroups 在没有运行内核时，按与运行时一致的结构拼出分组：
//
//	🚀 节点选择（含 ♻️ 自动选择、各订阅分组、DIRECT）
//	♻️ 自动选择（所有节点）
//	📦 订阅名（该订阅的节点）
func (a *App) previewGroups() []groupDTO {
	a.mu.RLock()
	subs := make([]Sub, len(a.subs))
	copy(subs, a.subs)
	a.mu.RUnlock()

	type subView struct {
		group string
		nodes []nodeDTO
	}
	var views []subView
	var allNodes []nodeDTO

	for _, s := range subs {
		if !s.Enabled {
			continue
		}
		proxies := loadProviderProxies(a.providerPath(s.ID))
		if len(proxies) == 0 {
			continue
		}
		group := "📦 " + s.Name
		nodes := make([]nodeDTO, 0, len(proxies))
		for _, p := range proxies {
			nd := nodeDTO{
				Name:  "[" + s.Name + "] " + p.Name,
				Type:  p.Type,
				Sub:   s.Name,
				Delay: 0,
			}
			nodes = append(nodes, nd)
			allNodes = append(allNodes, nd)
		}
		views = append(views, subView{group: group, nodes: nodes})
	}

	if len(views) == 0 {
		return nil
	}

	groups := make([]groupDTO, 0, len(views)+2)

	mainNodes := []nodeDTO{{Name: groupAuto, Type: "url-test"}}
	for _, v := range views {
		mainNodes = append(mainNodes, nodeDTO{Name: v.group, Type: "select"})
	}
	mainNodes = append(mainNodes, nodeDTO{Name: "DIRECT", Type: "direct"})
	groups = append(groups, groupDTO{
		Name:  groupMain,
		Type:  "select",
		Now:   groupAuto,
		Nodes: mainNodes,
	})

	autoNow := ""
	if len(allNodes) > 0 {
		autoNow = allNodes[0].Name
	}
	groups = append(groups, groupDTO{
		Name:  groupAuto,
		Type:  "url-test",
		Now:   autoNow,
		Nodes: allNodes,
	})

	for _, v := range views {
		now := ""
		if len(v.nodes) > 0 {
			now = v.nodes[0].Name
		}
		groups = append(groups, groupDTO{
			Name:  v.group,
			Type:  "select",
			Now:   now,
			Nodes: v.nodes,
		})
	}
	return groups
}

// previewNodeCount 未启动时统计可用节点数（供概览显示）。
func (a *App) previewNodeCount() int {
	n := 0
	for _, g := range a.previewGroups() {
		if strings.HasPrefix(g.Name, "📦 ") {
			n += len(g.Nodes)
		}
	}
	return n
}

var _ = filepath.Join
