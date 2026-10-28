package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Settings 面板/引擎的全部可配置项，持久化到 dataDir/settings.json。
type Settings struct {
	Mode          string `json:"mode"`          // rule | global | direct
	AllowLan      bool   `json:"allowLan"`      // 允许 LAN 访问代理端口
	RedirPort     int    `json:"redirPort"`     // 透明代理 TCP 入口
	MixedPort     int    `json:"mixedPort"`     // HTTP/SOCKS 混合端口
	LogLevel      string `json:"logLevel"`      // silent|error|warning|info|debug
	AutoStart     bool   `json:"autoStart"`     // 开机自启
	BlockQuic     bool   `json:"blockQuic"`     // 阻断 UDP/443，逼回 TCP
	RouterSelf    bool   `json:"routerSelf"`    // 是否也代理路由器自身流量（实验）
	SubInterval   int    `json:"subInterval"`   // 订阅自动更新间隔（小时，0=关闭）
	PanelPassword string `json:"panelPassword"` // 空=免密
	RulePreset    string `json:"rulePreset"`    // cn-direct | proxy-all
	Sniffer       bool   `json:"sniffer"`       // 域名嗅探

	// 订阅拉取策略（对齐 fancyss / MerlinClash 的实现）
	SubUA        string `json:"subUA"`        // default|curl|v2rayn|v2rayng|shadowrocket|custom
	SubUACustom  string `json:"subUACustom"`  // SubUA=custom 时生效
	SubInsecure  bool   `json:"subInsecure"`  // 跳过证书校验（路由器时间不准时必需）
	SubProxyMode string `json:"subProxyMode"` // auto|direct|proxy
	// 国内 IP 规则订阅
	RuleSourcePreset string `json:"ruleSourcePreset"` // 预设来源 id（chnroutes2/17mon/loyalsoldier/custom）
	RuleSourceURL    string `json:"ruleSourceURL"`    // 自定义规则源，留空用内置镜像
	RuleAutoUpdate   bool   `json:"ruleAutoUpdate"`   // 自动更新
	RuleIntervalH    int    `json:"ruleIntervalH"`    // 更新间隔（小时）
	LanIf            string `json:"lanIf"`            // 局域网接口，默认 br0

	// 定时重启内核
	AutoRestart       bool   `json:"autoRestart"`       // 是否启用定时重启
	RestartEveryHours int    `json:"restartEveryHours"` // 每 N 小时重启一次（1..168）
	RestartAt         string `json:"restartAt"`         // 每天固定时刻 "HH:MM"，空=不用

	// 自动选择组（url-test）参数
	AutoIntervalSec int `json:"autoIntervalSec"` // 测速间隔秒（30..3600）
	AutoToleranceMs int `json:"autoToleranceMs"` // 容差毫秒（0..1000）

	// 内部字段，不通过 API 暴露
	ClashSecret string `json:"clashSecret"`
	APIPort     int    `json:"apiPort"`
}

func defaultSettings() Settings {
	return Settings{
		Mode:             "rule",
		AllowLan:         true,
		RedirPort:        3333,
		MixedPort:        7890,
		LogLevel:         "info",
		AutoStart:        true,
		BlockQuic:        true,
		SubInterval:      6,
		RulePreset:       "cn-direct",
		Sniffer:          true,
		SubUA:            "default",
		SubInsecure:      true,
		SubProxyMode:     "auto",
		RuleSourcePreset: defaultRulePresetID,
		RuleAutoUpdate:   true,
		RuleIntervalH:    24,
		LanIf:            "br0",

		AutoRestart:       false,
		RestartEveryHours: 24,
		RestartAt:         "",
		AutoIntervalSec:   300,
		AutoToleranceMs:   50,

		APIPort: 9090,
	}
}

func (a *App) loadSettings() {
	a.settings = defaultSettings()
	b, err := os.ReadFile(filepath.Join(a.dataDir, "settings.json"))
	if err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
	if a.settings.ClashSecret == "" {
		a.settings.ClashSecret = randomToken(24)
	}
	if a.settings.APIPort == 0 {
		a.settings.APIPort = 9090
	}
	// 兜底纠错
	if a.settings.Mode != "rule" && a.settings.Mode != "global" && a.settings.Mode != "direct" {
		a.settings.Mode = "rule"
	}
	if a.settings.RedirPort <= 0 {
		a.settings.RedirPort = 3333
	}
	if a.settings.MixedPort <= 0 {
		a.settings.MixedPort = 7890
	}
	if a.settings.LanIf == "" {
		a.settings.LanIf = "br0"
	}
	if a.settings.LogLevel == "" {
		a.settings.LogLevel = "info"
	}
	// 定时重启相关字段的兜底纠错（老版本 settings.json 没有这些键）
	if a.settings.RestartEveryHours < 1 || a.settings.RestartEveryHours > 168 {
		a.settings.RestartEveryHours = 24
	}
	if a.settings.RestartAt != "" && !restartAtPattern.MatchString(a.settings.RestartAt) {
		a.settings.RestartAt = ""
	}
	if a.settings.AutoIntervalSec < 30 || a.settings.AutoIntervalSec > 3600 {
		a.settings.AutoIntervalSec = 300
	}
	if a.settings.AutoToleranceMs < 0 || a.settings.AutoToleranceMs > 1000 {
		a.settings.AutoToleranceMs = 50
	}
	a.saveSettings()
}

func (a *App) saveSettings() error {
	return writeJSONAtomic(filepath.Join(a.dataDir, "settings.json"), a.settings)
}

// validateSettings 校验外部传入的改动并返回给前端的完整设置。
var errSettingsInvalid = errors.New("设置不合法")

func (s *Settings) applyPatch(p map[string]any) error {
	if v, ok := p["mode"].(string); ok {
		switch v {
		case "rule", "global", "direct":
			s.Mode = v
		default:
			return errors.New("mode 只能是 rule/global/direct")
		}
	}
	if v, ok := p["logLevel"].(string); ok {
		switch v {
		case "silent", "error", "warning", "info", "debug":
			s.LogLevel = v
		default:
			return errors.New("日志级别不合法")
		}
	}
	if v, ok := p["rulePreset"].(string); ok {
		switch v {
		case "cn-direct", "proxy-all":
			s.RulePreset = v
		default:
			return errors.New("规则预设不合法")
		}
	}
	if v, ok := p["allowLan"].(bool); ok {
		s.AllowLan = v
	}
	if v, ok := p["autoStart"].(bool); ok {
		s.AutoStart = v
	}
	if v, ok := p["blockQuic"].(bool); ok {
		s.BlockQuic = v
	}
	if v, ok := p["routerSelf"].(bool); ok {
		s.RouterSelf = v
	}
	if v, ok := p["sniffer"].(bool); ok {
		s.Sniffer = v
	}
	if v, ok := p["subInsecure"].(bool); ok {
		s.SubInsecure = v
	}
	if v, ok := p["subUA"].(string); ok {
		switch v {
		case "default", "curl", "v2rayn", "v2rayng", "shadowrocket", "custom":
			s.SubUA = v
		default:
			return errors.New("订阅 UA 预设不合法")
		}
	}
	if v, ok := p["subUACustom"].(string); ok {
		s.SubUACustom = strings.TrimSpace(v)
	}
	if v, ok := p["ruleSourcePreset"].(string); ok {
		s.RuleSourcePreset = strings.TrimSpace(v)
	}
	if v, ok := p["ruleSourceURL"].(string); ok {
		s.RuleSourceURL = strings.TrimSpace(v)
	}
	if v, ok := p["ruleAutoUpdate"].(bool); ok {
		s.RuleAutoUpdate = v
	}
	if v, ok := p["ruleIntervalH"].(float64); ok {
		if v < 0 {
			return errors.New("规则更新间隔不能为负")
		}
		s.RuleIntervalH = int(v)
	}
	if v, ok := p["subProxyMode"].(string); ok {
		switch v {
		case "auto", "direct", "proxy":
			s.SubProxyMode = v
		default:
			return errors.New("订阅下载方式不合法")
		}
	}
	if v, ok := p["panelPassword"].(string); ok {
		s.PanelPassword = strings.TrimSpace(v)
	}
	if v, ok := p["lanIf"].(string); ok && strings.TrimSpace(v) != "" {
		s.LanIf = strings.TrimSpace(v)
	}
	if v, ok := num(p["redirPort"]); ok {
		if v < 1 || v > 65535 {
			return errors.New("redir 端口越界")
		}
		s.RedirPort = v
	}
	if v, ok := num(p["mixedPort"]); ok {
		if v < 1 || v > 65535 {
			return errors.New("mixed 端口越界")
		}
		s.MixedPort = v
	}
	if v, ok := num(p["subInterval"]); ok {
		if v < 0 || v > 168 {
			return errors.New("订阅间隔需在 0-168 小时之间")
		}
		s.SubInterval = v
	}
	if v, ok := p["autoRestart"].(bool); ok {
		s.AutoRestart = v
	}
	if v, ok := num(p["restartEveryHours"]); ok {
		if v < 1 || v > 168 {
			return errors.New("重启间隔需在 1-168 小时之间")
		}
		s.RestartEveryHours = v
	}
	if v, ok := p["restartAt"].(string); ok {
		t := strings.TrimSpace(v)
		if t != "" && !restartAtPattern.MatchString(t) {
			return errors.New("定时重启时刻格式应为 HH:MM（如 04:30）")
		}
		s.RestartAt = t
	}
	if v, ok := num(p["autoIntervalSec"]); ok {
		if v < 30 || v > 3600 {
			return errors.New("自动选择测速间隔需在 30-3600 秒之间")
		}
		s.AutoIntervalSec = v
	}
	if v, ok := num(p["autoToleranceMs"]); ok {
		if v < 0 || v > 1000 {
			return errors.New("自动选择容差需在 0-1000 毫秒之间")
		}
		s.AutoToleranceMs = v
	}
	if s.RedirPort == s.MixedPort {
		return errors.New("redir 与 mixed 端口不能相同")
	}
	return nil
}

func num(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	}
	return 0, false
}
