package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 查询源地址做成变量，便于测试替换
var (
	ipAPIURL  = "http://ip-api.com/json/?fields=status,message,country,countryCode,isp,as,query,hosting,mobile,proxy&lang=zh-CN"
	ipinfoURL = "https://ipinfo.io/json"
)

// ExitInfo 是一次出口 IP 探测的结果（参考 FlClash 仪表盘的「当前出口」卡片）。
type ExitInfo struct {
	IP          string `json:"ip"`
	CountryCode string `json:"countryCode"`
	Country     string `json:"country"`
	ISP         string `json:"isp"`
	ASN         string `json:"asn"`
	Kind        string `json:"kind"` // 住宅 / 移动 / 机房 / 未知
	Hosting     bool   `json:"hosting"`
	Mobile      bool   `json:"mobile"`
	Proxy       bool   `json:"proxyFlag"`
	LatencyMS   int64  `json:"latencyMs"`
	Source      string `json:"source"`
	Err         string `json:"err,omitempty"`
}

// NetInfo 汇总网络状态，给概览页用。
type NetInfo struct {
	Direct      *ExitInfo `json:"direct"`
	Proxy       *ExitInfo `json:"proxy"`
	SameIP      bool      `json:"sameIp"`
	Proxied     bool      `json:"proxied"`
	Connections int       `json:"connections"`
	MemoryMB    float64   `json:"memoryMb"`
	Node        string    `json:"node"`
	NodeDelayAt string    `json:"nodeDelayAt"`
	UpTotal     int64     `json:"upTotal"`
	DownTotal   int64     `json:"downTotal"`
	NodeDelay   int       `json:"nodeDelay"`
	UpdatedAt   int64     `json:"updatedAt"`
}

type netInfoCache struct {
	mu      sync.Mutex
	at      time.Time
	data    *NetInfo
	probing bool
}

// ---------- 出口 IP 质量 ----------

// 查询源做成包级变量，便于测试替换（生产环境走「经代理」路径）。
var (
	ipQualityURL         = "https://api.ipapi.is/?q="
	ipQualityFallbackURL = "https://ipapi.co/json/"
)

// ipQualityCacheTTL 是 IP 质量结果的缓存时长，避免面板反复外呼。
const ipQualityCacheTTL = 120 * time.Second

// IPQualityChecks 是 IP 质量探测关心的三个风险标记。
type IPQualityChecks struct {
	Proxy bool `json:"proxy"`
	VPN   bool `json:"vpn"`
	Tor   bool `json:"tor"`
}

// IPQuality 是一次出口 IP 质量探测的结果。
type IPQuality struct {
	IP     string          `json:"ip"`
	Type   string          `json:"type"`  // hosting / isp / business…（查询源自称的类型）
	Level  string          `json:"level"` // normal | risk | unknown
	Checks IPQualityChecks `json:"checks"`
	Source string          `json:"source"`
	Err    string          `json:"err"`
}

// qualityLevel 依据风险标记给出等级；查询源没给出可用标记时返回 unknown。
// qualityLevel 统一用 good / normal / risky 三档（与前端展示一致）。
func qualityLevel(proxy, vpn, tor bool) string {
	if proxy || vpn || tor {
		return "risky"
	}
	return "good"
}

// parseIPQualityIS 解析 api.ipapi.is 的返回。
func parseIPQualityIS(body []byte) (*IPQuality, error) {
	var r struct {
		IP           string `json:"ip"`
		IsProxy      bool   `json:"is_proxy"`
		IsVPN        bool   `json:"is_vpn"`
		IsTor        bool   `json:"is_tor"`
		IsDatacenter bool   `json:"is_datacenter"`
		IsAbuser     bool   `json:"is_abuser"`
		Company      struct {
			Type string `json:"type"`
		} `json:"company"`
		ASN struct {
			Type string `json:"type"`
		} `json:"asn"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.IP) == "" {
		return nil, errors.New("返回内容缺少 ip 字段")
	}
	checks := IPQualityChecks{Proxy: r.IsProxy, VPN: r.IsVPN, Tor: r.IsTor}
	return &IPQuality{
		IP:     r.IP,
		Type:   firstNonEmpty(r.Company.Type, r.ASN.Type),
		Level:  qualityLevel(checks.Proxy, checks.VPN, checks.Tor),
		Checks: checks,
		Source: "ipapi.is",
	}, nil
}

// parseIPQualityCO 解析备用源 ipapi.co 的返回；该源没有风险标记，等级只能给 unknown。
func parseIPQualityCO(body []byte) (*IPQuality, error) {
	var r struct {
		IP     string `json:"ip"`
		Error  bool   `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Error {
		return nil, fmt.Errorf("查询源报错：%s", firstNonEmpty(r.Reason, "未知原因"))
	}
	if strings.TrimSpace(r.IP) == "" {
		return nil, errors.New("返回内容缺少 ip 字段")
	}
	return &IPQuality{IP: r.IP, Level: "unknown", Checks: IPQualityChecks{}, Source: "ipapi.co"}, nil
}

// respStatus 把 HTTP 响应的状态码转成可读文本（用于 err 说明）。
func respStatus(resp *http.Response) string {
	if resp == nil {
		return "无响应"
	}
	return itoa(resp.StatusCode)
}

// probeIPQuality 经代理探测出口 IP 质量：先 ipapi.is，失败退 ipapi.co。
// 任何失败都只写进 Err，不返回 error，保证接口永远能给出 200 响应。
func (a *App) probeIPQuality() *IPQuality {
	var errs []string

	// 首选：ip-api.com（与出口 IP 同一来源，实测稳定，返回 hosting/mobile/proxy 标记）
	resp, body, err := a.doFetchQuiet(ipAPIURL, true)
	if err == nil && resp != nil && resp.StatusCode == 200 {
		if q, perr := parseIPQualityIpAPI(body); perr == nil {
			return q
		} else {
			errs = append(errs, "ip-api: "+perr.Error())
		}
	} else if err != nil {
		errs = append(errs, "ip-api: "+err.Error())
	}

	// 备用 1：ipapi.is（信息最全，但不是每个网络都能连上）
	resp2, body2, err2 := a.doFetchQuiet(ipQualityURL, true)
	if err2 == nil && resp2 != nil && resp2.StatusCode == 200 {
		if q, perr := parseIPQualityIS(body2); perr == nil {
			return q
		} else {
			errs = append(errs, "ipapi.is: "+perr.Error())
		}
	} else if err2 != nil {
		errs = append(errs, "ipapi.is: "+err2.Error())
	}

	// 备用 2：ipapi.co
	resp3, body3, err3 := a.doFetchQuiet(ipQualityFallbackURL, true)
	if err3 == nil && resp3 != nil && resp3.StatusCode == 200 {
		if q, perr := parseIPQualityCO(body3); perr == nil {
			return q
		} else {
			errs = append(errs, "ipapi.co: "+perr.Error())
		}
	} else if err3 != nil {
		errs = append(errs, "ipapi.co: "+err3.Error())
	}

	if len(errs) == 0 {
		errs = append(errs, "所有查询源都没有返回可用结果")
	}
	return &IPQuality{Level: "unknown", Type: "unknown", Err: strings.Join(errs, "；")}
}

// parseIPQualityIpAPI 用 ip-api.com 的标记推导 IP 类型与质量。
func parseIPQualityIpAPI(body []byte) (*IPQuality, error) {
	var r struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Query       string `json:"query"`
		Hosting     bool   `json:"hosting"`
		Mobile      bool   `json:"mobile"`
		Proxy       bool   `json:"proxy"`
		CountryCode string `json:"countryCode"`
		ISP         string `json:"isp"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" || strings.TrimSpace(r.Query) == "" {
		return nil, fmt.Errorf("ip-api 返回不可用：%s", firstNonEmpty(r.Message, r.Status))
	}
	typ := "residential"
	switch {
	case r.Hosting:
		typ = "hosting"
	case r.Mobile:
		typ = "mobile"
	}
	checks := IPQualityChecks{Proxy: r.Proxy}
	level := qualityLevel(checks.Proxy, checks.VPN, checks.Tor)
	if level != "risky" && r.Hosting {
		level = "normal"
	}
	return &IPQuality{
		IP:     r.Query,
		Type:   typ,
		Level:  level,
		Checks: checks,
		Source: "ip-api.com",
	}, nil
}

// ---------- 内网信息 ----------

// nvramBin 是读取路由器 NVRAM 的命令名，做成变量便于测试替换成假脚本。
var nvramBin = "nvram"

// nvramLanIP 读路由器的 LAN IP；命令缺失或执行失败时返回空串（不报错）。
func nvramLanIP() string {
	out, err := run(3*time.Second, nvramBin, "get", "lan_ipaddr")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ifaceIPv4 取指定网络接口上的 IPv4 地址（跳过回环与链路本地地址）。
func ifaceIPv4(name string) []string {
	out := make([]string, 0, 2)
	if strings.TrimSpace(name) == "" {
		return out
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return out
	}
	for _, ad := range addrs {
		var ip net.IP
		switch v := ad.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
			out = append(out, ip4.String())
		}
	}
	return out
}

// probeExitIP 查出口 IP 与国家码。ip-api.com 免费且不需要 key（仅 HTTP），
// 失败时退到 ipinfo.io。viaProxy=true 时经本机 mihomo 的混合端口发起。
func (a *App) probeExitIP(viaProxy bool) *ExitInfo {
	start := time.Now()
	info := &ExitInfo{LatencyMS: -1}

	type ipAPI struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		ISP         string `json:"isp"`
		AS          string `json:"as"`
		Query       string `json:"query"`
		Hosting     bool   `json:"hosting"`
		Mobile      bool   `json:"mobile"`
		Proxy       bool   `json:"proxy"`
	}
	url := ipAPIURL
	resp, body, err := a.doFetchQuiet(url, viaProxy)
	if err == nil && resp != nil && resp.StatusCode == 200 {
		var r ipAPI
		if json.Unmarshal(body, &r) == nil && r.Status == "success" {
			info.IP = r.Query
			info.CountryCode = strings.ToUpper(r.CountryCode)
			info.Country = r.Country
			info.ISP = r.ISP
			info.ASN = r.AS
			info.Hosting, info.Mobile, info.Proxy = r.Hosting, r.Mobile, r.Proxy
			switch {
			case r.Mobile:
				info.Kind = "移动网络"
			case r.Hosting:
				info.Kind = "机房/云"
			case r.Proxy:
				info.Kind = "代理/VPN"
			default:
				info.Kind = "住宅/商业"
			}
			info.Source = "ip-api.com"
			info.LatencyMS = time.Since(start).Milliseconds()
			return info
		}
	}

	// 备用源
	resp2, body2, err2 := a.doFetchQuiet(ipinfoURL, viaProxy)
	if err2 == nil && resp2 != nil && resp2.StatusCode == 200 {
		var r struct {
			IP       string `json:"ip"`
			Country  string `json:"country"`
			Org      string `json:"org"`
			City     string `json:"city"`
			Region   string `json:"region"`
			Timezone string `json:"timezone"`
		}
		if json.Unmarshal(body2, &r) == nil && r.IP != "" {
			info.IP = r.IP
			info.CountryCode = strings.ToUpper(r.Country)
			info.ISP = r.Org
			info.Kind = "未知"
			info.Source = "ipinfo.io"
			info.LatencyMS = time.Since(start).Milliseconds()
			return info
		}
	}

	if err != nil {
		info.Err = err.Error()
	} else if err2 != nil {
		info.Err = err2.Error()
	} else {
		info.Err = "所有查询源都没有返回可用结果"
	}
	return info
}

// doFetchQuiet 轻量请求：不产生进度事件，用于状态类查询。
func (a *App) doFetchQuiet(rawURL string, viaProxy bool) (*http.Response, []byte, error) {
	client := a.newHTTPClient(viaProxy, a.settings.SubInsecure)
	client.Timeout = 5 * time.Second
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "mihomo-merlin/"+panelVersion)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return resp, nil, err
	}
	return resp, body, nil
}

// collectNetInfo 汇总出口 IP、直连对比、连接数、内存等。
// collectNetInfo 只收集本地即可得到的运行信息（当前节点 / 延迟 / 内存），
// 不再对外探测出口 IP——面板不再展示这些内容，避免路由器产生多余外呼。
func (a *App) collectNetInfo() *NetInfo {
	info := &NetInfo{UpdatedAt: time.Now().Unix()}
	if a.engine.Running() {
		if pi := a.engine.PID(); pi > 0 {
			info.MemoryMB = memUsageMB(pi)
		}
		if a.samples != nil {
			info.Connections = int(a.samples.count())
		}
		info.UpTotal, info.DownTotal = a.hub.Totals()
	}
	// 当前选中节点与它的延迟
	if a.engine.Running() {
		if proxies, _, err := a.clash.ProviderProxyIndex(); err == nil {
			cur := ""
			for name, g := range proxies {
				if isGroupType(g.Type) && name == groupMain {
					cur = g.Now
					break
				}
			}
			// 「节点选择」常常指向「自动选择」，再往下找到真正的节点
			for depth := 0; depth < 5 && cur != ""; depth++ {
				p, ok := proxies[cur]
				if !ok || !isGroupType(p.Type) {
					break
				}
				if p.Now == "" || p.Now == cur {
					break
				}
				cur = p.Now
			}
			info.Node = cur
			// 先用内核已经测出来的延迟（节点页测过速就有），避免重复探测
			if p, ok := proxies[cur]; ok && len(p.History) > 0 {
				last := p.History[len(p.History)-1]
				if last.Delay > 0 {
					info.NodeDelay = last.Delay
					info.NodeDelayAt = last.Time
				}
			}
		}
		if info.Node != "" && info.NodeDelay == 0 {
			// 没有历史值就现场测一次（5 秒超时，慢节点也能测出来）
			if d, err := a.clash.Delay(info.Node, testURL, 5000); err == nil && d > 0 {
				info.NodeDelay = d
			}
		}
	}
	return info
}

// handleNetInfo 返回网络状态；结果缓存 20 秒，避免反复外呼。
func (a *App) handleNetInfo(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") != ""

	a.netInfo.mu.Lock()
	if !force && a.netInfo.data != nil && time.Since(a.netInfo.at) < 60*time.Second {
		data := a.netInfo.data
		a.netInfo.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "info": data, "cached": true})
		return
	}
	if !force && a.netInfo.probing {
		a.netInfo.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "pending": true})
		return
	}
	if !force {
		// 冷启动 / 缓存过期：先返回 pending，后台探测，避免把界面卡住十几秒
		a.netInfo.probing = true
		a.netInfo.mu.Unlock()
		go func() {
			info := a.collectNetInfo()
			a.netInfo.mu.Lock()
			a.netInfo.data = info
			a.netInfo.at = time.Now()
			a.netInfo.probing = false
			a.netInfo.mu.Unlock()
		}()
		writeJSON(w, 200, map[string]any{"ok": true, "pending": true})
		return
	}
	a.netInfo.mu.Unlock()

	// 用户主动刷新：同步探测（前端有按钮，等一下可以接受）
	info := a.collectNetInfo()
	a.netInfo.mu.Lock()
	a.netInfo.data = info
	a.netInfo.at = time.Now()
	a.netInfo.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "info": info})
}

var _ = fmt.Sprintf
var _ = net.ParseIP
