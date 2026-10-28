package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleClash = `proxies:
  - {name: "香港 01", type: ss, server: 1.1.1.1, port: 8388, cipher: aes-256-gcm, password: "pass1", udp: true}
  - {name: "日本 02", type: trojan, server: 2.2.2.2, port: 443, password: "pass2", sni: jp.example.com}
proxy-groups:
  - {name: "机场A-自动", type: url-test, proxies: ["香港 01", "日本 02"]}
rules:
  - MATCH,DIRECT
`

const sampleURIList = `ss://YWVzLTEyOC1nY206cHcx@9.9.9.9:8388#新加坡 01
vmess://eyJ2IjoiMiIsInBzIjoi5Y+w5rm+IDA0IiwiYWRkIjoiNC40LjQuNCIsInBvcnQiOiI0NDMiLCJpZCI6IjExMTExMTExLTIyMjItMzMzMy00NDQ0LTU1NTU1NTU1NTU1NSIsImFpZCI6IjAiLCJzY3kiOiJhdXRvIiwibmV0Ijoid3MiLCJ0eXBlIjoibm9uZSIsImhvc3QiOiJ0dy5leGFtcGxlLmNvbSIsInBhdGgiOiIvcmF5IiwidGxzIjoidGxzIiwic25pIjoidHcuZXhhbXBsZS5jb20iLCJhbHBuIjoiaDIsaHR0cC8xLjEiLCJmcCI6ImNocm9tZSJ9
vless://11111111-2222-3333-4444-555555555555@5.5.5.5:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0&sid=0123&type=tcp&flow=xtls-rprx-vision#香港 Reality
trojan://pw3@6.6.6.6:443?sni=hk.example.com&allowInsecure=1&type=ws&path=/tj#香港 Trojan
hysteria2://auth123@7.7.7.7:443?sni=hy.example.com&insecure=1#美国 Hy2
tuic://11111111-2222-3333-4444-555555555555:pw4@8.8.8.8:443?sni=tuic.example.com&alpn=h3&congestion_control=bbr#日本 Tuic
`

func TestParseURIList(t *testing.T) {
	res, err := parseURIList([]byte(sampleURIList), formatURIList)
	if err != nil {
		t.Fatalf("parseURIList: %v", err)
	}
	if res.NodeCount != 6 {
		t.Fatalf("期望 6 个节点，实际 %d（skipped=%d errs=%v）", res.NodeCount, res.Skipped, res.Errs)
	}
	var doc struct {
		Proxies []Proxy `json:"proxies"`
	}
	if err := json.Unmarshal(res.ProviderBytes, &doc); err != nil {
		t.Fatalf("provider 内容不是合法 JSON: %v", err)
	}
	byName := map[string]Proxy{}
	for _, p := range doc.Proxies {
		byName[p["name"].(string)] = p
	}
	if _, ok := byName["新加坡 01"]; !ok {
		t.Errorf("缺少 ss 节点，实际有 %v", keys(byName))
	}
	if vm, ok := byName["台湾 04"]; !ok {
		t.Errorf("缺少 vmess 节点")
	} else {
		if vm["network"] != "ws" {
			t.Errorf("vmess 传输层应为 ws，实际 %v", vm["network"])
		}
		if vm["tls"] != true {
			t.Errorf("vmess 应开启 tls")
		}
	}
	if vl, ok := byName["香港 Reality"]; ok {
		if vl["reality-opts"] == nil {
			t.Errorf("vless reality 缺少 reality-opts")
		}
		if vl["flow"] != "xtls-rprx-vision" {
			t.Errorf("vless flow 解析错误: %v", vl["flow"])
		}
	} else {
		t.Errorf("缺少 vless 节点")
	}
	if tj, ok := byName["香港 Trojan"]; ok {
		if tj["skip-cert-verify"] != true {
			t.Errorf("trojan allowInsecure 未映射")
		}
		if tj["network"] != "ws" {
			t.Errorf("trojan ws 未映射: %v", tj["network"])
		}
	} else {
		t.Errorf("缺少 trojan 节点")
	}
}

func TestParseClashPassthrough(t *testing.T) {
	res, err := ParseSubscription([]byte(sampleClash))
	if err != nil {
		t.Fatalf("ParseSubscription: %v", err)
	}
	if res.Format != formatClash {
		t.Fatalf("格式判断错误: %s", res.Format)
	}
	if res.NodeCount != 2 {
		t.Fatalf("clash 节点计数错误: %d", res.NodeCount)
	}
	if string(res.ProviderBytes) != sampleClash {
		t.Fatalf("clash 订阅应原样透传")
	}
}

func TestParseBase64Subscription(t *testing.T) {
	body := b64([]byte(sampleURIList))
	res, err := ParseSubscription(body)
	if err != nil {
		t.Fatalf("ParseSubscription(base64): %v", err)
	}
	if res.Format != formatBase64 {
		t.Fatalf("应识别为 base64，实际 %s", res.Format)
	}
	if res.NodeCount != 6 {
		t.Fatalf("节点数错误: %d", res.NodeCount)
	}
}

func TestParseUnsupported(t *testing.T) {
	if _, err := ParseSubscription([]byte("<html>404</html>")); err == nil {
		t.Fatal("应当报错")
	}
}

func TestMaskURL(t *testing.T) {
	in := "https://a.example.com/api/v1/client/subscribe?token=secret123&flag=clash"
	if got := maskURL(in, false); strings.Contains(got, "secret123") {
		t.Fatalf("token 未打码: %s", got)
	}
	if got := maskURL(in, true); got != in {
		t.Fatalf("reveal 应返回原文: %s", got)
	}
}

func TestParseUserInfo(t *testing.T) {
	tr := parseUserInfo("upload=100; download=200; total=1000; expire=1793036000")
	if tr == nil || tr.Upload != 100 || tr.Download != 200 || tr.Total != 1000 || tr.Expire != 1793036000 {
		t.Fatalf("subscription-userinfo 解析错误: %+v", tr)
	}
	if parseUserInfo("") != nil {
		t.Fatal("空头应返回 nil")
	}
}

func b64(b []byte) []byte {
	enc := make([]byte, 0, len(b)*2)
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := 0; i < len(b); i += 3 {
		var n, rem int
		if i+3 <= len(b) {
			n = int(b[i])<<16 | int(b[i+1])<<8 | int(b[i+2])
			rem = 3
		} else if i+2 <= len(b) {
			n = int(b[i])<<16 | int(b[i+1])<<8
			rem = 2
		} else {
			n = int(b[i]) << 16
			rem = 1
		}
		enc = append(enc, table[(n>>18)&63], table[(n>>12)&63])
		if rem > 1 {
			enc = append(enc, table[(n>>6)&63])
		} else {
			enc = append(enc, '=')
		}
		if rem > 2 {
			enc = append(enc, table[n&63])
		} else {
			enc = append(enc, '=')
		}
	}
	return enc
}

func keys(m map[string]Proxy) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------- 与 mihomo 的集成测试 ----------

func mihomoBin(t *testing.T) string {
	p := os.Getenv("MM_MIHOMO")
	if p == "" {
		p = "/koolshare/bin/mihomo"
	}
	if !fileExists(p) {
		t.Skip("未设置 MM_MIHOMO，跳过与真实内核的集成测试")
	}
	return p
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a := &App{dataDir: dir}
	a.settings = defaultSettings()
	a.settings.ClashSecret = "test-secret"
	// 用随机空闲端口，避免上一次测试残留的进程占着端口
	a.settings.RedirPort = freePort(t)
	a.settings.MixedPort = freePort(t)
	a.settings.APIPort = freePort(t)
	a.settings.LanIf = "br0"
	a.mihomoPath = mihomoBin(t)
	_ = os.MkdirAll(filepath.Join(dir, "providers"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "rules"), 0o755)
	a.clash = NewClashClient("127.0.0.1:"+itoa(a.settings.APIPort), a.settings.ClashSecret)
	a.engine = NewEngine(a)
	a.hub = NewHub(a)
	a.samples = newSampler(a)
	a.logRing = newRingBuffer(100)
	return a
}

// TestFullFlowWithRealCore 用 httptest 提供订阅，真跑 mihomo 内核。
func TestFullFlowWithRealCore(t *testing.T) {
	a := newTestApp(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clash.yaml":
			w.Header().Set("subscription-userinfo", "upload=1024; download=2048; total=10737418240; expire=1793036000")
			_, _ = w.Write([]byte(sampleFullClash))
		case "/sub.txt":
			_, _ = w.Write(b64([]byte(sampleURIList)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a.subs = []Sub{
		{ID: "s_a", Name: "机场A", URL: srv.URL + "/clash.yaml", Interval: 0, Enabled: true, Status: "never"},
		{ID: "s_b", Name: "机场B", URL: srv.URL + "/sub.txt", Interval: 0, Enabled: true, Status: "never"},
	}
	for i := range a.subs {
		if err := a.refreshSubByID(a.subs[i].ID); err != nil {
			t.Fatalf("订阅 %s 刷新失败: %v", a.subs[i].Name, err)
		}
	}
	if a.subs[0].NodeCount != 2 {
		t.Errorf("机场A 节点数 %d，期望 2", a.subs[0].NodeCount)
	}
	if a.subs[1].NodeCount != 6 {
		t.Errorf("机场B 节点数 %d，期望 6", a.subs[1].NodeCount)
	}
	if a.subs[0].Traffic == nil || a.subs[0].Traffic.Total != 10737418240 {
		t.Errorf("流量信息未解析: %+v", a.subs[0].Traffic)
	}

	cfg, err := a.BuildConfig()
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	providers := cfg["proxy-providers"].(map[string]any)
	if len(providers) != 2 {
		t.Fatalf("provider 数量 %d，期望 2", len(providers))
	}
	groups := cfg["proxy-groups"].([]any)
	if len(groups) != 4 { // 主 + 自动 + 2 个子订阅组
		t.Fatalf("分组数量 %d，期望 4", len(groups))
	}

	// 写盘并让真实内核校验 + 启动
	if err := a.WriteConfig(); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	if out, err := run(60e9, a.mihomoPath, "-t", "-d", a.dataDir, "-f", a.configPath()); err != nil {
		t.Fatalf("mihomo 校验失败: %v\n%s", err, out)
	}
	if err := a.engine.Start(); err != nil {
		t.Fatalf("启动内核失败: %v", err)
	}
	defer func() { _ = a.engine.Stop() }()

	proxies, err := a.clash.Proxies()
	if err != nil {
		t.Fatalf("读取节点失败: %v", err)
	}
	main, ok := proxies[groupMain]
	if !ok {
		t.Fatalf("找不到主分组 %s", groupMain)
	}
	if len(main.All) < 8 {
		t.Fatalf("主分组节点数偏少: %v", main.All)
	}
	found := false
	for _, n := range main.All {
		if strings.HasPrefix(n, "[机场B] ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("节点名未带订阅前缀: %v", main.All)
	}

	if _, err := a.clash.Delay("[机场A] 香港 01", testURLConst, 1500); err == nil {
		t.Log("延迟测试意外成功（假服务器不应连通）")
	}

	rules, err := a.clash.Rules()
	if err != nil || len(rules) == 0 {
		t.Fatalf("读取规则失败: %v", err)
	}

	// 热重载
	if err := a.engine.Reload(); err != nil {
		t.Fatalf("热重载失败: %v", err)
	}
	if _, err := a.clash.Proxies(); err != nil {
		t.Fatalf("重载后内核不可用: %v", err)
	}
}

// TestEachGeneratedProxy 逐个节点交给真实内核校验，定位生成字段的错误。
func TestEachGeneratedProxy(t *testing.T) {
	bin := mihomoBin(t)
	dir := t.TempDir()
	for _, line := range strings.Split(strings.TrimSpace(sampleURIList), "\n") {
		p, err := parseURI(strings.TrimSpace(line))
		if err != nil {
			t.Fatalf("解析失败 %q: %v", line[:20], err)
		}
		name, _ := p["name"].(string)
		prov := filepath.Join(dir, "p.json")
		content, _ := json.Marshal(map[string]any{"proxies": []Proxy{p}})
		if err := os.WriteFile(prov, content, 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := map[string]any{
			"mixed-port": 17891,
			"mode":       "rule",
			"proxy-providers": map[string]any{
				"p": map[string]any{"type": "file", "path": prov},
			},
			"proxy-groups": []any{map[string]any{"name": "G", "type": "select", "use": []any{"p"}}},
			"rules":        []any{"MATCH,G"},
		}
		cfgPath := filepath.Join(dir, "c.json")
		b, _ := json.Marshal(cfg)
		_ = os.WriteFile(cfgPath, b, 0o644)
		out, err := run(60e9, bin, "-t", "-d", dir, "-f", cfgPath)
		if err != nil {
			t.Errorf("节点 %q 校验失败: %v\n%s", name, err, out)
		}
	}
}

// 真实机场常见：整份 Clash 配置，port/mode 等键在 proxies 之前
const sampleFullClash = `mixed-port: 7890
allow-lan: true
bind-address: '*'
mode: rule
log-level: info
external-controller: '127.0.0.1:9090'
dns:
  enable: false
proxies:
  - {name: "香港 01", type: ss, server: 1.1.1.1, port: 8388, cipher: aes-256-gcm, password: "p1"}
  - {name: "日本 02", type: trojan, server: 2.2.2.2, port: 443, password: "p2"}
proxy-groups:
  - {name: "PROXY", type: select, proxies: ["香港 01", "日本 02"]}
rules:
  - MATCH,PROXY
`

func TestClashWithLeadingKeys(t *testing.T) {
	res, err := ParseSubscription([]byte(sampleFullClash))
	if err != nil {
		t.Fatalf("完整 Clash 配置应被识别: %v", err)
	}
	if res.Format != formatClash {
		t.Fatalf("格式应为 clash，实际 %s", res.Format)
	}
	if res.NodeCount != 2 {
		t.Fatalf("节点数应为 2，实际 %d", res.NodeCount)
	}
}

func TestClashWithBOM(t *testing.T) {
	body := append([]byte{0xef, 0xbb, 0xbf}, []byte(sampleFullClash)...)
	res, err := ParseSubscription(body)
	if err != nil || res.Format != formatClash {
		t.Fatalf("带 BOM 的 Clash 配置应被识别: %v %v", err, res)
	}
}

func TestURIListStillDetected(t *testing.T) {
	// 链接列表里出现 http:// 之类的内容不应被误判成 Clash 配置
	res, err := ParseSubscription([]byte(sampleURIList))
	if err != nil {
		t.Fatalf("链接列表解析失败: %v", err)
	}
	if res.Format != formatURIList {
		t.Fatalf("应识别为 uri-list，实际 %s", res.Format)
	}
}

func TestClashProviderOnly(t *testing.T) {
	body := []byte("proxies:\n  - {name: '仅节点', type: ss, server: 3.3.3.3, port: 8388, cipher: aes-128-gcm, password: x}\n")
	res, err := ParseSubscription(body)
	if err != nil || res.Format != formatClash || res.NodeCount != 1 {
		t.Fatalf("只有 proxies 的文件应识别为 clash 且 1 个节点: %+v %v", res, err)
	}
}

// freePort 取一个当前空闲的本地端口。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取空闲端口失败: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
