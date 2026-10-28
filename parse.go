package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Proxy 是生成给 mihomo 的单个节点（字段名直接沿用 mihomo 配置键）。
type Proxy = map[string]any

// ParseResult 订阅解析结果。
type ParseResult struct {
	Format        string // clash | base64 | uri-list
	ProviderBytes []byte // 写给 mihomo proxy-provider 的文件内容
	NodeCount     int
	Skipped       int
	Errs          []string
}

const (
	formatClash    = "clash"
	formatBase64   = "base64"
	formatURIList  = "uri-list"
	formatUnknown  = "unknown"
	providerPrefix = "["
)

// ParseSubscription 识别订阅内容并产出 provider 文件内容。
func ParseSubscription(body []byte) (*ParseResult, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errors.New("订阅内容为空")
	}

	// 1) Clash / mihomo 原生配置：交给 mihomo 自己解析，原样存成 provider
	if isClashConfig(trimmed) {
		// 剔除「剩余流量/到期/套餐」这类伪节点，避免它们混进节点列表与分组
		filtered, total, kept := filterClashBody(body)
		if total > 0 && kept == 0 {
			filtered, kept = body, total // 全是伪节点时保底，不清空订阅
		}
		return &ParseResult{
			Format:        formatClash,
			ProviderBytes: filtered,
			NodeCount:     kept,
		}, nil
	}

	// 2) base64 编码的节点列表
	if dec, ok := tryBase64(trimmed); ok && bytes.Contains(dec, []byte("://")) {
		return parseURIList(dec, formatBase64)
	}

	// 3) 明文节点列表
	if bytes.Contains(trimmed, []byte("://")) {
		return parseURIList(trimmed, formatURIList)
	}

	snippet := string(trimmed)
	if len(snippet) > 160 {
		snippet = snippet[:160] + "…"
	}
	return nil, fmt.Errorf("无法识别的订阅格式（既不是 Clash 配置，也不是节点链接列表）：%s", snippet)
}

func isClashConfig(b []byte) bool {
	b = bytes.TrimSpace(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")))
	if len(b) == 0 {
		return false
	}
	// JSON 形态的 Clash 配置
	if b[0] == '{' {
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) == nil {
			if _, ok := m["proxies"]; ok {
				return true
			}
		}
		return false
	}

	// YAML：顶部可能先出现 port / mixed-port / allow-lan 等键，proxies 在后面，
	// 所以不能"遇到第一个顶层键就放弃"，要扫到任一 Clash 顶层键为止。
	clashKeys := []string{
		"proxies:", "proxy-groups:", "proxy-providers:", "rules:",
		"mixed-port:", "port:", "socks-port:", "redir-port:", "tproxy-port:",
		"allow-lan:", "bind-address:", "external-controller:", "mode:", "log-level:",
		"dns:", "tun:", "profile:", "sniffer:",
	}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // 只看顶层键
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		for _, k := range clashKeys {
			if strings.HasPrefix(t, k) {
				return true
			}
		}
	}
	return false
}

// pseudoNodeRe 机场常把「剩余流量 / 到期时间」伪装成节点，这些不是代理，应当剔除。
var pseudoNodeRe = regexp.MustCompile(`剩余流量|距离下次重置|到期|套餐|重置剩余|官网|客服|过期`)

func isPseudoNodeName(name string) bool { return pseudoNodeRe.MatchString(name) }

var nodeNameRe = regexp.MustCompile(`name:\s*(?:'([^']*)'|"([^"]*)"|([^,}]+))`)

// filterClashBody 去掉 Clash 配置里 proxies 段的伪节点，返回过滤后的内容与实际保留数量。
func filterClashBody(b []byte) ([]byte, int, int) {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return filterClashJSON(trimmed)
	}
	return filterClashYAML(b)
}

func filterClashJSON(b []byte) ([]byte, int, int) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		return b, 0, countClashProxies(b)
	}
	raw, ok := doc["proxies"]
	if !ok {
		return b, 0, 0
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return b, 0, countClashProxies(b)
	}
	total := len(list)
	kept := make([]json.RawMessage, 0, total)
	for _, item := range list {
		var meta struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(item, &meta)
		if meta.Name != "" && isPseudoNodeName(meta.Name) {
			continue
		}
		kept = append(kept, item)
	}
	enc, err := json.Marshal(kept)
	if err != nil {
		return b, total, total
	}
	doc["proxies"] = enc
	out, err := json.Marshal(doc)
	if err != nil {
		return b, total, total
	}
	return out, total, len(kept)
}

func filterClashYAML(b []byte) ([]byte, int, int) {
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	inProxies := false
	var item []string
	total, kept := 0, 0

	flush := func() {
		if item == nil {
			return
		}
		total++
		name := ""
		if m := nodeNameRe.FindStringSubmatch(strings.Join(item, " ")); m != nil {
			name = firstNonEmpty(m[1], m[2], strings.TrimSpace(m[3]))
		}
		if name != "" && isPseudoNodeName(name) {
			return // 丢弃
		}
		kept++
		out = append(out, item...)
		item = nil
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		topLevel := line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
		if topLevel {
			flush()
			inProxies = strings.HasPrefix(strings.TrimSpace(line), "proxies:")
			out = append(out, line)
			continue
		}
		if inProxies && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			flush()
			item = []string{line}
			continue
		}
		if item != nil {
			item = append(item, line)
			continue
		}
		out = append(out, line)
	}
	flush()
	return []byte(strings.Join(out, "\n")), total, kept
}

func countClashProxies(b []byte) int {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var doc struct {
			Proxies []json.RawMessage `json:"proxies"`
		}
		if json.Unmarshal(trimmed, &doc) == nil {
			return len(doc.Proxies)
		}
		return 0
	}

	// YAML：定位顶格的 proxies:，然后统计该块内的序列项。
	// 不依赖 "- name:" 这种写法——很多机场输出的是 "- { name: '...' }"（{ 后面有空格）。
	n := 0
	inProxies := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		topLevel := !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
		if topLevel {
			inProxies = strings.HasPrefix(strings.TrimSpace(line), "proxies:")
			continue
		}
		if !inProxies {
			continue
		}
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || t == "-" || strings.HasPrefix(t, "-{") {
			n++
		}
	}
	return n
}

func tryBase64(b []byte) ([]byte, bool) {
	s := strings.TrimSpace(string(b))
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, e := range encodings {
		if dec, err := e.DecodeString(s); err == nil && len(dec) > 0 {
			return dec, true
		}
	}
	return nil, false
}

func b64dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return base64.StdEncoding.DecodeString(s)
}

// parseURIList 逐行解析 ss/vmess/vless/trojan/hysteria2/tuic 等链接。
func parseURIList(body []byte, format string) (*ParseResult, error) {
	var proxies []Proxy
	var errs []string
	skipped := 0
	seen := map[string]int{}

	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		p, err := parseURI(line)
		if err != nil {
			skipped++
			if len(errs) < 6 {
				errs = append(errs, err.Error())
			}
			continue
		}
		name, _ := p["name"].(string)
		if name == "" {
			name = fmt.Sprintf("%s-%v", strings.ToUpper(fmt.Sprint(p["type"])), p["server"])
			p["name"] = name
		}
		if c, dup := seen[name]; dup {
			seen[name] = c + 1
			p["name"] = fmt.Sprintf("%s #%d", name, c+1)
		} else {
			seen[name] = 1
		}
		proxies = append(proxies, p)
	}

	if len(proxies) == 0 {
		msg := "订阅里没有解析出可用节点"
		if len(errs) > 0 {
			msg += "：" + strings.Join(errs, "；")
		}
		return nil, errors.New(msg)
	}

	content, err := json.Marshal(map[string]any{"proxies": proxies})
	if err != nil {
		return nil, err
	}
	return &ParseResult{
		Format:        format,
		ProviderBytes: content,
		NodeCount:     len(proxies),
		Skipped:       skipped,
		Errs:          errs,
	}, nil
}

func parseURI(link string) (Proxy, error) {
	switch {
	case strings.HasPrefix(link, "ss://"):
		return parseSS(link)
	case strings.HasPrefix(link, "ssr://"):
		return parseSSR(link)
	case strings.HasPrefix(link, "vmess://"):
		return parseVmess(link)
	case strings.HasPrefix(link, "vless://"):
		return parseVless(link)
	case strings.HasPrefix(link, "trojan://"):
		return parseTrojan(link)
	case strings.HasPrefix(link, "hysteria2://"), strings.HasPrefix(link, "hy2://"):
		return parseHysteria2(link)
	case strings.HasPrefix(link, "hysteria://"):
		return parseHysteria1(link)
	case strings.HasPrefix(link, "tuic://"):
		return parseTuic(link)
	case strings.HasPrefix(link, "socks5://"), strings.HasPrefix(link, "socks://"):
		return parseSocks(link)
	default:
		scheme := link
		if i := strings.Index(link, "://"); i > 0 {
			scheme = link[:i+3]
		}
		return nil, fmt.Errorf("不支持的协议 %s", scheme)
	}
}

// splitName 取出 #fragment 作为节点名，并返回去掉 fragment 的链接。
func splitName(link string) (string, string) {
	if i := strings.Index(link, "#"); i >= 0 {
		name := link[i+1:]
		if u, err := url.QueryUnescape(name); err == nil {
			name = u
		}
		return strings.TrimSpace(name), link[:i]
	}
	return "", link
}

func atoi(s string) int { v, _ := strconv.Atoi(strings.TrimSpace(s)); return v }

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func parseSS(link string) (Proxy, error) {
	name, body := splitName(link)
	body = strings.TrimPrefix(body, "ss://")

	// 拆出 ?plugin=...
	var query string
	if i := strings.Index(body, "?"); i >= 0 {
		query = body[i+1:]
		body = body[:i]
	}

	var method, password, hostport string
	if i := strings.LastIndex(body, "@"); i >= 0 {
		// SIP002: base64(method:password)@host:port
		cred := body[:i]
		hostport = body[i+1:]
		dec, err := b64dec(cred)
		if err != nil {
			if u, e2 := url.QueryUnescape(cred); e2 == nil && strings.Contains(u, ":") {
				dec = []byte(u)
			} else {
				return nil, fmt.Errorf("ss 凭据解析失败: %v", err)
			}
		}
		parts := strings.SplitN(string(dec), ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("ss 凭据格式错误")
		}
		method, password = parts[0], parts[1]
	} else {
		// legacy: base64(method:password@host:port)
		dec, err := b64dec(body)
		if err != nil {
			return nil, errors.New("ss 链接解析失败")
		}
		s := string(dec)
		i := strings.LastIndex(s, "@")
		if i < 0 {
			return nil, errors.New("ss 链接缺少 @")
		}
		parts := strings.SplitN(s[:i], ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("ss 凭据格式错误")
		}
		method, password = parts[0], parts[1]
		hostport = s[i+1:]
	}
	hostport = strings.TrimSuffix(hostport, "/")
	host, port, err := splitHostPort(hostport)
	if err != nil {
		return nil, err
	}

	p := Proxy{
		"name":   name,
		"type":   "ss",
		"server": host,
		"port":   port,
		"cipher": method,
		"udp":    true,
	}
	if password != "" {
		p["password"] = password
	}

	if query != "" {
		q, _ := url.ParseQuery(query)
		if plugin := q.Get("plugin"); plugin != "" {
			applySSPlugin(p, plugin)
		}
	}
	return p, nil
}

func applySSPlugin(p Proxy, plugin string) {
	// plugin=obfs-local;obfs=http;obfs-host=xxx
	fields := strings.Split(plugin, ";")
	kind := strings.TrimSpace(fields[0])
	opts := map[string]string{}
	for _, f := range fields[1:] {
		if kv := strings.SplitN(f, "=", 2); len(kv) == 2 {
			opts[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		} else if strings.TrimSpace(f) != "" {
			opts[strings.TrimSpace(f)] = ""
		}
	}
	switch kind {
	case "obfs-local", "simple-obfs", "obfs":
		mode := opts["obfs"]
		if mode == "" {
			mode = "http"
		}
		p["plugin"] = "obfs"
		p["plugin-opts"] = map[string]any{"mode": mode, "host": opts["obfs-host"]}
	case "v2ray-plugin", "xray-plugin":
		po := map[string]any{"mode": "websocket"}
		if h := opts["host"]; h != "" {
			po["host"] = h
		}
		if pa := opts["path"]; pa != "" {
			po["path"] = pa
		}
		if _, ok := opts["tls"]; ok {
			po["tls"] = true
		}
		p["plugin"] = "v2ray-plugin"
		p["plugin-opts"] = po
	}
}

func parseVmess(link string) (Proxy, error) {
	raw := strings.TrimPrefix(link, "vmess://")
	if strings.Contains(raw, "%") {
		if un, err := url.PathUnescape(raw); err == nil {
			raw = un
		}
	}
	dec, err := b64dec(raw)
	if err != nil {
		return nil, errors.New("vmess 链接 base64 解析失败")
	}
	var v struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		Aid  any    `json:"aid"`
		Scy  string `json:"scy"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
		ALPN string `json:"alpn"`
		FP   string `json:"fp"`
	}
	if err := json.Unmarshal(dec, &v); err != nil {
		return nil, errors.New("vmess 参数解析失败")
	}
	if v.Add == "" || v.ID == "" {
		return nil, errors.New("vmess 缺少地址或 uuid")
	}
	port := toInt(v.Port)
	if port <= 0 {
		return nil, errors.New("vmess 端口非法")
	}
	p := Proxy{
		"name":    strings.TrimSpace(v.PS),
		"type":    "vmess",
		"server":  v.Add,
		"port":    port,
		"uuid":    v.ID,
		"alterId": toInt(v.Aid),
		"cipher":  firstNonEmpty(v.Scy, "auto"),
		"udp":     true,
	}
	if strings.EqualFold(v.TLS, "tls") || strings.EqualFold(v.Type, "tls") {
		p["tls"] = true
		if v.SNI != "" {
			p["servername"] = v.SNI
		} else if v.Host != "" && !isIPAddress(v.Host) {
			p["servername"] = v.Host
		}
	}
	if v.FP != "" {
		p["client-fingerprint"] = v.FP
	}
	if v.ALPN != "" {
		p["alpn"] = strings.Split(v.ALPN, ",")
	}
	applyTransport(p, v.Net, v.Host, v.Path, v.Type, v.SNI)
	return p, nil
}

func parseVless(link string) (Proxy, error) {
	name, body := splitName(link)
	u, err := url.Parse(body)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return nil, errors.New("vless 链接格式错误")
	}
	q := u.Query()
	p := Proxy{
		"name":   name,
		"type":   "vless",
		"server": u.Hostname(),
		"port":   atoi(u.Port()),
		"uuid":   u.User.Username(),
		"udp":    true,
	}
	if p["port"].(int) <= 0 {
		return nil, errors.New("vless 端口非法")
	}
	security := q.Get("security")
	flow := q.Get("flow")
	if flow != "" {
		p["flow"] = flow
	}
	sni := q.Get("sni")
	if security == "tls" || security == "reality" || security == "xtls" {
		p["tls"] = true
		if sni != "" {
			p["servername"] = sni
		}
	}
	if fp := q.Get("fp"); fp != "" {
		p["client-fingerprint"] = fp
	}
	if security == "reality" {
		pbk := q.Get("pbk")
		if !validRealityKey(pbk) {
			return nil, errors.New("reality 公钥非法（需 32 字节 base64）")
		}
		if sni == "" {
			return nil, errors.New("reality 节点缺少 sni")
		}
		p["reality-opts"] = map[string]any{
			"public-key": pbk,
			"short-id":   q.Get("sid"),
		}
	}
	if v := q.Get("allowInsecure"); truthy(v) {
		p["skip-cert-verify"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = strings.Split(alpn, ",")
	}
	applyTransport(p, q.Get("type"), q.Get("host"), q.Get("path"), "", q.Get("serviceName"))
	if sn := q.Get("packetEncoding"); sn != "" {
		p["packet-encoding"] = sn
	}
	return p, nil
}

func parseTrojan(link string) (Proxy, error) {
	name, body := splitName(link)
	u, err := url.Parse(body)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return nil, errors.New("trojan 链接格式错误")
	}
	q := u.Query()
	pw, _ := url.QueryUnescape(u.User.Username())
	p := Proxy{
		"name":     name,
		"type":     "trojan",
		"server":   u.Hostname(),
		"port":     atoi(u.Port()),
		"password": pw,
		"udp":      true,
	}
	if p["port"].(int) <= 0 {
		return nil, errors.New("trojan 端口非法")
	}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer")); sni != "" {
		p["sni"] = sni
	}
	if truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")) || truthy(q.Get("skip-cert-verify")) {
		p["skip-cert-verify"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = strings.Split(alpn, ",")
	}
	if fp := q.Get("fp"); fp != "" {
		p["client-fingerprint"] = fp
	}
	applyTransport(p, q.Get("type"), q.Get("host"), q.Get("path"), "", q.Get("serviceName"))
	return p, nil
}

func parseHysteria2(link string) (Proxy, error) {
	name, body := splitName(link)
	body = strings.TrimPrefix(strings.TrimPrefix(body, "hysteria2://"), "hy2://")
	u, err := url.Parse("hy2://" + body)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("hysteria2 链接格式错误")
	}
	q := u.Query()
	auth := ""
	if u.User != nil {
		auth, _ = url.QueryUnescape(u.User.Username())
		if pw, ok := u.User.Password(); ok && pw != "" {
			auth += ":" + pw
		}
	}
	p := Proxy{
		"name":     name,
		"type":     "hysteria2",
		"server":   u.Hostname(),
		"port":     atoi(u.Port()),
		"password": auth,
		"udp":      true,
	}
	if p["port"].(int) <= 0 {
		return nil, errors.New("hysteria2 端口非法")
	}
	if sni := q.Get("sni"); sni != "" {
		p["sni"] = sni
	}
	if truthy(q.Get("insecure")) || truthy(q.Get("allowInsecure")) {
		p["skip-cert-verify"] = true
	}
	if obfs := q.Get("obfs"); obfs != "" {
		p["obfs"] = obfs
		p["obfs-password"] = q.Get("obfs-password")
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = strings.Split(alpn, ",")
	}
	return p, nil
}

func parseTuic(link string) (Proxy, error) {
	name, body := splitName(link)
	u, err := url.Parse(body)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("tuic 链接格式错误")
	}
	q := u.Query()
	uuid, pw := "", ""
	if u.User != nil {
		uuid = u.User.Username()
		pw, _ = u.User.Password()
	}
	p := Proxy{
		"name":     name,
		"type":     "tuic",
		"server":   u.Hostname(),
		"port":     atoi(u.Port()),
		"uuid":     uuid,
		"password": pw,
		"udp":      true,
	}
	if p["port"].(int) <= 0 {
		return nil, errors.New("tuic 端口非法")
	}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer")); sni != "" {
		p["sni"] = sni
	}
	if truthy(q.Get("allow_insecure")) || truthy(q.Get("allowInsecure")) {
		p["skip-cert-verify"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = strings.Split(alpn, ",")
	}
	if cc := q.Get("congestion_control"); cc != "" {
		p["congestion-controller"] = cc
	}
	if urm := q.Get("udp_relay_mode"); urm != "" {
		p["udp-relay-mode"] = urm
	}
	return p, nil
}

func parseSocks(link string) (Proxy, error) {
	name, body := splitName(link)
	u, err := url.Parse(body)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("socks5 链接格式错误")
	}
	p := Proxy{
		"name":   name,
		"type":   "socks5",
		"server": u.Hostname(),
		"port":   atoi(u.Port()),
		"udp":    true,
	}
	if u.User != nil {
		p["username"] = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			p["password"] = pw
		}
	}
	return p, nil
}

// applyTransport 处理 ws/grpc/h2/tcp 传输层参数。
func applyTransport(p Proxy, netw, host, path, vtype, extra string) {
	switch strings.ToLower(netw) {
	case "ws", "websocket":
		p["network"] = "ws"
		opts := map[string]any{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		if len(opts) > 0 {
			p["ws-opts"] = opts
		}
	case "grpc", "gun":
		p["network"] = "grpc"
		sn := extra
		if sn == "" {
			sn = path
		}
		if sn != "" {
			p["grpc-opts"] = map[string]any{"grpc-service-name": sn}
		}
	case "h2", "http":
		p["network"] = "h2"
		opts := map[string]any{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["host"] = []string{host}
		}
		if len(opts) > 0 {
			p["h2-opts"] = opts
		}
	default:
		// tcp 等：保持默认
	}
}

func splitHostPort(hp string) (string, int, error) {
	i := strings.LastIndex(hp, ":")
	if i <= 0 {
		return "", 0, errors.New("地址缺少端口")
	}
	host := strings.Trim(hp[:i], "[]")
	port := atoi(hp[i+1:])
	if port <= 0 {
		return "", 0, errors.New("端口非法")
	}
	return host, port, nil
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		return atoi(t)
	}
	return 0
}

func isIPAddress(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if n := atoi(p); n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// validRealityKey 校验 REALITY 公钥：必须是 32 字节的 base64/base64url。
func validRealityKey(k string) bool {
	if k == "" {
		return false
	}
	enc := base64.RawURLEncoding
	dec, err := enc.DecodeString(strings.TrimRight(k, "="))
	if err != nil {
		if d2, e2 := base64.RawStdEncoding.DecodeString(strings.TrimRight(k, "=")); e2 == nil {
			dec = d2
		} else {
			return false
		}
	}
	return len(dec) == 32
}

// parseSSR 解析 ssr://（老机场仍常见）：base64(host:port:proto:method:obfs:base64(pass)/?params)
func parseSSR(link string) (Proxy, error) {
	raw := strings.TrimPrefix(link, "ssr://")
	dec, err := b64dec(raw)
	if err != nil {
		return nil, errors.New("ssr 链接 base64 解析失败")
	}
	main := string(dec)
	query := ""
	if i := strings.Index(main, "/?"); i >= 0 {
		query = main[i+2:]
		main = main[:i]
	}
	parts := strings.Split(main, ":")
	if len(parts) < 6 {
		return nil, errors.New("ssr 链接字段不足")
	}
	host := parts[0]
	port := atoi(parts[1])
	proto, method, obfs := parts[2], parts[3], parts[4]
	pwB64 := strings.Join(parts[5:], ":")
	if port <= 0 {
		return nil, errors.New("ssr 端口非法")
	}
	pw, err := b64dec(pwB64)
	if err != nil {
		pw = []byte(pwB64)
	}

	p := Proxy{
		"type":     "ssr",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": string(pw),
		"protocol": proto,
		"obfs":     obfs,
		"udp":      true,
	}
	q := url.Values{}
	if query != "" {
		if v, err := url.ParseQuery(query); err == nil {
			q = v
		}
	}
	decParam := func(k string) string {
		v := q.Get(k)
		if v == "" {
			return ""
		}
		// 注意：ParseQuery 已经解过一层，这里只在仍有 % 时才再解，
		// 否则 base64 里的 '+' 会被当成空格
		if strings.Contains(v, "%") {
			if un, err := url.QueryUnescape(v); err == nil {
				v = un
			}
		}
		candidates := []string{v, strings.NewReplacer("-", "+", "_", "/").Replace(v)}
		for _, c := range candidates {
			if b, err := b64dec(c); err == nil && len(b) > 0 {
				return string(b)
			}
		}
		return v
	}
	if name := decParam("remarks"); name != "" {
		p["name"] = name
	}
	if op := decParam("obfsparam"); op != "" {
		p["obfs-param"] = op
	}
	if pp := decParam("protoparam"); pp != "" {
		p["protocol-param"] = pp
	}
	return p, nil
}

// parseHysteria1 解析 hysteria://（v1，mihomo 用 type: hysteria）
func parseHysteria1(link string) (Proxy, error) {
	name, body := splitName(link)
	u, err := url.Parse(body)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("hysteria 链接格式错误")
	}
	q := u.Query()
	port := atoi(u.Port())
	if port <= 0 {
		return nil, errors.New("hysteria 端口非法")
	}
	p := Proxy{
		"name":     name,
		"type":     "hysteria",
		"server":   u.Hostname(),
		"port":     port,
		"udp":      true,
		"protocol": firstNonEmpty(q.Get("protocol"), "udp"),
	}
	if auth := q.Get("auth"); auth != "" {
		p["auth-str"] = auth
	} else if auth := q.Get("auth_str"); auth != "" {
		p["auth-str"] = auth
	}
	if peer := firstNonEmpty(q.Get("peer"), q.Get("sni")); peer != "" {
		p["sni"] = peer
	}
	if truthy(q.Get("insecure")) || truthy(q.Get("allowInsecure")) {
		p["skip-cert-verify"] = true
	}
	if v := atoi(q.Get("upmbps")); v > 0 {
		p["up"] = v
	}
	if v := atoi(q.Get("downmbps")); v > 0 {
		p["down"] = v
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = strings.Split(alpn, ",")
	}
	return p, nil
}
