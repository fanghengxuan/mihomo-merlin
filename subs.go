package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Traffic 订阅流量信息（来自 subscription-userinfo 响应头）。
type Traffic struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}

// Sub 一条订阅。
type Sub struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Interval  int      `json:"interval"`
	Enabled   bool     `json:"enabled"`
	NodeCount int      `json:"nodeCount"`
	Format    string   `json:"format"`
	UpdatedAt int64    `json:"updatedAt"`
	Status    string   `json:"status"` // ok | error | never
	Error     string   `json:"error"`
	Traffic   *Traffic `json:"traffic"`
	Skipped   int      `json:"skipped"`
	// 节点过滤（对应 mihomo proxy-provider 的 filter / exclude-filter，正则）
	IncludeFilter string `json:"includeFilter"`
	ExcludeFilter string `json:"excludeFilter"`
}

const userAgent = "clash.meta/1.19.33"

func (a *App) loadSubs() {
	var subs []Sub
	_ = readJSON(filepath.Join(a.dataDir, "subs.json"), &subs)
	for i := range subs {
		if subs[i].Status == "" {
			subs[i].Status = "never"
		}
	}
	a.mu.Lock()
	a.subs = subs
	a.mu.Unlock()
}

func (a *App) saveSubsLocked() error {
	return writeJSONAtomic(filepath.Join(a.dataDir, "subs.json"), a.subs)
}

func (a *App) providerPath(id string) string {
	return filepath.Join(a.dataDir, "providers", id+".json")
}

func (a *App) findSub(id string) (*Sub, int) {
	for i := range a.subs {
		if a.subs[i].ID == id {
			return &a.subs[i], i
		}
	}
	return nil, -1
}

// fetchSub 拉取并解析订阅；成功时把 provider 文件写入磁盘。
func (a *App) fetchSub(s *Sub) error {
	return a.fetchSubAs(s, "更新")
}

// fetchSubAs 拉取订阅；op 用于进度事件分类（新增/更新）。
func (a *App) fetchSubAs(s *Sub, op string) error {
	if strings.TrimSpace(s.URL) == "" {
		return errors.New("订阅地址为空")
	}
	a.prog("fetch", s.Name, "start", "开始拉取订阅："+maskURL(s.URL, false), false, false)

	resp, body, err := a.fetchSubscription(s.URL, s.Name)
	if err != nil {
		a.prog("fetch", s.Name, "error", err.Error(), true, true)
		return err
	}
	defer resp.Body.Close()
	a.prog("fetch", s.Name, "download", fmt.Sprintf("已下载 %s 字节，开始识别格式", humanBytes(int64(len(body)))), false, false)
	a.saveSubRaw(s.ID, body)

	res, err := ParseSubscription(body)
	if err != nil {
		a.prog("fetch", s.Name, "error", "解析失败："+err.Error(), true, true)
		return err
	}
	a.prog("fetch", s.Name, "parse", fmt.Sprintf("识别为 %s，解析出 %d 个节点", res.Format, res.NodeCount), false, false)

	if err := os.MkdirAll(filepath.Dir(a.providerPath(s.ID)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(a.providerPath(s.ID), res.ProviderBytes, 0o600); err != nil {
		a.prog("fetch", s.Name, "error", "写入节点文件失败："+err.Error(), true, true)
		return fmt.Errorf("写入节点文件失败: %v", err)
	}
	a.prog("fetch", s.Name, "write", "节点文件已写入", false, false)

	s.Format = res.Format
	s.NodeCount = res.NodeCount
	s.Skipped = res.Skipped
	s.UpdatedAt = time.Now().Unix()
	s.Status = "ok"
	s.Error = ""
	if len(res.Errs) > 0 {
		s.Error = "部分节点不支持：" + strings.Join(res.Errs, "；")
	}
	if t := parseUserInfo(resp.Header.Get("subscription-userinfo")); t != nil {
		s.Traffic = t
	}
	a.prog("fetch", s.Name, "done", fmt.Sprintf("完成：%d 个节点", res.NodeCount), true, false)
	return nil
}

func parseUserInfo(h string) *Traffic {
	if strings.TrimSpace(h) == "" {
		return nil
	}
	t := &Traffic{}
	found := false
	for _, part := range strings.Split(h, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			t.Upload = v
			found = true
		case "download":
			t.Download = v
			found = true
		case "total":
			t.Total = v
			found = true
		case "expire":
			t.Expire = v
			found = true
		}
	}
	if !found {
		return nil
	}
	return t
}

// refreshSubByID 拉取单条订阅；调用方需自行串行化。
func (a *App) refreshSubByID(id string) error {
	sub, _ := a.findSub(id)
	if sub == nil {
		return errors.New("订阅不存在")
	}
	if err := a.fetchSub(sub); err != nil {
		sub.Status = "error"
		sub.Error = err.Error()
		_ = a.saveSubsLocked()
		return err
	}
	return a.saveSubsLocked()
}

// maskURL 向非作者视图打码 token 参数。
func maskURL(raw string, reveal bool) string {
	if reveal || raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// 逐段重建 query，避免 url.Values.Encode() 把 *** 编成 %2A%2A%2A
	parts := strings.Split(u.RawQuery, "&")
	changed := false
	for i, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 0 {
			continue
		}
		lk := strings.ToLower(kv[0])
		if strings.Contains(lk, "token") || strings.Contains(lk, "key") ||
			strings.Contains(lk, "pass") || strings.Contains(lk, "auth") || lk == "sub" {
			parts[i] = kv[0] + "=***"
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = strings.Join(parts, "&")
	return u.String()
}

// refreshDueSubs 按间隔更新过期订阅（面板后台任务）。
func (a *App) refreshDueSubs() {
	a.mu.RLock()
	interval := a.settings.SubInterval
	subs := make([]Sub, len(a.subs))
	copy(subs, a.subs)
	a.mu.RUnlock()

	now := time.Now().Unix()
	for _, s := range subs {
		if !s.Enabled {
			continue
		}
		if strings.TrimSpace(s.URL) == "" {
			continue // 手动导入的配置没有源地址，跳过自动更新
		}
		if !subUpdateDue(s, interval, now) {
			continue
		}
		a.subMu.Lock()
		err := func() error {
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.refreshSubByID(s.ID)
		}()
		a.subMu.Unlock()
		if err != nil {
			a.addLog("warning", fmt.Sprintf("订阅 %s 自动更新失败：%v", s.Name, err))
			continue
		}
		a.addLog("info", fmt.Sprintf("订阅 %s 已自动更新", s.Name))
		if a.engine.Running() {
			_ = a.engine.Reload()
		}
	}
}

// uaString 按设置返回订阅请求的 UA（对齐 fancyss 的 UA 预设与 MCLite 的固定 clash.meta）
func (a *App) uaString() string {
	switch a.settings.SubUA {
	case "curl":
		return "curl/8.5.0"
	case "v2rayn":
		return "v2rayN/6.45"
	case "v2rayng":
		return "v2rayNG/1.9.16"
	case "shadowrocket":
		return "Shadowrocket/2.2.9"
	case "custom":
		if strings.TrimSpace(a.settings.SubUACustom) != "" {
			return strings.TrimSpace(a.settings.SubUACustom)
		}
	}
	return userAgent // 默认 clash.meta/x
}

func (a *App) newHTTPClient(viaProxy, insecure bool) *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 只走 IPv4：路由器常常没有可用 IPv6，走 AAAA 会一直卡到超时
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout:   12 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if viaProxy {
		proxyURL, _ := url.Parse("http://127.0.0.1:" + itoa(a.settings.MixedPort))
		tr.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: tr, Timeout: 45 * time.Second}
}

// doFetchOnce 单次请求；insecure 控制是否跳过证书校验。
func (a *App) doFetchOnce(rawURL string, viaProxy, insecure bool, tag string) (*http.Response, []byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("订阅地址非法: %v", err)
	}
	req.Header.Set("User-Agent", a.uaString())
	req.Header.Set("Accept", "*/*")

	// 把 DNS / 连接 / TLS / 首字节这些阶段都报出来，否则只看到「下载中」一片空白
	start := time.Now()
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			a.prog("fetch", tag, "dns", "正在解析域名…", false, false)
		},
		DNSDone: func(i httptrace.DNSDoneInfo) {
			if i.Err != nil {
				a.prog("fetch", tag, "dns", "域名解析失败："+i.Err.Error(), false, true)
				return
			}
			addrs := make([]string, 0, len(i.Addrs))
			for _, ad := range i.Addrs {
				addrs = append(addrs, ad.IP.String())
			}
			a.prog("fetch", tag, "dns", "域名解析完成："+strings.Join(addrs, ", "), false, false)
		},
		ConnectStart: func(network, addr string) {
			a.prog("fetch", tag, "connect", "正在建立 TCP 连接 → "+addr, false, false)
		},
		ConnectDone: func(network, addr string, err error) {
			if err != nil {
				a.prog("fetch", tag, "connect", "TCP 连接失败："+err.Error(), false, true)
			}
		},
		TLSHandshakeStart: func() {
			a.prog("fetch", tag, "tls", "开始 TLS 握手…", false, false)
		},
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			if err != nil {
				a.prog("fetch", tag, "tls", "TLS 握手失败："+err.Error(), false, true)
				return
			}
			a.prog("fetch", tag, "tls", fmt.Sprintf("TLS 握手完成（%s，%s）",
				tlsVersionName(cs.Version), tls.CipherSuiteName(cs.CipherSuite)), false, false)
		},
		GotFirstResponseByte: func() {
			a.prog("fetch", tag, "response", fmt.Sprintf("已收到服务器响应（耗时 %.1fs）", time.Since(start).Seconds()), false, false)
		},
	}

	// 等待期间每 5 秒报一次还在等什么，避免界面像死了一样
	doneCh := make(chan struct{})
	if tag != "" {
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-doneCh:
					return
				case <-t.C:
					a.prog("fetch", tag, "wait", fmt.Sprintf("仍在等待响应…（已 %.0fs）", time.Since(start).Seconds()), false, false)
				}
			}
		}()
	}
	defer close(doneCh)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))

	resp, err := a.newHTTPClient(viaProxy, insecure).Do(req)
	if err != nil {
		return nil, nil, describeNetErr(err, rawURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp, nil, fmt.Errorf("订阅服务器返回 HTTP %d", resp.StatusCode)
	}
	if cl := resp.ContentLength; cl > 0 && tag != "" {
		a.prog("fetch", tag, "response", fmt.Sprintf("响应头已收到：HTTP %d，%s", resp.StatusCode, humanBytes(cl)), false, false)
	}

	body, err := readBodyWithProgress(resp.Body, 8<<20, resp.ContentLength, func(read int64, total int64) {
		if tag == "" {
			return
		}
		if total > 0 {
			a.prog("fetch", tag, "bytes", fmt.Sprintf("已下载 %s / %s（%d%%）",
				humanBytes(read), humanBytes(total), int(read*100/total)), false, false)
		} else {
			a.prog("fetch", tag, "bytes", "已下载 "+humanBytes(read), false, false)
		}
	})
	if err != nil {
		return resp, nil, fmt.Errorf("读取订阅失败: %v", err)
	}
	if len(body) == 0 {
		return resp, nil, fmt.Errorf("订阅内容为空（有的机场对 UA 敏感，可在设置里换 UA 预设）")
	}
	return resp, body, nil
}

// doFetch 带重试：最多 3 次，短超时（fancyss 用 --connect-timeout 5 --retry 3）。
func (a *App) doFetch(rawURL string, viaProxy bool, tag string) (*http.Response, []byte, error) {
	var lastErr error
	insecure := a.settings.SubInsecure
	for i := 0; i < 3; i++ {
		resp, body, err := a.doFetchOnce(rawURL, viaProxy, insecure, tag)
		if err == nil {
			return resp, body, nil
		}
		lastErr = err
		if i < 2 && tag != "" {
			a.prog("fetch", tag, "retry", fmt.Sprintf("第 %d 次失败（%v），准备重试…", i+1, err), false, false)
		}
		// 证书问题且未开跳过校验时，自动降级重试（路由器时间不准很常见）
		if !insecure && isCertErr(err) {
			insecure = true
			continue
		}
		time.Sleep(time.Duration(i+1) * 600 * time.Millisecond)
	}
	return nil, nil, lastErr
}

func isCertErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "证书") || strings.Contains(msg, "certificate") || strings.Contains(msg, "x509")
}

// fetchSubscription 拉取订阅：按设置决定直连/代理，失败自动换另一种再试一次。
func (a *App) fetchSubscription(rawURL, subName string) (*http.Response, []byte, error) {
	mode := a.settings.SubProxyMode
	var attempts []bool
	switch mode {
	case "direct":
		attempts = []bool{false}
	case "proxy":
		attempts = []bool{true}
	default:
		attempts = []bool{false}
		if a.engine.Running() {
			attempts = append(attempts, true)
		}
	}

	var errs []string
	for _, viaProxy := range attempts {
		if viaProxy {
			a.prog("fetch", subName, "proxy", "直连失败，改用本机代理重试…", false, false)
		} else {
			a.prog("fetch", subName, "connect", "通过本地网络直连下载…", false, false)
		}
		resp, body, err := a.doFetch(rawURL, viaProxy, subName)
		if err == nil {
			if viaProxy {
				a.addLog("info", "订阅通过本机代理拉取成功："+rawURL)
			}
			return resp, body, nil
		}
		if viaProxy {
			errs = append(errs, "经代理："+err.Error())
		} else {
			errs = append(errs, "直连："+err.Error())
		}
	}
	if mode == "auto" && !a.engine.Running() {
		errs = append(errs, "（内核未运行，无法通过代理重试；也可在订阅页用「导入配置」手动导入）")
	}
	return nil, nil, errors.New(strings.Join(errs, "；"))
}

// describeNetErr 把 Go 的网络错误翻译成可操作的中文提示。
func describeNetErr(err error, rawURL string) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "TLS handshake timeout"):
		return fmt.Errorf("TLS 握手超时：TCP 通了但握手没完成，通常是路由器出网 MTU 被卡（PPPoE/MTU 1492 常见）或被中间设备干扰；已自动尝试改用代理")
	case strings.Contains(msg, "x509"), strings.Contains(msg, "certificate"):
		return fmt.Errorf("证书校验失败（检查路由器时间是否准确）: %v", err)
	case strings.Contains(msg, "no such host"):
		u, _ := url.Parse(rawURL)
		host := ""
		if u != nil {
			host = u.Hostname()
		}
		return fmt.Errorf("域名解析失败：%s 无法解析（DNS 被污染或路由器 DNS 不可用）", host)
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("连接被拒绝：%v", err)
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "context deadline"):
		return fmt.Errorf("连接超时：%v", err)
	default:
		return fmt.Errorf("请求失败：%v", err)
	}
}

// saveSubRaw 保存最近一次拉到的原始内容，供「查看原始内容」排查格式问题。
func (a *App) saveSubRaw(id string, body []byte) {
	dir := filepath.Join(a.dataDir, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data := body
	if len(data) > 512<<10 {
		data = data[:512<<10]
	}
	_ = os.WriteFile(filepath.Join(dir, id+".sub"), data, 0o600)
}

// readSubRaw 读取原始内容。
func (a *App) readSubRaw(id string) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(a.dataDir, "cache", id+".sub"))
	if err != nil {
		return nil, false
	}
	return b, true
}

// readBodyWithProgress 分块读取响应体，每 300ms 或每 64KB 回调一次进度。
func readBodyWithProgress(r io.Reader, limit, total int64, progress func(read, total int64)) ([]byte, error) {
	var buf bytes.Buffer
	chunk := make([]byte, 32<<10)
	var read int64
	lastReport := time.Now()
	buf.Grow(64 << 10)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			read += int64(n)
			if read > limit {
				buf.Write(chunk[:n])
				return buf.Bytes(), nil
			}
			buf.Write(chunk[:n])
			if progress != nil && time.Since(lastReport) >= 300*time.Millisecond {
				progress(read, total)
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			if progress != nil {
				progress(read, total)
			}
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	default:
		return fmt.Sprintf("TLS 0x%04x", v)
	}
}

// subUpdateDue 判断某个订阅是否到了自动更新时间。
// 单条订阅的 interval 优先；都为 0 表示关闭。
func subUpdateDue(s Sub, globalHours int, now int64) bool {
	if !s.Enabled || strings.TrimSpace(s.URL) == "" {
		return false
	}
	eff := s.Interval
	if eff <= 0 {
		eff = globalHours
	}
	if eff <= 0 {
		return false // 关闭
	}
	if s.UpdatedAt <= 0 {
		return true // 从未成功更新过
	}
	return now-s.UpdatedAt >= int64(eff)*3600
}
