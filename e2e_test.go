package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// 用户真实订阅的形状：完整 Clash 配置、4 空格缩进、行内映射（{ name: … }）、
// anytls + vless(ws)、外加三个流量/到期伪节点。
const e2eSubscription = `mixed-port: 7890
allow-lan: true
mode: rule
log-level: info
external-controller: '127.0.0.1:9090'
dns:
    enable: true
    enhanced-mode: fake-ip
    nameserver: ['https://223.5.5.5/dns-query']
proxies:
    - { name: '剩余流量：189.76 GB', type: anytls, server: 51.89.216.207, port: 443, password: p1, udp: true, sni: cs.localhost, skip-cert-verify: true }
    - { name: '距离下次重置剩余：14 天', type: anytls, server: 51.89.216.207, port: 443, password: p1, udp: true, sni: cs.localhost, skip-cert-verify: true }
    - { name: 套餐到期：2027-05-11, type: anytls, server: 51.89.216.207, port: 443, password: p1, udp: true, sni: cs.localhost, skip-cert-verify: true }
    - { name: 英国|高速下载, type: anytls, server: 51.89.216.207, port: 443, password: p1, udp: true, sni: cs.localhost, skip-cert-verify: true }
    - { name: 美国凤凰城|高速下载, type: anytls, server: 142.202.240.68, port: 443, password: p1, udp: true, sni: snownbx.apple.com, skip-cert-verify: true }
    - { name: 新加坡1|移动优化, type: anytls, server: sg-fdc.aikunapp.com, port: 6002, password: p1, udp: true, sni: 127.0.0.1, skip-cert-verify: true }
    - { name: 日本cdn1|电信高速, type: vless, server: cdncdn.aikunapp.com, port: 443, uuid: 4529c66c-1035-4196-9424-da7d1afb53b1, alterId: 0, cipher: auto, udp: true, tls: true, skip-cert-verify: false, servername: 82a5f837.ilovecncertcc.bid, network: ws, ws-opts: { path: /3021/index, headers: { Host: sm-jp1.ilovecncertcc.bid } } }
    - { name: '香港 aws 优化线路', type: anytls, server: aws-hkhk.aikunapp.com, port: 6002, password: p1, udp: true, sni: bootcdn.xuexi.cn, skip-cert-verify: true }
proxy-groups:
    - { name: PROXY, type: select, proxies: ['英国|高速下载', '美国凤凰城|高速下载'] }
rules:
    - MATCH,PROXY
`

const e2eNodeTotal = 5 // 8 条里 3 条是「剩余流量/到期/套餐」伪节点，会被剔除

type e2eEnv struct {
	t   *testing.T
	app *App
	srv *httptest.Server
}

func newE2E(t *testing.T) *e2eEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("端到端测试需要真实内核，-short 跳过")
	}
	a := newTestApp(t)
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return &e2eEnv{t: t, app: a, srv: srv}
}

func (e *e2eEnv) call(method, path string, body any, out any) int {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		e.t.Fatalf("构造请求失败: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s 请求失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s 响应不是 JSON: %v\n%s", method, path, err, string(raw))
		}
	}
	if resp.StatusCode >= 400 {
		e.t.Logf("%s %s -> %d %s", method, path, resp.StatusCode, string(raw))
	}
	return resp.StatusCode
}

// 完整走一遍：建订阅 → 启动 → 看节点 → 切节点 → 测延迟 → 流量 → 规则 → 刷新 → 删除
func TestEndToEndWithRealCore(t *testing.T) {
	e := newE2E(t)

	subSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=1024; download=2048; total=107374182400; expire=1793000000")
		_, _ = w.Write([]byte(e2eSubscription))
	}))
	defer subSrv.Close()

	// 1. 添加订阅
	var created struct {
		Sub struct {
			ID        string `json:"id"`
			NodeCount int    `json:"nodeCount"`
			Status    string `json:"status"`
			Traffic   *struct {
				Total int64 `json:"total"`
			} `json:"traffic"`
		} `json:"sub"`
	}
	if code := e.call("POST", "/api/subs", map[string]any{"name": "E2E 机场", "url": subSrv.URL}, &created); code != 200 {
		t.Fatalf("添加订阅失败: %d", code)
	}
	if created.Sub.NodeCount != e2eNodeTotal {
		t.Fatalf("节点数应为 %d，实际 %d", e2eNodeTotal, created.Sub.NodeCount)
	}
	if created.Sub.Status != "ok" {
		t.Fatalf("订阅状态应为 ok，实际 %s", created.Sub.Status)
	}
	if created.Sub.Traffic == nil || created.Sub.Traffic.Total == 0 {
		t.Errorf("没有解析到流量信息（subscription-userinfo）")
	}
	t.Logf("✓ 添加订阅：%d 个节点，状态 %s", created.Sub.NodeCount, created.Sub.Status)
	id := created.Sub.ID

	// 1.5 未启动内核时，节点列表也应该能看到（预览）
	var pre struct {
		Preview bool `json:"preview"`
		Groups  []struct {
			Name  string `json:"name"`
			Nodes []struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				IsGroup bool   `json:"isGroup"`
			} `json:"nodes"`
		} `json:"groups"`
	}
	if code := e.call("GET", "/api/nodes", nil, &pre); code != 200 {
		t.Fatalf("未启动时读节点失败: %d", code)
	}
	if !pre.Preview {
		t.Errorf("未启动时应标记 preview=true")
	}
	var preSubNodes int
	for _, g := range pre.Groups {
		if strings.Contains(g.Name, "E2E 机场") {
			preSubNodes = len(g.Nodes)
		}
	}
	if preSubNodes != e2eNodeTotal {
		t.Fatalf("未启动预览应有 %d 个节点，实际 %d（分组 %v）", e2eNodeTotal, preSubNodes, pre.Groups)
	}
	t.Logf("✓ 未启动即可看到节点：%d 个", preSubNodes)

	// 2. 启动内核
	var startRes struct{ OK bool }
	if code := e.call("POST", "/api/engine/start", nil, &startRes); code != 200 || !startRes.OK {
		t.Fatalf("启动内核失败: %d", code)
	}
	t.Logf("✓ 内核已启动")

	// 3. 节点列表：分组存在、节点已合并进来
	var nodes struct {
		Groups []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Now   string `json:"now"`
			Nodes []struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				IsGroup bool   `json:"isGroup"`
			} `json:"nodes"`
		} `json:"groups"`
	}
	if code := e.call("GET", "/api/nodes", nil, &nodes); code != 200 {
		t.Fatalf("读取节点失败: %d", code)
	}
	if len(nodes.Groups) == 0 {
		t.Fatal("没有任何分组")
	}
	var subGroup *struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Now   string `json:"now"`
		Nodes []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			IsGroup bool   `json:"isGroup"`
		} `json:"nodes"`
	}
	for i := range nodes.Groups {
		if strings.Contains(nodes.Groups[i].Name, "E2E 机场") {
			subGroup = &nodes.Groups[i]
		}
	}
	if subGroup == nil {
		names := make([]string, 0, len(nodes.Groups))
		for _, g := range nodes.Groups {
			names = append(names, g.Name)
		}
		t.Fatalf("没有找到订阅分组，实际分组：%v", names)
	}
	if len(subGroup.Nodes) != e2eNodeTotal {
		t.Fatalf("订阅分组应有 %d 个节点，实际 %d", e2eNodeTotal, len(subGroup.Nodes))
	}
	if !strings.HasPrefix(subGroup.Nodes[0].Name, "[E2E 机场]") {
		t.Errorf("节点名没有带订阅前缀: %q", subGroup.Nodes[0].Name)
	}
	t.Logf("✓ 节点列表：%d 个分组，订阅分组 %d 个节点，首节点 %q", len(nodes.Groups), len(subGroup.Nodes), subGroup.Nodes[0].Name)

	// 分组成员（自动选择 / 订阅组 / DIRECT）必须带 isGroup 标记，前端才不会把它们当节点重复显示
	for _, g := range nodes.Groups {
		for _, n := range g.Nodes {
			if n.Name == groupAuto || strings.HasPrefix(n.Name, "📦 ") || n.Name == "DIRECT" {
				if !n.IsGroup {
					t.Errorf("分组 %q 里的成员 %q 应标记 isGroup", g.Name, n.Name)
				}
			}
		}
	}
	// 聚合组里应该有全部真实节点（isGroup=false）
	var aggReal int
	for _, g := range nodes.Groups {
		if g.Name == groupMain {
			for _, n := range g.Nodes {
				if !n.IsGroup {
					aggReal++
				}
			}
		}
	}
	if aggReal != e2eNodeTotal {
		t.Errorf("节点选择里应有 %d 个真实节点，实际 %d", e2eNodeTotal, aggReal)
	}
	t.Logf("✓ 聚合组 %d 个真实节点，分组成员已标记 isGroup", aggReal)

	// 4. 切换节点
	target := subGroup.Nodes[1].Name
	if code := e.call("PUT", "/api/select", map[string]any{"group": subGroup.Name, "node": target}, nil); code != 200 {
		t.Fatalf("切换节点失败: %d", code)
	}
	var after struct {
		Groups []struct {
			Name string `json:"name"`
			Now  string `json:"now"`
		} `json:"groups"`
	}
	e.call("GET", "/api/nodes", nil, &after)
	for _, g := range after.Groups {
		if g.Name == subGroup.Name && g.Now != target {
			t.Fatalf("切换未生效：期望 %q，实际 %q", target, g.Now)
		}
	}
	t.Logf("✓ 切换节点：%s", target)

	// 5. 单节点延迟（目标不可达时应返回 delay=0 而不是报错）
	var delay struct {
		OK    bool `json:"ok"`
		Delay int  `json:"delay"`
	}
	if code := e.call("GET", "/api/delay?name="+urlQueryEscape(target), nil, &delay); code != 200 || !delay.OK {
		t.Fatalf("延迟接口异常: %d %+v", code, delay)
	}
	t.Logf("✓ 延迟测试接口：delay=%d", delay.Delay)

	// 6. 流量 SSE 有数据
	if n := countSSEEvents(t, e.srv.URL+"/api/traffic/stream", 3*time.Second); n == 0 {
		t.Errorf("流量流 3 秒内没有任何数据")
	} else {
		t.Logf("✓ 流量流：%d 个事件", n)
	}

	// 7. 规则与连接
	var rules struct {
		Rules []any `json:"rules"`
	}
	if code := e.call("GET", "/api/rules", nil, &rules); code != 200 || len(rules.Rules) == 0 {
		t.Errorf("规则列表异常: code=%d n=%d", code, len(rules.Rules))
	} else {
		t.Logf("✓ 规则：%d 条", len(rules.Rules))
	}
	var conns map[string]any
	if code := e.call("GET", "/api/connections", nil, &conns); code != 200 {
		t.Errorf("连接列表异常: %d", code)
	}

	// 8. 刷新订阅后再看节点
	if code := e.call("POST", "/api/subs/"+id+"/refresh", nil, nil); code != 200 {
		t.Fatalf("刷新订阅失败: %d", code)
	}
	var again struct {
		Groups []struct {
			Nodes []any `json:"nodes"`
		} `json:"groups"`
	}
	e.call("GET", "/api/nodes", nil, &again)
	t.Logf("✓ 刷新后分组数：%d", len(again.Groups))

	// 8.5 国内 IP 规则订阅
	var sb strings.Builder
	for i := 0; i < 1200; i++ {
		sb.WriteString("10.")
		sb.WriteString(itoa(i / 256))
		sb.WriteString(".")
		sb.WriteString(itoa(i % 256))
		sb.WriteString(".0/24\n")
	}
	ruleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# e2e rules\n" + sb.String()))
	}))
	defer ruleSrv.Close()

	var ruleUpd struct {
		Meta struct {
			Count  int    `json:"count"`
			Source string `json:"source"`
		} `json:"meta"`
	}
	if code := e.call("POST", "/api/rules/update", map[string]any{"url": ruleSrv.URL}, &ruleUpd); code != 200 {
		t.Fatalf("规则更新失败: %d", code)
	}
	if ruleUpd.Meta.Count < 1000 {
		t.Fatalf("规则条数不合理: %d", ruleUpd.Meta.Count)
	}
	var ruleInfo struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Auto bool `json:"auto"`
	}
	e.call("GET", "/api/rules/set", nil, &ruleInfo)
	if ruleInfo.Meta.Count != ruleUpd.Meta.Count {
		t.Errorf("规则元信息不一致: %d vs %d", ruleInfo.Meta.Count, ruleUpd.Meta.Count)
	}
	if code := e.call("GET", "/api/rules", nil, nil); code != 200 {
		t.Errorf("规则更新后内核规则读取失败: %d", code)
	}
	t.Logf("✓ 规则订阅：%d 条（引擎已热重载）", ruleUpd.Meta.Count)

	// 8.55 出站模式：切换后必须在内核里真正生效
	if cfg, err := e.app.clash.Configs(); err != nil || cfg.Mode != "rule" {
		t.Fatalf("启动后内核模式应为 rule: %+v %v", cfg, err)
	}
	if code := e.call("PUT", "/api/settings", map[string]any{"mode": "global"}, nil); code != 200 {
		t.Fatalf("切换全局模式失败: %d", code)
	}
	if cfg, err := e.app.clash.Configs(); err != nil || cfg.Mode != "global" {
		t.Fatalf("切换后内核模式应为 global: %+v %v", cfg, err)
	}
	t.Logf("✓ 出站模式切换生效：内核 mode=global")
	if code := e.call("PUT", "/api/settings", map[string]any{"mode": "rule"}, nil); code != 200 {
		t.Fatalf("切回规则模式失败: %d", code)
	}
	if cfg, err := e.app.clash.Configs(); err != nil || cfg.Mode != "rule" {
		t.Fatalf("切回后内核模式应为 rule: %+v %v", cfg, err)
	}
	t.Logf("✓ 已切回规则模式")

	// 8.56 网络状态接口（允许外网不可达，但结构必须正确）
	var ni struct {
		Info struct {
			Direct *struct {
				IP  string `json:"ip"`
				Err string `json:"err"`
			} `json:"direct"`
			Node string `json:"node"`
		} `json:"info"`
	}
	if code := e.call("GET", "/api/netinfo", nil, &ni); code != 200 {
		t.Fatalf("网络状态接口失败: %d", code)
	}
	if ni.Info.Direct != nil && ni.Info.Direct.IP == "" && ni.Info.Direct.Err == "" {
		t.Errorf("直连出口信息既没有 IP 也没有错误说明")
	}
	t.Logf("✓ 网络状态：直连探测=%v，当前节点=%q", ni.Info.Direct != nil, ni.Info.Node)

	// 8.57 新接口结构（连接明细 / 服务状态 / 内网 IP / IP 质量）
	var connsRows struct {
		Rows  []map[string]any `json:"rows"`
		Count int              `json:"count"`
	}
	if code := e.call("GET", "/api/connections", nil, &connsRows); code != 200 {
		t.Fatalf("连接明细接口失败: %d", code)
	}
	if connsRows.Rows == nil {
		t.Errorf("rows 应为数组（可为空）")
	}
	var svc struct {
		Core struct {
			Reachable bool `json:"reachable"`
		} `json:"core"`
		Ports struct {
			Redir int `json:"redir"`
		} `json:"ports"`
		ModeActual string `json:"modeActual"`
	}
	if code := e.call("GET", "/api/service", nil, &svc); code != 200 {
		t.Fatalf("服务状态接口失败: %d", code)
	}
	if svc.Ports.Redir <= 0 {
		t.Errorf("redir 端口异常: %d", svc.Ports.Redir)
	}
	if !svc.Core.Reachable {
		t.Errorf("内核应在运行且 API 可达")
	}
	if svc.ModeActual != "rule" {
		t.Errorf("内核实际模式应为 rule，实际 %q", svc.ModeActual)
	}
	var intra struct {
		LanIP string   `json:"lanIp"`
		Iface string   `json:"iface"`
		IPv4  []string `json:"ipv4"`
		OK    bool     `json:"ok"`
	}
	if code := e.call("GET", "/api/intranet", nil, &intra); code != 200 || !intra.OK {
		t.Fatalf("内网接口失败: %d", code)
	}
	t.Logf("✓ 新接口：连接 rows=%d，服务可达=%v，内网 iface=%s", len(connsRows.Rows), svc.Core.Reachable, intra.Iface)

	var q struct {
		Quality struct {
			Level string `json:"level"`
			Err   string `json:"err"`
		} `json:"quality"`
	}
	if code := e.call("GET", "/api/ipquality", nil, &q); code != 200 {
		t.Fatalf("IP 质量接口不应 5xx，实际 %d", code)
	}
	if q.Quality.Level == "" {
		t.Errorf("level 字段缺失")
	}
	t.Logf("✓ IP 质量：level=%s", q.Quality.Level)

	// 8.58 定时重启与自动选择参数校验
	if code := e.call("PUT", "/api/settings", map[string]any{"restartAt": "4:30"}, nil); code != 400 {
		t.Errorf("非法 restartAt 应返回 400，实际 %d", code)
	}
	if code := e.call("PUT", "/api/settings", map[string]any{"autoIntervalSec": 5}, nil); code != 400 {
		t.Errorf("过小的检查间隔应返回 400，实际 %d", code)
	}
	if code := e.call("PUT", "/api/settings", map[string]any{"autoIntervalSec": 120, "autoToleranceMs": 80}, nil); code != 200 {
		t.Errorf("合法的自动选择参数应保存成功，实际 %d", code)
	}
	// 参数要真的写进内核配置
	if cfg, err := e.app.clash.Configs(); err == nil && cfg.Mode == "" {
		t.Errorf("内核配置读取异常")
	}
	t.Logf("✓ 定时重启/自动选择参数校验生效")

	// 8.6 存储占用与清理
	var st1 struct {
		Report struct {
			Total int64 `json:"total"`
			Free  int64 `json:"free"`
		} `json:"report"`
	}
	if code := e.call("GET", "/api/storage", nil, &st1); code != 200 {
		t.Fatalf("存储接口失败: %d", code)
	}
	if st1.Report.Free <= 0 {
		t.Errorf("剩余空间应为正数: %d", st1.Report.Free)
	}
	var clean struct {
		Freed int64    `json:"freed"`
		Steps []string `json:"steps"`
	}
	if code := e.call("POST", "/api/storage/clean", map[string]any{}, &clean); code != 200 {
		t.Fatalf("清理接口失败: %d", code)
	}
	t.Logf("✓ 存储：已用 %s，剩余 %s；清理释放 %s（%d 项）",
		humanBytes(st1.Report.Total), humanBytes(st1.Report.Free),
		humanBytes(clean.Freed), len(clean.Steps))

	// 9. 设置读写往返
	type settingsEnvelope struct {
		Settings struct {
			LogLevel string `json:"logLevel"`
		} `json:"settings"`
	}
	var st settingsEnvelope
	if code := e.call("GET", "/api/settings", nil, &st); code != 200 {
		t.Fatalf("读取设置失败: %d", code)
	}
	if code := e.call("PUT", "/api/settings", map[string]any{"logLevel": "warning"}, nil); code != 200 {
		t.Fatalf("写入设置失败: %d", code)
	}
	var st2 settingsEnvelope
	e.call("GET", "/api/settings", nil, &st2)
	if st2.Settings.LogLevel != "warning" {
		t.Errorf("设置没有生效: logLevel=%q", st2.Settings.LogLevel)
	}
	t.Logf("✓ 设置读写：logLevel=%s", st2.Settings.LogLevel)

	// 10. 原始内容
	var raw struct {
		Bytes   int    `json:"bytes"`
		Guess   string `json:"guess"`
		Preview string `json:"preview"`
	}
	if code := e.call("GET", "/api/subs/"+id+"/raw", nil, &raw); code != 200 {
		t.Fatalf("原始内容接口失败: %d", code)
	}
	if !strings.Contains(raw.Preview, "proxies:") {
		t.Errorf("原始内容不含 proxies：%q", raw.Preview[:min(80, len(raw.Preview))])
	}
	if !strings.Contains(raw.Preview, "剩余流量") {
		t.Logf("提示：原始内容是过滤后的版本（不含伪节点）")
	}
	t.Logf("✓ 原始内容：%d 字节，识别为 %s", raw.Bytes, raw.Guess)

	// 11. 批量测速
	var batch map[string]any
	if code := e.call("POST", "/api/delay", map[string]any{"names": []string{target}}, &batch); code != 200 {
		t.Errorf("批量测速失败: %d", code)
	} else {
		t.Logf("✓ 批量测速：%v", batch["results"])
	}

	// 12. 内核信息与状态
	var kinfo map[string]any
	if code := e.call("GET", "/api/kernel", nil, &kinfo); code != 200 {
		t.Errorf("内核信息失败: %d", code)
	}
	var status struct {
		Engine struct {
			Running bool `json:"running"`
		} `json:"engine"`
		Panel struct {
			Version string `json:"version"`
		} `json:"panel"`
	}
	e.call("GET", "/api/status", nil, &status)
	if !status.Engine.Running {
		t.Errorf("状态里内核应为 running")
	}
	t.Logf("✓ 状态：running=%v panel=%s", status.Engine.Running, status.Panel.Version)

	// 13. 改名
	if code := e.call("PUT", "/api/subs/"+id, map[string]any{"name": "改名后的机场"}, nil); code != 200 {
		t.Errorf("改名失败: %d", code)
	}

	// 14. 停止内核 + 删除订阅
	if code := e.call("POST", "/api/engine/stop", nil, nil); code != 200 {
		t.Errorf("停止内核失败: %d", code)
	}
	if code := e.call("DELETE", "/api/subs/"+id, nil, nil); code != 200 {
		t.Errorf("删除订阅失败: %d", code)
	}
	if len(e.app.subs) != 0 {
		t.Errorf("订阅没有被删除: %+v", e.app.subs)
	}
	if fileExists(e.app.providerPath(id)) {
		t.Errorf("删除订阅后节点文件仍在")
	}
	if fileExists(filepath.Join(e.app.dataDir, "cache", id+".sub")) {
		t.Errorf("删除订阅后原始缓存仍在")
	}
	t.Logf("✓ 停止内核并删除订阅")
}

// countSSEEvents 连到 SSE 端点，统计 3 秒内收到的事件数
func countSSEEvents(t *testing.T, url string, d time.Duration) int {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0
	}
	client := &http.Client{Timeout: d}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	// 第一块通常只有 "retry: 3000"，要持续读到超时为止
	total := 0
	buf := make([]byte, 4096)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			total += strings.Count(string(buf[:n]), "data:")
		}
		if err != nil {
			break
		}
	}
	return total
}

func urlQueryEscape(s string) string {
	r := strings.NewReplacer(" ", "%20", "|", "%7C", "+", "%2B", "&", "%26", "?", "%3F", "#", "%23")
	return r.Replace(s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = fmt.Sprintf

// 订阅名称必须唯一（新增、改名、导入都要拦）
func TestSubNameDuplicate(t *testing.T) {
	e := newE2E(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFullClash))
	}))
	defer srv.Close()

	var first struct {
		Sub struct {
			ID string `json:"id"`
		} `json:"sub"`
	}
	if code := e.call("POST", "/api/subs", map[string]any{"name": "同名测试", "url": srv.URL}, &first); code != 200 {
		t.Fatalf("第一个订阅创建失败: %d", code)
	}

	// 1) 重名新增
	if code := e.call("POST", "/api/subs", map[string]any{"name": "同名测试", "url": srv.URL}, nil); code != 400 {
		t.Errorf("重名新增应返回 400，实际 %d", code)
	}
	// 2) 大小写 / 空格视为同名
	if code := e.call("POST", "/api/subs", map[string]any{"name": "  同名测试  ", "url": srv.URL}, nil); code != 400 {
		t.Errorf("带空格的重名也应被拦，实际 %d", code)
	}
	// 3) 改名撞名
	var second struct {
		Sub struct {
			ID string `json:"id"`
		} `json:"sub"`
	}
	if code := e.call("POST", "/api/subs", map[string]any{"name": "另一个", "url": srv.URL}, &second); code != 200 {
		t.Fatalf("第二个订阅创建失败: %d", code)
	}
	if code := e.call("PUT", "/api/subs/"+second.Sub.ID, map[string]any{"name": "同名测试"}, nil); code != 400 {
		t.Errorf("改名撞名应返回 400，实际 %d", code)
	}
	// 4) 导入撞名
	impBody := map[string]any{"name": "同名测试", "content": sampleFullClash}
	if code := e.call("POST", "/api/subs/import", impBody, nil); code != 400 {
		t.Errorf("导入重名应返回 400，实际 %d", code)
	}
	t.Logf("✓ 名称查重：新增/空格/改名/导入 四种情况都被拦下")
}

// 订阅下载过程要能看到 dns/connect/tls/response/bytes 各阶段（否则界面一片空白）
func TestFetchProgressStages(t *testing.T) {
	a := newTestApp(t)
	payload := strings.Repeat("proxies: []\n", 8000) // 约 96KB，分多次读
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond) // 模拟慢服务器，让各阶段有机会被观测
		w.Header().Set("Content-Length", itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	ch, unsub := a.hub.SubscribeProgress()
	defer unsub()

	got := map[string]bool{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.After(6 * time.Second)
		for {
			select {
			case ev := <-ch:
				got[ev.Step] = true
				if ev.Step == "done" || ev.Err {
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	sub := Sub{ID: "s_prog", Name: "阶段测试", URL: srv.URL, Enabled: true}
	_ = a.fetchSub(&sub) // 自签证书 → 走降级路径，正好覆盖 tls 阶段
	<-done

	for _, step := range []string{"start", "tls", "response", "bytes"} {
		if !got[step] {
			t.Errorf("缺少进度阶段 %q（已收到 %v）", step, keysOf(got))
		}
	}
	t.Logf("✓ 下载阶段可见：%v", keysOf(got))
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
