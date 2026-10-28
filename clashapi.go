package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClashClient 访问 mihomo 的 RESTful API（只监听 127.0.0.1）。
type ClashClient struct {
	base   string
	secret string
	hc     *http.Client
}

func NewClashClient(addr, secret string) *ClashClient {
	return &ClashClient{
		base:   "http://" + addr,
		secret: secret,
		hc:     &http.Client{Timeout: 6 * time.Second},
	}
}

func (c *ClashClient) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("mihomo API %s %s 返回 %d：%s", method, path, resp.StatusCode, msg)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *ClashClient) Version() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var v struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, "GET", "/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

type HistoryItem struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

type ProxyInfo struct {
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	Now          string        `json:"now"`
	All          []string      `json:"all"`
	History      []HistoryItem `json:"history"`
	ProviderName string        `json:"provider-name"`
	Hidden       bool          `json:"hidden"`
	UDP          bool          `json:"udp"`
}

type ProxyMap map[string]ProxyInfo

func (c *ClashClient) Proxies() (ProxyMap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var v struct {
		Proxies ProxyMap `json:"proxies"`
	}
	if err := c.do(ctx, "GET", "/proxies", nil, &v); err != nil {
		return nil, err
	}
	return v.Proxies, nil
}

func (c *ClashClient) Delay(name, testURL string, timeoutMS int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS+2000)*time.Millisecond)
	defer cancel()
	p := "/proxies/" + url.PathEscape(name) + "/delay?timeout=" + itoa(timeoutMS) + "&url=" + url.QueryEscape(testURL)
	var v struct {
		Delay int `json:"delay"`
	}
	if err := c.do(ctx, "GET", p, nil, &v); err != nil {
		return 0, err
	}
	return v.Delay, nil
}

func (c *ClashClient) Select(group, node string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	return c.do(ctx, "PUT", "/proxies/"+url.PathEscape(group), map[string]string{"name": node}, nil)
}

type Rule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
}

func (c *ClashClient) Rules() ([]Rule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var v struct {
		Rules []Rule `json:"rules"`
	}
	if err := c.do(ctx, "GET", "/rules", nil, &v); err != nil {
		return nil, err
	}
	return v.Rules, nil
}

type ConnSnapshot struct {
	DownloadTotal int64 `json:"downloadTotal"`
	UploadTotal   int64 `json:"uploadTotal"`
	Connections   []struct {
		ID       string `json:"id"`
		Metadata struct {
			Host      string `json:"host"`
			SniffHost string `json:"sniffHost"`
			DestIP    string `json:"destinationIP"`
			DestPort  string `json:"destinationPort"`
			Network   string `json:"network"`
			Type      string `json:"type"` // 入口类型：Redir / Tun / Socks / HTTP…
			Process   string `json:"processPath"`
		} `json:"metadata"`
		Upload      int64    `json:"upload"`
		Download    int64    `json:"download"`
		Chains      []string `json:"chains"`
		Rule        string   `json:"rule"`
		RulePayload string   `json:"rulePayload"`
		Start       string   `json:"start"`
	} `json:"connections"`
}

func (c *ClashClient) Connections() (*ConnSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var v ConnSnapshot
	if err := c.do(ctx, "GET", "/connections", nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *ClashClient) ReloadConfig(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.do(ctx, "PUT", "/configs?force=true", map[string]any{"path": path}, nil)
}

type ProviderInfo struct {
	Name         string      `json:"name"`
	VehicleType  string      `json:"vehicleType"`
	Proxies      []ProxyInfo `json:"proxies"`
	TestURL      string      `json:"testUrl"`
	ExpectedStat string      `json:"expectedStatus"`
	UpdatedAt    string      `json:"updatedAt"`
}

// Providers 返回 proxy-provider 列表（含每个订阅的节点明细）。
func (c *ClashClient) Providers() (map[string]ProviderInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var v struct {
		Providers map[string]ProviderInfo `json:"providers"`
	}
	if err := c.do(ctx, "GET", "/providers/proxies", nil, &v); err != nil {
		return nil, err
	}
	return v.Providers, nil
}

type ProxyProvider struct {
	Name string
	Type string
}

// HealthCheck 对 provider 触发一次健康检查（批量测速用）。
func (c *ClashClient) ProviderHealthCheck(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.do(ctx, "GET", "/providers/proxies/"+url.PathEscape(name)+"/healthcheck", nil, nil)
}

// StreamTraffic 订阅 mihomo 的流量流，逐条回调；函数返回即断开。
func (c *ClashClient) StreamTraffic(ctx context.Context, fn func(up, down, upTotal, downTotal int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/traffic", nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("traffic 流返回 %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v struct {
			Up        int64 `json:"up"`
			Down      int64 `json:"down"`
			UpTotal   int64 `json:"upTotal"`
			DownTotal int64 `json:"downTotal"`
		}
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			continue
		}
		fn(v.Up, v.Down, v.UpTotal, v.DownTotal)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

type LogEntry struct {
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"-"`
}

// StreamLogs 订阅 mihomo 日志流。
func (c *ClashClient) StreamLogs(ctx context.Context, level string, fn func(LogEntry)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/logs?level="+url.QueryEscape(level), nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("logs 流返回 %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		fn(LogEntry{Type: e.Type, Payload: e.Payload, Timestamp: time.Now().Unix()})
	}
	return nil
}

// CoreConfig 是内核当前生效的配置（用于验证出站模式、端口是否真的被应用）。
type CoreConfig struct {
	Mode      string `json:"mode"`
	MixedPort int    `json:"mixed-port"`
	RedirPort int    `json:"redir-port"`
	LogLevel  string `json:"log-level"`
	IPv6      bool   `json:"ipv6"`
}

// Configs 读取内核实际运行的配置。
func (c *ClashClient) Configs() (*CoreConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out CoreConfig
	if err := c.do(ctx, "GET", "/configs", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CloseConnection 关闭一条连接。
func (c *ClashClient) CloseConnection(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.do(ctx, "DELETE", "/connections/"+id, nil, nil)
}

// CloseAllConnections 关闭全部连接。
func (c *ClashClient) CloseAllConnections() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.do(ctx, "DELETE", "/connections", nil, nil)
}

// ProviderProxyIndex 把 provider 里的节点合并进来（它们不在 /proxies 顶层表里），
// 并返回 provider 名 → 节点名的映射，便于查订阅归属。
func (c *ClashClient) ProviderProxyIndex() (ProxyMap, map[string]string, error) {
	proxies, err := c.Proxies()
	if err != nil {
		return nil, nil, err
	}
	idx := ProxyMap{}
	for name, p := range proxies {
		idx[name] = p
	}
	owner := map[string]string{}
	if provs, err := c.Providers(); err == nil {
		for provName, pr := range provs {
			for _, p := range pr.Proxies {
				if p.Name == "" {
					continue
				}
				p.ProviderName = provName
				if _, exists := idx[p.Name]; !exists {
					idx[p.Name] = p
				}
				owner[p.Name] = provName
			}
		}
	}
	return idx, owner, nil
}
