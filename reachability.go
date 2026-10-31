package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ReachTarget 一个连通性探测目标。
type ReachTarget struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Status    string `json:"status"` // ok|restricted|timeout|fail|unknown
	HTTPCode  int    `json:"httpCode"`
	LatencyMS int64  `json:"latencyMs"`
	Err       string `json:"err,omitempty"`
}

// 内置目标（做成变量便于测试替换）
var reachabilityTargets = []ReachTarget{
	{ID: "google", Name: "Google", URL: "https://www.google.com/generate_204"},
	// 不用 api.github.com：未认证 API 只有 60 次/小时/IP，机场共用出口很容易
	// 被限流成 403，界面就会一直显示「受限」。robots.txt 稳定返回 200。
	{ID: "github", Name: "GitHub", URL: "https://github.com/robots.txt"},
	{ID: "youtube", Name: "YouTube", URL: "https://www.youtube.com/generate_204"},
	{ID: "chatgpt", Name: "ChatGPT", URL: "https://chat.openai.com/cdn-cgi/trace"},
	{ID: "claude", Name: "Claude", URL: "https://claude.ai"},
	{ID: "netflix", Name: "Netflix", URL: "https://www.netflix.com/title/81280792"},
	{ID: "bilibili", Name: "Bilibili", URL: "https://www.bilibili.com"},
}

type reachCache struct {
	mu   sync.Mutex
	at   time.Time
	data []ReachTarget
}

// probeOne 经当前出站探测一个目标。
func (a *App) probeOne(t ReachTarget) ReachTarget {
	out := t
	out.Status = "unknown"
	start := time.Now()
	resp, _, err := a.doFetchQuiet(t.URL, true)
	if err != nil {
		out.LatencyMS = time.Since(start).Milliseconds()
		out.Err = err.Error()
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "timeout") || strings.Contains(low, "deadline") {
			out.Status = "timeout"
		} else {
			out.Status = "fail"
		}
		return out
	}
	out.LatencyMS = time.Since(start).Milliseconds()
	out.HTTPCode = resp.StatusCode
	out.Status = classifyReachStatus(resp.StatusCode, nil)
	if out.Status == "fail" {
		out.Err = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return out
}

// classifyReachStatus 把 HTTP 状态码/错误归类为 ok/restricted/timeout/fail。
func classifyReachStatus(code int, err error) string {
	if err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "timeout") || strings.Contains(low, "deadline") {
			return "timeout"
		}
		return "fail"
	}
	switch {
	case code >= 200 && code < 400:
		return "ok"
	case code == http.StatusUnauthorized || code == http.StatusForbidden || code == 451:
		return "restricted"
	default:
		return "fail"
	}
}

// collectReachability 并发探测全部目标。
func (a *App) collectReachability() []ReachTarget {
	if !a.engine.Running() {
		out := make([]ReachTarget, 0, len(reachabilityTargets))
		for _, t := range reachabilityTargets {
			t.Status = "unknown"
			t.Err = "内核未运行"
			out = append(out, t)
		}
		return out
	}
	results := make([]ReachTarget, len(reachabilityTargets))
	var wg sync.WaitGroup
	for i, t := range reachabilityTargets {
		wg.Add(1)
		go func(i int, t ReachTarget) {
			defer wg.Done()
			results[i] = a.probeOne(t)
		}(i, t)
	}
	wg.Wait()
	return results
}

// handleReachability 返回连通性结果（缓存 45 秒）。
func (a *App) handleReachability(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") != ""

	a.reach.mu.Lock()
	if !force && a.reach.data != nil && time.Since(a.reach.at) < 45*time.Second {
		data := a.reach.data
		at := a.reach.at.Unix()
		a.reach.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "targets": data, "updatedAt": at, "cached": true})
		return
	}
	a.reach.mu.Unlock()

	data := a.collectReachability()

	a.reach.mu.Lock()
	a.reach.data = data
	a.reach.at = time.Now()
	at := a.reach.at.Unix()
	a.reach.mu.Unlock()

	writeJSON(w, 200, map[string]any{"ok": true, "targets": data, "updatedAt": at})
}

// handleCloseConnection 关闭单条连接。
func (a *App) handleCloseConnection(w http.ResponseWriter, r *http.Request) {
	if !a.engine.Running() {
		writeJSON(w, 200, map[string]any{"ok": false, "err": "内核未运行"})
		return
	}
	id := r.PathValue("id")
	if err := a.clash.CloseConnection(id); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "err": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleCloseAllConnections 关闭全部连接。
func (a *App) handleCloseAllConnections(w http.ResponseWriter, r *http.Request) {
	if !a.engine.Running() {
		writeJSON(w, 200, map[string]any{"ok": false, "err": "内核未运行"})
		return
	}
	n := 0
	if snap, err := a.clash.Connections(); err == nil {
		n = len(snap.Connections)
	}
	if err := a.clash.CloseAllConnections(); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "err": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "closed": n})
}
