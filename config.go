package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const groupMain = "🚀 节点选择"
const groupAuto = "♻️ 自动选择"
const testURL = "http://www.gstatic.com/generate_204"

func (a *App) configPath() string    { return filepath.Join(a.dataDir, "config.json") }
func (a *App) ruleSetPath() string   { return filepath.Join(a.dataDir, "rules", "chnroute.txt") }
func (a *App) logPath() string       { return filepath.Join(a.dataDir, "logs", "mihomo.log") }
func (a *App) mihomoAPIPath() string { return "127.0.0.1:9090" }
func (a *App) enabledSubsLocked() []Sub {
	var out []Sub
	for _, s := range a.subs {
		if !s.Enabled {
			continue
		}
		if !fileExists(a.providerPath(s.ID)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ensureRuleSet 检查国内 IP 列表是否存在；缺失时退化为不做国内直连。
func (a *App) ensureRuleSet() {
	if fileExists(a.ruleSetPath()) {
		return
	}
	// 安装包会把规则放在 /koolshare/mihomo/rules/chnroute.txt；
	// 这里再兜底找一次面板自身目录下的 rules/。
	for _, c := range []string{
		filepath.Join(a.dataDir, "dist-rules", "chnroute.txt"),
		"rules/chnroute.txt",
		"/koolshare/mihomo/rules/chnroute.txt",
	} {
		if fileExists(c) {
			_ = copyFile(c, a.ruleSetPath())
			return
		}
	}
}

// BuildConfig 依据订阅与设置生成 mihomo 配置。
func (a *App) BuildConfig() (map[string]any, error) {
	s := a.settings
	if len(a.subs) == 0 {
		return nil, errors.New("还没有订阅，请先添加订阅")
	}
	enabled := a.enabledSubsLocked()
	if len(enabled) == 0 {
		return nil, errors.New("没有可用的订阅节点，请先更新订阅")
	}

	cfg := map[string]any{
		"mixed-port":          s.MixedPort,
		"redir-port":          s.RedirPort,
		"allow-lan":           s.AllowLan,
		"bind-address":        "*",
		"mode":                s.Mode,
		"log-level":           s.LogLevel,
		"external-controller": "127.0.0.1:" + itoa(s.APIPort),
		"secret":              s.ClashSecret,
		"unified-delay":       true,
		"tcp-concurrent":      true,
		"ipv6":                false,
		"find-process-mode":   "off",
		"profile": map[string]any{
			"store-selected": true,
			"store-fake-ip":  true,
		},
	}
	if s.Sniffer {
		cfg["sniffer"] = map[string]any{
			"enable":               true,
			"parse-pure-ip":        true,
			"override-destination": true,
			"sniff": map[string]any{
				"HTTP": map[string]any{"ports": []any{80, "8080-8880"}, "override-destination": true},
				"TLS":  map[string]any{"ports": []any{443, 8443}},
			},
		}
	}

	providers := map[string]any{}
	var useIDs []string
	var subGroups []any
	for _, sub := range enabled {
		id := sub.ID
		providers[id] = map[string]any{
			"type": "file",
			"path": a.providerPath(id),
			"override": map[string]any{
				"additional-prefix": "[" + sub.Name + "] ",
				"udp":               true,
			},
		}
		useIDs = append(useIDs, id)
		// 子分组只挂自己的 provider：默认选中第一个节点，而不是 DIRECT
		subGroups = append(subGroups, map[string]any{
			"name": "📦 " + sub.Name,
			"type": "select",
			"use":  []any{id},
		})
	}
	cfg["proxy-providers"] = providers

	mainProxies := []any{groupAuto}
	mainProxies = append(mainProxies, subGroups2Names(subGroups)...)
	mainProxies = append(mainProxies, "DIRECT")

	// url-test 的测速节奏可由面板调整；老配置/零值退回默认 300s / 50ms
	autoInterval := s.AutoIntervalSec
	if autoInterval < 30 {
		autoInterval = 300
	}
	autoTolerance := s.AutoToleranceMs
	if autoTolerance < 0 {
		autoTolerance = 50
	}

	groups := []any{
		map[string]any{
			"name":    groupMain,
			"type":    "select",
			"proxies": mainProxies,
			"use":     toAnySlice(useIDs),
		},
		map[string]any{
			"name":      groupAuto,
			"type":      "url-test",
			"use":       toAnySlice(useIDs),
			"url":       testURL,
			"interval":  autoInterval,
			"tolerance": autoTolerance,
		},
	}
	groups = append(groups, subGroups...)
	cfg["proxy-groups"] = groups

	// 规则
	var rules []any
	if s.RulePreset == "cn-direct" {
		if fileExists(a.ruleSetPath()) {
			cfg["rule-providers"] = map[string]any{
				"cn-ip": map[string]any{
					"type":     "file",
					"behavior": "ipcidr",
					"path":     a.ruleSetPath(),
				},
			}
			rules = append(rules, "RULE-SET,cn-ip,DIRECT")
		}
		rules = append(rules,
			"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
			"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
			"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
			"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
		)
	}
	if s.Mode == "direct" {
		rules = append(rules, "MATCH,DIRECT")
	} else {
		rules = append(rules, "MATCH,"+groupMain)
	}
	cfg["rules"] = rules

	return cfg, nil
}

func subGroups2Names(groups []any) []any {
	out := make([]any, 0, len(groups))
	for _, g := range groups {
		if m, ok := g.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// WriteConfig 落盘配置。
func (a *App) WriteConfig() error {
	cfg, err := a.BuildConfig()
	if err != nil {
		return err
	}
	return writeJSONAtomic(a.configPath(), cfg)
}

func normalizeProviderPath(p string) string {
	return filepath.ToSlash(strings.TrimSpace(p))
}

var _ = normalizeProviderPath
var _ = os.Remove
