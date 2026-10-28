package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func gzBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

func TestMaybeGunzip(t *testing.T) {
	payload := []byte("mihomo-fake-binary")
	got, wasGz, err := maybeGunzip(gzBytes(payload))
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !wasGz {
		t.Fatal("应识别为 gzip")
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("解压结果不一致: %q", got)
	}

	raw := []byte("plain")
	got2, wasGz2, err := maybeGunzip(raw)
	if err != nil || wasGz2 || !bytes.Equal(got2, raw) {
		t.Fatalf("非 gzip 内容不应被改动: %v %v", wasGz2, err)
	}
}

func TestInspectELF(t *testing.T) {
	// 文本文件必须被拒绝
	if err := inspectELF([]byte("hello world, definitely not an elf")); err == nil {
		t.Fatal("文本文件应被拒绝")
	}
	// 截断的 ELF 头也必须被拒绝
	if err := inspectELF([]byte{0x7f, 'E', 'L', 'F'}); err == nil {
		t.Fatal("过短的文件应被拒绝")
	}
	// 构造一个 64 位 ELF 头（class=2）→ 拒绝
	elf64 := make([]byte, 64)
	copy(elf64, []byte{0x7f, 'E', 'L', 'F'})
	elf64[4] = 2
	if err := inspectELF(elf64); err == nil {
		t.Fatal("64 位 ELF 应被拒绝")
	}

	// 真实的 armv7 内核（构建缓存里有就用，没有就跳过）
	candidate := filepath.Join(".cache", "mihomo-linux-armv7-1.19.33")
	if b, err := os.ReadFile(candidate); err == nil {
		if err := inspectELF(b); err != nil {
			t.Fatalf("真实 armv7 内核应通过校验，却被拒绝: %v", err)
		}
	} else {
		t.Log("跳过真实内核校验（.cache 里没有 armv7 内核）")
	}
}

func TestExtractProviderError(t *testing.T) {
	logText := `time="x" level=info msg="Start initial provider s_a"
time="x" level=error msg="initial proxy provider s_b error: proxy 2 error: invalid REALITY public key"`
	got := extractProviderError(logText, "s_b")
	if got != "proxy 2 error: invalid REALITY public key" {
		t.Fatalf("错误提取不对: %q", got)
	}
	if extractProviderError(logText, "s_a") != "" {
		t.Fatal("没有错误的 provider 不应返回内容")
	}
}

// 导入接口：模拟用户粘贴一份 Clash 配置
func TestImportSubWritesProvider(t *testing.T) {
	a := newTestApp(t)
	body := `{"name":"导入测试","content":` + strconv.Quote(sampleFullClash) + `}`

	req := httptest.NewRequest("POST", "/api/subs/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handleImportSub(rec, req)

	if rec.Code != 200 {
		t.Fatalf("导入失败: %d %s", rec.Code, rec.Body.String())
	}
	if len(a.subs) != 1 {
		t.Fatalf("订阅数量应为 1，实际 %d", len(a.subs))
	}
	sub := a.subs[0]
	if sub.URL != "" {
		t.Errorf("手动导入不应带源地址: %q", sub.URL)
	}
	if sub.NodeCount != 2 {
		t.Errorf("节点数应为 2，实际 %d", sub.NodeCount)
	}
	if !fileExists(a.providerPath(sub.ID)) {
		t.Fatal("节点文件没有落盘")
	}
	// 空地址的订阅不应参与自动刷新
	a.refreshDueSubs()
	if a.subs[0].Status != "ok" {
		t.Errorf("手动导入的订阅被自动刷新覆盖了: %+v", a.subs[0])
	}
}

// 自签证书的订阅：默认校验会失败，应自动降级（跳过校验）后成功
func TestFetchSubscriptionInsecureFallback(t *testing.T) {
	a := newTestApp(t)
	a.settings.SubInsecure = false // 强制走"先校验、失败再降级"的路径
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Errorf("请求没有带 UA")
		}
		_, _ = w.Write([]byte(sampleFullClash))
	}))
	defer srv.Close()

	sub := Sub{ID: "s_tls", Name: "TLS 机场", URL: srv.URL, Enabled: true, Status: "never"}
	if err := a.fetchSub(&sub); err != nil {
		t.Fatalf("自签证书应能自动降级通过: %v", err)
	}
	if sub.NodeCount != 2 || sub.Format != formatClash {
		t.Fatalf("解析结果不对: %+v", sub)
	}
}

// 拉订阅时应产出可观测的进度事件（start→connect→download→parse→write→done）
func TestProgressEventsOnFetch(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFullClash))
	}))
	defer srv.Close()

	a.subs = []Sub{{ID: "s_p", Name: "进度机场", URL: srv.URL, Enabled: true, Status: "never"}}
	ch, unsub := a.hub.SubscribeProgress()
	defer unsub()

	var steps []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.After(5 * time.Second)
		for {
			select {
			case ev := <-ch:
				steps = append(steps, ev.Step)
				if ev.Done {
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	if err := a.fetchSubAs(&a.subs[0], "更新"); err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	<-done

	need := []string{"start", "connect", "download", "parse", "write", "done"}
	for _, n := range need {
		found := false
		for _, s := range steps {
			if s == n {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("缺少进度事件 %q，实际事件：%v", n, steps)
		}
	}
}

func TestParseSSRAndHysteria(t *testing.T) {
	// ssr://  : base64(host:port:proto:method:obfs:base64(pass)/?remarks=base64(name))
	ssrInner := "1.2.3.4:8388:origin:aes-256-cfb:plain:" + b64Str("mypass") +
		"/?remarks=" + url.QueryEscape(b64Str("SSR 测试"))
	ssrLink := "ssr://" + b64Str(ssrInner)
	p, err := parseURI(ssrLink)
	if err != nil {
		t.Fatalf("ssr 解析失败: %v", err)
	}
	if p["type"] != "ssr" || p["server"] != "1.2.3.4" || p["password"] != "mypass" || p["name"] != "SSR 测试" {
		t.Fatalf("ssr 字段不对: %+v", p)
	}

	hy := "hysteria://5.6.7.8:443?auth=secret&peer=hy.example.com&insecure=1&upmbps=20&downmbps=100#HY1"
	p2, err := parseURI(hy)
	if err != nil {
		t.Fatalf("hysteria 解析失败: %v", err)
	}
	if p2["type"] != "hysteria" || p2["auth-str"] != "secret" || p2["sni"] != "hy.example.com" {
		t.Fatalf("hysteria 字段不对: %+v", p2)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func b64Str(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// 机场常见写法：- { name: '...', type: anytls, ... }（{ 后有空格，且整行行内映射）
func TestClashCountFlowStyleWithSpace(t *testing.T) {
	cfg := `mixed-port: 7890
proxies:
    - { name: '剩余流量：94.79 GB', type: anytls, server: 1.1.1.1, port: 6001, password: p, udp: true }
    - { name: '美国洛杉矶|高速下载', type: anytls, server: 2.2.2.2, port: 6001, password: p }
    - { name: 日本cdn1, type: vless, server: cdn.example.com, port: 443, uuid: u }
proxy-groups:
    - { name: PROXY, type: select, proxies: ['美国洛杉矶|高速下载'] }
rules:
    - MATCH,PROXY
`
	res, err := ParseSubscription([]byte(cfg))
	if err != nil {
		t.Fatalf("应识别为 Clash 配置: %v", err)
	}
	if res.Format != formatClash {
		t.Fatalf("格式应为 clash，实际 %s", res.Format)
	}
	// 三条里有一条是「剩余流量」伪节点，应被剔除
	if res.NodeCount != 2 {
		t.Fatalf("节点数应为 2（剔除伪节点后），实际 %d", res.NodeCount)
	}
	if bytes.Contains(res.ProviderBytes, []byte("剩余流量")) {
		t.Fatalf("伪节点没有被从节点文件里剔除")
	}
}

// 新增订阅：响应里的 nodeCount 必须是解析后的真实数量（曾经因为用了 append 的副本恒为 0）
func TestCreateSubReportsNodeCount(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFullClash))
	}))
	defer srv.Close()

	body := `{"name":"计数测试","url":"` + srv.URL + `"}`
	req := httptest.NewRequest("POST", "/api/subs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handleCreateSub(rec, req)

	if rec.Code != 200 {
		t.Fatalf("创建失败: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Sub struct {
			NodeCount int    `json:"nodeCount"`
			Status    string `json:"status"`
			Error     string `json:"error"`
		} `json:"sub"`
	}
	if err := jsonUnmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp.Sub.NodeCount != 2 {
		t.Fatalf("响应的节点数应为 2，实际 %d（error=%q）", resp.Sub.NodeCount, resp.Sub.Error)
	}
	if resp.Sub.Status == "error" {
		t.Fatalf("不该被标记为异常: %s", resp.Sub.Error)
	}
	if len(a.subs) != 1 || a.subs[0].NodeCount != 2 {
		t.Fatalf("存储的订阅节点数不对: %+v", a.subs)
	}
}
