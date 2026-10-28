package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- 测试替身 ----------

// newBackendTestApp 构造一个不依赖真实 mihomo 内核的 App，
// 这些接口在「内核未运行」时也必须给出 200，所以不需要真的拉起内核。
func newBackendTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a := &App{dataDir: dir, startedAt: time.Now()}
	a.settings = defaultSettings()
	a.settings.ClashSecret = "test-secret"
	a.settings.RedirPort = freePort(t)
	a.settings.MixedPort = freePort(t)
	a.settings.APIPort = freePort(t)
	a.settings.LanIf = "br0"
	a.mihomoPath = filepath.Join(dir, "mihomo-not-installed")
	a.clash = NewClashClient("127.0.0.1:"+itoa(a.settings.APIPort), a.settings.ClashSecret)
	a.engine = NewEngine(a)
	a.hub = NewHub(a)
	a.samples = newSampler(a)
	a.logRing = newRingBuffer(100)
	_ = os.MkdirAll(filepath.Join(dir, "providers"), 0o755)
	return a
}

// testRoutes 只注册本次改动涉及的路由，顺便验证路由 pattern 合法。
func testRoutes(a *App) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/connections", a.handleConnections)
	mux.HandleFunc("GET /api/service", a.handleService)
	mux.HandleFunc("GET /api/intranet", a.handleIntranet)
	mux.HandleFunc("GET /api/ipquality", a.handleIPQuality)
	return mux
}

// getJSON 走一遍真实的 ServeMux，返回状态码与解析后的 JSON。
func getJSON(t *testing.T, a *App, target string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	testRoutes(a).ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s 返回的不是合法 JSON（%d）：%s", target, rec.Code, rec.Body.String())
	}
	return rec.Code, body
}

// serverClient 把 ClashClient 指向 httptest 替身。
func serverClient(t *testing.T, srv *httptest.Server) *ClashClient {
	t.Helper()
	return NewClashClient(strings.TrimPrefix(srv.URL, "http://"), "test-secret")
}

// portOfURL 取出监听端口，用于把「经代理」探测指向本地替身。
func portOfURL(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析替身地址失败: %v", err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("替身地址没有端口: %v", raw)
	}
	return p
}

// writeFakeNvram 写一个假的 nvram 脚本，模拟路由器的 nvram 输出。
func writeFakeNvram(t *testing.T, out string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nvram")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+out+"\n"), 0o755); err != nil {
		t.Fatalf("写假 nvram 失败: %v", err)
	}
	return p
}

// mustJSONLine 把响应压成一行，便于测试日志里直接看懂返回形状。
func mustJSONLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	return string(b)
}

// ifaceWithIPv4 找一块带 IPv4 地址的网卡，用于验证 nvram 失败时的回退。
func ifaceWithIPv4(t *testing.T) (string, string) {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("枚举网卡失败: %v", err)
	}
	for _, ifi := range ifaces {
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			ipnet, ok := ad.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			return ifi.Name, ip4.String()
		}
	}
	t.Skip("本机没有带 IPv4 的网卡，跳过回退用例")
	return "", ""
}

// ---------- 1) /api/connections ----------

func TestConnectionsKernelDownReturns200(t *testing.T) {
	a := newBackendTestApp(t)
	code, body := getJSON(t, a, "/api/connections")
	if code != 200 {
		t.Fatalf("内核未运行时必须返回 200，实际 %d", code)
	}
	if body["ok"] != true {
		t.Errorf("ok 应为 true，实际 %v", body["ok"])
	}
	if n, _ := body["count"].(float64); n != 0 {
		t.Errorf("count 应为 0，实际 %v", body["count"])
	}
	if n, _ := body["up"].(float64); n != 0 {
		t.Errorf("up 应为 0，实际 %v", body["up"])
	}
	rows, ok := body["rows"].([]any)
	if !ok {
		t.Fatalf("rows 应为数组，实际 %T", body["rows"])
	}
	if len(rows) != 0 {
		t.Errorf("rows 应为空，实际 %v", rows)
	}
	if e, _ := body["err"].(string); !strings.Contains(e, "内核未运行") {
		t.Errorf("err 应说明内核未运行，实际 %q", e)
	}
}

const connsFixture = `{
  "downloadTotal": 5678,
  "uploadTotal": 1234,
  "connections": [
    {
      "id": "conn-1",
      "metadata": {
        "host": "www.google.com",
        "destinationIP": "142.250.1.1",
        "destinationPort": "443",
        "network": "tcp",
        "type": "Redir"
      },
      "upload": 1234,
      "download": 5678,
      "chains": ["🚀 节点选择", "[机场A] 香港 01"],
      "rule": "RuleSet",
      "rulePayload": "cn-ip",
      "start": "2026-10-28T23:00:00Z"
    },
    {
      "id": "conn-2",
      "metadata": {
        "host": "",
        "destinationIP": "1.1.1.1",
        "destinationPort": "53",
        "network": "udp",
        "type": "Redir"
      },
      "upload": 1,
      "download": 2,
      "chains": ["DIRECT"],
      "rule": "Match",
      "rulePayload": "",
      "start": "2026-10-28T23:01:00Z"
    }
  ]
}`

func TestConnectionsFlattensRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, connsFixture)
	}))
	defer srv.Close()

	a := newBackendTestApp(t)
	a.engine.running = true
	a.settings.APIPort = portOfURL(t, srv.URL)
	a.clash = serverClient(t, srv)

	code, body := getJSON(t, a, "/api/connections")
	if code != 200 {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	if n, _ := body["count"].(float64); n != 2 {
		t.Errorf("count 应为 2，实际 %v", body["count"])
	}
	if n, _ := body["up"].(float64); n != 1235 {
		t.Errorf("up 应为行内上传之和 1235，实际 %v", body["up"])
	}
	if n, _ := body["down"].(float64); n != 5680 {
		t.Errorf("down 应为行内下载之和 5680，实际 %v", body["down"])
	}

	type row struct {
		ID          string   `json:"id"`
		Host        string   `json:"host"`
		Dest        string   `json:"dest"`
		Network     string   `json:"network"`
		Type        string   `json:"type"`
		Rule        string   `json:"rule"`
		RulePayload string   `json:"rulePayload"`
		Chains      []string `json:"chains"`
		Upload      int64    `json:"upload"`
		Download    int64    `json:"download"`
		Start       string   `json:"start"`
	}
	raw, err := json.Marshal(body["rows"])
	if err != nil {
		t.Fatalf("重新编码 rows 失败: %v", err)
	}
	var rows []row
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("rows 结构不符合契约: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows 长度应为 2，实际 %d", len(rows))
	}

	r0 := rows[0]
	if r0.ID != "conn-1" || r0.Host != "www.google.com" {
		t.Errorf("第一条连接解析错误: %+v", r0)
	}
	if r0.Dest != "142.250.1.1:443" {
		t.Errorf("dest 应为 destinationIP:port，实际 %q", r0.Dest)
	}
	if r0.Network != "tcp" || r0.Type != "Redir" {
		t.Errorf("network/type 解析错误: %+v", r0)
	}
	if r0.Rule != "RuleSet" || r0.RulePayload != "cn-ip" {
		t.Errorf("rule/rulePayload 解析错误: %+v", r0)
	}
	if len(r0.Chains) != 2 || r0.Chains[0] != "🚀 节点选择" {
		t.Errorf("chains 解析错误: %+v", r0.Chains)
	}
	if r0.Upload != 1234 || r0.Download != 5678 || r0.Start != "2026-10-28T23:00:00Z" {
		t.Errorf("流量/开始时间解析错误: %+v", r0)
	}

	t.Logf("connections 样例：%s", mustJSONLine(t, map[string]any{
		"count": body["count"], "up": body["up"], "down": body["down"], "rows": rows,
	}))

	// host 为空时用 dest 兜底
	if rows[1].Host != "1.1.1.1:53" {
		t.Errorf("host 为空应回退到 dest，实际 %q", rows[1].Host)
	}
	// 旧字段 connections 保留，前端还没迁移完的页面不受影响
	if legacy, ok := body["connections"].([]any); !ok || len(legacy) != 2 {
		t.Errorf("兼容用的 connections 数组异常: %v", body["connections"])
	}
	if _, hasErr := body["err"]; hasErr {
		t.Errorf("读取成功时不应出现 err 字段")
	}
}

// ---------- 2) /api/service ----------

func TestServiceReportsCoreState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = io.WriteString(w, `{"version":"Mihomo Meta v1.19.33"}`)
		case "/configs":
			_, _ = io.WriteString(w, `{"mode":"global","mixed-port":7890,"redir-port":3333}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newBackendTestApp(t)
	a.clash = serverClient(t, srv)

	code, body := getJSON(t, a, "/api/service")
	if code != 200 {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	core, ok := body["core"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 core 对象: %v", body)
	}
	if core["reachable"] != true {
		t.Errorf("内核应可达，实际 %v", core["reachable"])
	}
	if core["version"] != "Mihomo Meta v1.19.33" {
		t.Errorf("version 解析错误: %v", core["version"])
	}
	if _, ok := core["apiLatencyMs"].(float64); !ok {
		t.Errorf("apiLatencyMs 应为数字: %v", core["apiLatencyMs"])
	}
	if body["mode"] != "rule" {
		t.Errorf("mode 应取设置值 rule，实际 %v", body["mode"])
	}
	if body["modeActual"] != "global" {
		t.Errorf("modeActual 应取内核真实值 global，实际 %v", body["modeActual"])
	}
	ipt, _ := body["iptables"].(map[string]any)
	if ipt["applied"] != false {
		t.Errorf("未应用 iptables 时 applied 应为 false，实际 %v", ipt["applied"])
	}
	ports, _ := body["ports"].(map[string]any)
	if int(ports["redir"].(float64)) != a.settings.RedirPort || int(ports["mixed"].(float64)) != a.settings.MixedPort {
		t.Errorf("ports 与设置不一致: %v", ports)
	}
	if _, hasErr := body["err"]; hasErr {
		t.Errorf("内核可达时不应有 err 字段")
	}
	t.Logf("service 样例：%s", mustJSONLine(t, body))
}

func TestServiceCoreDownReturns200(t *testing.T) {
	a := newBackendTestApp(t) // clash 指向没有监听的端口
	code, body := getJSON(t, a, "/api/service")
	if code != 200 {
		t.Fatalf("内核未运行时必须返回 200，实际 %d", code)
	}
	core, _ := body["core"].(map[string]any)
	if core["reachable"] != false {
		t.Errorf("内核不可达时 reachable 应为 false，实际 %v", core["reachable"])
	}
	if core["version"] != "" {
		t.Errorf("不可达时 version 应为空，实际 %v", core["version"])
	}
	if n, _ := core["apiLatencyMs"].(float64); n != 0 {
		t.Errorf("不可达时 apiLatencyMs 应为 0，实际 %v", core["apiLatencyMs"])
	}
	if body["modeActual"] != "" {
		t.Errorf("不可达时 modeActual 应为空，实际 %v", body["modeActual"])
	}
	if e, _ := body["err"].(string); e == "" {
		t.Errorf("不可达时应有 err 说明")
	}
}

// ---------- 3) /api/intranet ----------

func TestIntranetPrefersNvram(t *testing.T) {
	bin := writeFakeNvram(t, "192.168.50.1")
	old := nvramBin
	nvramBin = bin
	defer func() { nvramBin = old }()

	a := newBackendTestApp(t)
	code, body := getJSON(t, a, "/api/intranet")
	if code != 200 {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	if body["lanIp"] != "192.168.50.1" {
		t.Errorf("lanIp 应取自 nvram，实际 %v", body["lanIp"])
	}
	if body["iface"] != "br0" {
		t.Errorf("iface 应取设置值 br0，实际 %v", body["iface"])
	}
	if _, ok := body["ipv4"].([]any); !ok {
		t.Errorf("ipv4 应为数组，实际 %T", body["ipv4"])
	}
	if h, _ := body["hostname"].(string); h == "" {
		t.Logf("提示：本机 hostname 为空")
	}
	if _, hasErr := body["err"]; hasErr {
		t.Errorf("拿到 lanIp 时不应有 err 字段")
	}
	t.Logf("intranet 样例：%s", mustJSONLine(t, body))
}

func TestIntranetFallsBackToIface(t *testing.T) {
	iface, want := ifaceWithIPv4(t)

	old := nvramBin
	nvramBin = filepath.Join(t.TempDir(), "nvram-not-exists")
	defer func() { nvramBin = old }()

	a := newBackendTestApp(t)
	a.settings.LanIf = iface

	code, body := getJSON(t, a, "/api/intranet")
	if code != 200 {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	if body["lanIp"] != want {
		t.Errorf("nvram 不可用时应回退到接口地址 %s，实际 %v", want, body["lanIp"])
	}
	ipv4, _ := body["ipv4"].([]any)
	if len(ipv4) == 0 || ipv4[0] != want {
		t.Errorf("ipv4 应包含接口地址 %s，实际 %v", want, ipv4)
	}
}

func TestIntranetWithoutAnyAddressStill200(t *testing.T) {
	old := nvramBin
	nvramBin = filepath.Join(t.TempDir(), "nvram-not-exists")
	defer func() { nvramBin = old }()

	a := newBackendTestApp(t)
	a.settings.LanIf = "no-such-if0"

	code, body := getJSON(t, a, "/api/intranet")
	if code != 200 {
		t.Fatalf("没有地址时也必须返回 200，实际 %d", code)
	}
	if body["lanIp"] != "" {
		t.Errorf("拿不到地址时 lanIp 应为空，实际 %v", body["lanIp"])
	}
	if e, _ := body["err"].(string); e == "" {
		t.Errorf("拿不到地址时应有 err 说明")
	}
}

// ---------- 4) /api/ipquality ----------

func ipQualityProxy(t *testing.T, hits *int64, isHandler, coHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt64(hits, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.Host, "api.ipapi.is"):
			isHandler(w, r)
		case strings.HasPrefix(r.Host, "ipapi.co"):
			coHandler(w, r)
		case strings.HasPrefix(r.Host, "ip-api.com"):
			http.Error(w, "ip-api disabled in this test", http.StatusBadGateway)
		default:
			http.Error(w, "unexpected host "+r.Host, http.StatusBadGateway)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// useIPQualitySources 把查询源换成走本地代理替身的 http 地址。
func useIPQualitySources(t *testing.T, a *App, proxyURL string) {
	t.Helper()
	a.settings.MixedPort = portOfURL(t, proxyURL)
	oldIS, oldCO := ipQualityURL, ipQualityFallbackURL
	ipQualityURL = "http://api.ipapi.is/?q="
	ipQualityFallbackURL = "http://ipapi.co/json/"
	t.Cleanup(func() {
		ipQualityURL, ipQualityFallbackURL = oldIS, oldCO
	})
}

func TestIPQualityProxiedParseAndCache(t *testing.T) {
	var hits int64
	proxy := ipQualityProxy(t, &hits,
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"ip":"1.2.3.4","is_proxy":false,"is_vpn":false,`+
				`"is_tor":false,"is_datacenter":true,"is_abuser":false,`+
				`"company":{"type":"hosting"},"asn":{"type":"hosting"}}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"ip":"5.6.7.8"}`)
		},
	)

	a := newBackendTestApp(t)
	useIPQualitySources(t, a, proxy.URL)

	code, body := getJSON(t, a, "/api/ipquality")
	if code != 200 {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	q, ok := body["quality"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 quality 对象: %v", body)
	}
	if q["ip"] != "1.2.3.4" {
		t.Errorf("ip 解析错误: %v", q["ip"])
	}
	if q["type"] != "hosting" {
		t.Errorf("type 应取 company.type=hosting，实际 %v", q["type"])
	}
	if q["level"] != "good" {
		t.Errorf("无风险标记时 level 应为 good，实际 %v", q["level"])
	}
	if q["source"] != "ipapi.is" {
		t.Errorf("source 应为 ipapi.is，实际 %v", q["source"])
	}
	if q["err"] != "" {
		t.Errorf("成功时 err 应为空，实际 %v", q["err"])
	}
	checks, _ := q["checks"].(map[string]any)
	for _, k := range []string{"proxy", "vpn", "tor"} {
		if checks[k] != false {
			t.Errorf("checks.%s 应为 false，实际 %v", k, checks[k])
		}
	}
	t.Logf("ipquality 成功样例：%s", mustJSONLine(t, body))
	// 先试 ip-api（被替身拒绝）再走 ipapi.is：共 2 次外呼
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("首次探测应外呼 2 次（ip-api + ipapi.is），实际 %d 次", got)
	}

	// 120 秒缓存期内直接命中缓存
	code2, body2 := getJSON(t, a, "/api/ipquality")
	if code2 != 200 {
		t.Fatalf("缓存响应状态码应为 200，实际 %d", code2)
	}
	if body2["cached"] != true {
		t.Errorf("第二次请求应标注 cached=true")
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("缓存期内不应重新外呼，实际 %d 次", got)
	}

	// refresh=1 强制重新探测
	_, body3 := getJSON(t, a, "/api/ipquality?refresh=1")
	if body3["cached"] == true {
		t.Errorf("强制刷新不应返回 cached")
	}
	if got := atomic.LoadInt64(&hits); got != 4 {
		t.Errorf("强制刷新应重新外呼，实际 %d 次", got)
	}
}

func TestIPQualityFallsBackToSecondarySource(t *testing.T) {
	proxy := ipQualityProxy(t, nil,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"ip":"5.6.7.8","asn":"AS123","org":"Example Ltd"}`)
		},
	)

	a := newBackendTestApp(t)
	useIPQualitySources(t, a, proxy.URL)

	code, body := getJSON(t, a, "/api/ipquality")
	if code != 200 {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	q, _ := body["quality"].(map[string]any)
	if q["ip"] != "5.6.7.8" {
		t.Errorf("备用源 ip 解析错误: %v", q["ip"])
	}
	if q["source"] != "ipapi.co" {
		t.Errorf("source 应标注备用源，实际 %v", q["source"])
	}
	if q["level"] != "unknown" {
		t.Errorf("备用源没有风险标记，level 应为 unknown，实际 %v", q["level"])
	}
	if q["err"] != "" {
		t.Errorf("备用源成功时 err 应为空，实际 %v", q["err"])
	}
}

func TestIPQualityFailureReturns200Unknown(t *testing.T) {
	a := newBackendTestApp(t)
	// 指向没有代理在监听的端口：经代理探测必然失败
	a.settings.MixedPort = freePort(t)
	a.settings.SubInsecure = true

	code, body := getJSON(t, a, "/api/ipquality")
	if code != 200 {
		t.Fatalf("查询失败也必须返回 200，实际 %d", code)
	}
	q, ok := body["quality"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 quality 对象: %v", body)
	}
	if q["level"] != "unknown" {
		t.Errorf("失败时 level 应为 unknown，实际 %v", q["level"])
	}
	if e, _ := q["err"].(string); e == "" {
		t.Errorf("失败时 err 应说明原因")
	}
	if q["source"] != "" {
		t.Errorf("失败时 source 应为空，实际 %v", q["source"])
	}
	checks, _ := q["checks"].(map[string]any)
	for _, k := range []string{"proxy", "vpn", "tor"} {
		if checks[k] != false {
			t.Errorf("失败时 checks.%s 应为零值 false，实际 %v", k, checks[k])
		}
	}
}

// ---------- 5) 定时重启与 url-test 参数 ----------

func TestSettingsValidateRestartAndAutoGroup(t *testing.T) {
	cases := []struct {
		name    string
		patch   map[string]any
		wantErr bool
	}{
		{"固定时刻合法", map[string]any{"restartAt": "04:30"}, false},
		{"清空固定时刻", map[string]any{"restartAt": ""}, false},
		{"缺前导零", map[string]any{"restartAt": "4:30"}, true},
		{"小时越界", map[string]any{"restartAt": "24:00"}, true},
		{"分钟越界", map[string]any{"restartAt": "04:60"}, true},
		{"间隔合法", map[string]any{"restartEveryHours": float64(12)}, false},
		{"间隔为零", map[string]any{"restartEveryHours": float64(0)}, true},
		{"间隔超上限", map[string]any{"restartEveryHours": float64(169)}, true},
		{"测速间隔合法", map[string]any{"autoIntervalSec": float64(600)}, false},
		{"测速间隔过小", map[string]any{"autoIntervalSec": float64(10)}, true},
		{"测速间隔超上限", map[string]any{"autoIntervalSec": float64(7200)}, true},
		{"容差合法", map[string]any{"autoToleranceMs": float64(0)}, false},
		{"容差过大", map[string]any{"autoToleranceMs": float64(5000)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := defaultSettings()
			err := s.applyPatch(tc.patch)
			if tc.wantErr && err == nil {
				t.Fatalf("期望校验失败，实际通过：%v", tc.patch)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望校验通过，实际报错：%v", err)
			}
		})
	}
}

func TestPutSettingsRestartAtReturns400(t *testing.T) {
	a := newBackendTestApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/settings", a.handlePutSettings)

	put := func(payload string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(payload)))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	code, body := put(`{"restartAt":"4:30"}`)
	if code != 400 {
		t.Fatalf("非法 restartAt 应返回 400，实际 %d（%v）", code, body)
	}

	code, body = put(`{"restartAt":"04:30","autoRestart":true,"restartEveryHours":12,` +
		`"autoIntervalSec":600,"autoToleranceMs":120}`)
	if code != 200 {
		t.Fatalf("合法设置应返回 200，实际 %d（%v）", code, body)
	}
	a.mu.RLock()
	s := a.settings
	a.mu.RUnlock()
	if s.RestartAt != "04:30" || !s.AutoRestart || s.RestartEveryHours != 12 {
		t.Errorf("设置没有正确保存: %+v", s)
	}
	if s.AutoIntervalSec != 600 || s.AutoToleranceMs != 120 {
		t.Errorf("自动选择参数没有正确保存: %+v", s)
	}
	// 面板要能读回这些字段
	rec := httptest.NewRecorder()
	a.handleGetSettings(rec, httptest.NewRequest("GET", "/api/settings", nil))
	var pub map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pub); err != nil {
		t.Fatalf("设置接口返回非法 JSON: %v", err)
	}
	st, _ := pub["settings"].(map[string]any)
	for _, k := range []string{
		"autoRestart", "restartEveryHours", "restartAt", "autoIntervalSec", "autoToleranceMs",
		"ruleSourcePreset", "ruleSourceURL", "ruleAutoUpdate", "ruleIntervalH",
		"subUA", "subUACustom", "subInsecure", "subProxyMode",
	} {
		if _, ok := st[k]; !ok {
			t.Errorf("publicSettings 缺少字段 %q", k)
		}
	}
}

func TestNextRestartAt(t *testing.T) {
	a := newBackendTestApp(t)
	now := time.Date(2026, 10, 28, 5, 0, 0, 0, time.Local)

	if got := a.nextRestartAt(now); got != 0 {
		t.Fatalf("未配置定时重启时应为 0，实际 %d", got)
	}

	a.settings.RestartAt = "04:30"
	want := time.Date(2026, 10, 29, 4, 30, 0, 0, time.Local).Unix()
	if got := a.nextRestartAt(now); got != want {
		t.Errorf("今天 04:30 已过，下一次应为明天 04:30（%d），实际 %d", want, got)
	}

	// 周期与固定时刻同时配置时取更早的那个
	a.settings.AutoRestart = true
	a.settings.RestartEveryHours = 2
	a.restartMu.Lock()
	a.lastRestartAt = now.Add(-time.Hour).Unix()
	a.restartMu.Unlock()
	if got, want := a.nextRestartAt(now), now.Add(time.Hour).Unix(); got != want {
		t.Errorf("周期重启一小时后到点，应取更早的那个（%d），实际 %d", want, got)
	}
}

func TestCheckScheduledRestart(t *testing.T) {
	t.Setenv("MM_NO_IPTABLES", "1") // 测试环境不要真的碰防火墙

	a := newBackendTestApp(t)
	a.engine.running = true // 只让 Running() 为真；没有 config 时 Restart 是空操作
	a.settings.AutoRestart = false
	a.settings.RestartAt = "04:30"

	now := time.Date(2026, 10, 28, 5, 0, 0, 0, time.Local)
	a.checkScheduledRestart(now)

	a.restartMu.Lock()
	daily := a.lastDailyDate
	restarted := a.lastRestartAt
	a.restartMu.Unlock()
	if daily != "2026-10-28" {
		t.Errorf("固定时刻到点后应标记当天已执行，实际 %q", daily)
	}
	if restarted == 0 {
		t.Errorf("固定时刻到点后应记录重启时间")
	}

	// 同一天不再重复执行：把上次重启时间清零后再次检查，不应被重新写入
	a.restartMu.Lock()
	a.lastRestartAt = 0
	a.restartMu.Unlock()
	a.checkScheduledRestart(now.Add(2 * time.Hour))
	a.restartMu.Lock()
	again := a.lastRestartAt
	a.restartMu.Unlock()
	if again != 0 {
		t.Errorf("同一天的固定时刻不应重复执行")
	}

	// 跨天后应重新执行
	a.checkScheduledRestart(now.AddDate(0, 0, 1))
	a.restartMu.Lock()
	nextDay := a.lastDailyDate
	a.restartMu.Unlock()
	if nextDay != "2026-10-29" {
		t.Errorf("跨天后应再次执行固定时刻重启，实际 %q", nextDay)
	}

	// 周期重启：未到间隔不触发，超过间隔才执行
	b := newBackendTestApp(t)
	b.engine.running = true
	b.settings.AutoRestart = true
	b.settings.RestartEveryHours = 1

	early := now.Add(-30 * time.Minute).Unix()
	b.restartMu.Lock()
	b.lastRestartAt = early
	b.restartMu.Unlock()
	b.checkScheduledRestart(now)
	b.restartMu.Lock()
	afterEarly := b.lastRestartAt
	b.restartMu.Unlock()
	if afterEarly != early {
		t.Errorf("距上次重启不足 1 小时时不应重启")
	}

	late := now.Add(-2 * time.Hour).Unix()
	b.restartMu.Lock()
	b.lastRestartAt = late
	b.restartMu.Unlock()
	b.checkScheduledRestart(now)
	b.restartMu.Lock()
	afterLate := b.lastRestartAt
	b.restartMu.Unlock()
	if afterLate == late {
		t.Errorf("超过周期后应执行重启")
	}

	// 进度要落盘
	raw, err := os.ReadFile(b.restartStatePath())
	if err != nil {
		t.Fatalf("定时重启进度没有落盘: %v", err)
	}
	var st restartState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("restart.json 不是合法 JSON: %v", err)
	}
	if st.LastRestartAt == 0 {
		t.Errorf("restart.json 里应记录上次重启时间，实际 %+v", st)
	}
}

func TestAutoGroupUsesSettings(t *testing.T) {
	a := newBackendTestApp(t)
	a.settings.AutoIntervalSec = 120
	a.settings.AutoToleranceMs = 200
	a.subs = []Sub{{ID: "s1", Name: "机场A", Enabled: true}}
	if err := os.WriteFile(a.providerPath("s1"), []byte(`{"proxies":[]}`), 0o644); err != nil {
		t.Fatalf("写 provider 替身失败: %v", err)
	}

	cfg, err := a.BuildConfig()
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	var auto map[string]any
	for _, g := range cfg["proxy-groups"].([]any) {
		m, ok := g.(map[string]any)
		if ok && m["name"] == groupAuto {
			auto = m
		}
	}
	if auto == nil {
		t.Fatalf("配置里找不到自动选择组 %s", groupAuto)
	}
	if got, _ := auto["interval"].(int); got != 120 {
		t.Errorf("url-test interval 应读取设置 120，实际 %v", auto["interval"])
	}
	if got, _ := auto["tolerance"].(int); got != 200 {
		t.Errorf("url-test tolerance 应读取设置 200，实际 %v", auto["tolerance"])
	}
}

func TestStatusExposesRestartFields(t *testing.T) {
	a := newBackendTestApp(t)
	a.settings.RestartAt = "04:30"

	rec := httptest.NewRecorder()
	a.handleStatus(rec, httptest.NewRequest("GET", "/api/status", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("/api/status 返回非法 JSON: %v", err)
	}
	eng, ok := body["engine"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 engine 对象: %v", body)
	}
	last, ok := eng["lastRestartAt"].(float64)
	if !ok {
		t.Fatalf("engine.lastRestartAt 应为数字: %v", eng["lastRestartAt"])
	}
	if int64(last) != 0 {
		t.Errorf("没有重启记录时应为 0，实际 %v", last)
	}
	next, ok := eng["nextRestartAt"].(float64)
	if !ok {
		t.Fatalf("engine.nextRestartAt 应为数字: %v", eng["nextRestartAt"])
	}
	if int64(next) <= time.Now().Unix() {
		t.Errorf("配置了固定时刻后 nextRestartAt 应大于当前时间，实际 %v", next)
	}
}
