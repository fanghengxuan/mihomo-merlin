package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RuleSetMeta 记录国内 IP 规则集的现状（展示在设置页）。
type RuleSetMeta struct {
	UpdatedAt int64  `json:"updatedAt"`
	Source    string `json:"source"`
	Count     int    `json:"count"`
	Bytes     int64  `json:"bytes"`
}

// RuleSourcePreset 一个可选的国内 IP 列表来源（都实测可用，纯 CIDR 文本）。
type RuleSourcePreset struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Note  string   `json:"note"`
	Lines string   `json:"lines"` // 大致条数，用于界面提示
	URLs  []string `json:"-"`
}

// ruleSourcePresets 内置来源，界面下拉选择；每条都带 CDN + 直连两个镜像。
var ruleSourcePresets = []RuleSourcePreset{
	{
		ID:    "chnroutes2",
		Name:  "chnroutes2（推荐）",
		Note:  "社区聚合，只收真实在用网段，体积最小",
		Lines: "≈3900",
		URLs: []string{
			"https://cdn.jsdelivr.net/gh/misakaio/chnroutes2@master/chnroutes.txt",
			"https://raw.githubusercontent.com/misakaio/chnroutes2/master/chnroutes.txt",
		},
	},
	{
		ID:    "17mon",
		Name:  "17mon china_ip_list",
		Note:  "老牌国内 IP 库，覆盖完整、更新稳定",
		Lines: "≈7400",
		URLs: []string{
			"https://cdn.jsdelivr.net/gh/17mon/china_ip_list@master/china_ip_list.txt",
			"https://raw.githubusercontent.com/17mon/china_ip_list/master/china_ip_list.txt",
		},
	},
	{
		ID:    "loyalsoldier",
		Name:  "Loyalsoldier/geoip",
		Note:  "与主流 Clash 规则同源，网段最细、体积最大",
		Lines: "≈10000",
		URLs: []string{
			"https://cdn.jsdelivr.net/gh/Loyalsoldier/geoip@release/text/cn.txt",
			"https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt",
		},
	},
}

const defaultRulePresetID = "chnroutes2"

func findRulePreset(id string) *RuleSourcePreset {
	for i := range ruleSourcePresets {
		if ruleSourcePresets[i].ID == id {
			return &ruleSourcePresets[i]
		}
	}
	return nil
}

// defaultRuleSources 兜底：默认预设的镜像列表。
func defaultRuleSources() []string {
	if p := findRulePreset(defaultRulePresetID); p != nil {
		return p.URLs
	}
	return nil
}

func (a *App) ruleMetaPath() string { return filepath.Join(a.dataDir, "rules", "meta.json") }

func (a *App) loadRuleMeta() RuleSetMeta {
	var m RuleSetMeta
	if b, err := os.ReadFile(a.ruleMetaPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if m.Bytes == 0 {
		if fi, err := os.Stat(a.ruleSetPath()); err == nil {
			m.Bytes = fi.Size()
			m.UpdatedAt = fi.ModTime().Unix()
		}
	}
	if m.Count == 0 {
		if lines, err := parseCIDRList(readFileOrNil(a.ruleSetPath())); err == nil {
			m.Count = len(lines)
		}
	}
	if m.Source == "" && m.Count > 0 {
		m.Source = "安装包内置"
	}
	return m
}

func readFileOrNil(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return b
}

// parseCIDRList 校验并规范化规则文本：只保留合法的 IPv4/IPv6 CIDR，
// 忽略空行与 # 注释。返回去重后的列表。
func parseCIDRList(b []byte) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		// 兼容 "1.2.3.0/24  # 注释" 与带前缀的写法
		if i := strings.IndexAny(line, " \t#"); i > 0 {
			line = line[:i]
		}
		if _, _, err := net.ParseCIDR(line); err != nil {
			// 单个 IP 也接受（补成 /32 或 /128）
			if ip := net.ParseIP(line); ip != nil {
				if ip.To4() != nil {
					line += "/32"
				} else {
					line += "/128"
				}
			} else {
				continue
			}
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取规则失败: %v", err)
	}
	return out, nil
}

// UpdateRuleSet 下载并校验规则集，成功后原子替换并返回元信息。
// sourceURL 为空时依次尝试内置镜像；显式指定则只试它。
func (a *App) UpdateRuleSet(sourceURL string) (*RuleSetMeta, error) {
	a.mu.RLock()
	custom := strings.TrimSpace(a.settings.RuleSourceURL)
	presetID := strings.TrimSpace(a.settings.RuleSourcePreset)
	if presetID == "" && custom == "" {
		presetID = defaultRulePresetID
	}
	if custom != "" && presetID == "custom" {
		presetID = ""
	}
	a.mu.RUnlock()

	candidates := resolveRuleCandidates(sourceURL, presetID, custom)

	return a.updateRuleSetWith(candidates)
}

// updateRuleSetWith 依次尝试给定来源，第一个通过校验的就落盘。
func (a *App) updateRuleSetWith(candidates []string) (*RuleSetMeta, error) {
	var errs []string
	for _, src := range candidates {
		a.prog("rules", "规则集", "connect", "开始下载规则："+src, false, false)
		_, body, err := a.doFetch(src, false, "规则集")
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", src, err))
			continue
		}
		list, err := parseCIDRList(body)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", src, err))
			continue
		}
		// 合理性检查：正常都在几千条以上，太少说明下载到的是错误页
		if len(list) < 500 {
			errs = append(errs, fmt.Sprintf("%s: 只解析出 %d 条，疑似下载到错误页", src, len(list)))
			continue
		}

		if err := os.MkdirAll(filepath.Dir(a.ruleSetPath()), 0o755); err != nil {
			return nil, fmt.Errorf("创建目录失败: %v", err)
		}
		tmp := a.ruleSetPath() + ".tmp"
		content := strings.Join(list, "\n") + "\n"
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("写入规则失败: %v", err)
		}
		if err := os.Rename(tmp, a.ruleSetPath()); err != nil {
			return nil, fmt.Errorf("替换规则失败: %v", err)
		}

		meta := RuleSetMeta{
			UpdatedAt: time.Now().Unix(),
			Source:    src,
			Count:     len(list),
			Bytes:     int64(len(content)),
		}
		if b, err := json.Marshal(meta); err == nil {
			_ = os.WriteFile(a.ruleMetaPath(), b, 0o644)
		}
		a.addLog("info", fmt.Sprintf("国内 IP 规则已更新：%d 条（来源 %s）", meta.Count, src))
		a.prog("rules", "规则集", "done", fmt.Sprintf("规则已更新：%d 条", meta.Count), true, false)
		return &meta, nil
	}

	a.prog("rules", "规则集", "error", "规则下载失败："+strings.Join(errs, "；"), true, true)
	return nil, fmt.Errorf("规则下载失败：%s", strings.Join(errs, "；"))
}

// ruleUpdateDue 判断是否到了自动更新时间。
func (a *App) ruleUpdateDue() bool {
	a.mu.RLock()
	auto := a.settings.RuleAutoUpdate
	hours := a.settings.RuleIntervalH
	a.mu.RUnlock()
	if !auto {
		return false
	}
	if hours <= 0 {
		hours = 24
	}
	meta := a.loadRuleMeta()
	if meta.UpdatedAt == 0 {
		return true
	}
	return time.Since(time.Unix(meta.UpdatedAt, 0)) >= time.Duration(hours)*time.Hour
}

// ruleScheduler 定期检查并自动更新规则集。
func (a *App) ruleScheduler() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		if !a.ruleUpdateDue() {
			continue
		}
		if _, err := a.UpdateRuleSet(""); err != nil {
			a.addLog("warn", "自动更新国内 IP 规则失败："+err.Error())
			continue
		}
		if a.engine.Running() {
			_ = a.appReload()
		}
	}
}

// resolveRuleCandidates 按优先级决定本次要尝试的来源：
// 显式 url > 显式 preset > 自定义 url > 默认预设。
func resolveRuleCandidates(sourceURL, presetID, customURL string) []string {
	if u := strings.TrimSpace(sourceURL); u != "" {
		return []string{u}
	}
	if id := strings.TrimSpace(presetID); id != "" && id != "custom" {
		if p := findRulePreset(id); p != nil {
			return p.URLs
		}
	}
	if c := strings.TrimSpace(customURL); c != "" {
		return []string{c}
	}
	return defaultRuleSources()
}
