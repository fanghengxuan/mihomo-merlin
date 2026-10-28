package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TrafficSample 一秒一条的流量采样。
type TrafficSample struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

// Hub 负责转发 mihomo 的流量与日志流，并维护面板自己的日志环形缓冲。
type Hub struct {
	app *App

	tmu   sync.RWMutex
	last  TrafficSample
	hist  []TrafficSample
	tsubs map[chan TrafficSample]struct{}

	lmu   sync.RWMutex
	logs  []LogEntry
	lsubs map[chan LogEntry]struct{}

	totUp   int64
	totDown int64

	pmu   sync.RWMutex
	psubs map[chan ProgressEvent]struct{}
}

// ProgressEvent 订阅任务的进度事件（拉取/解析/写盘/重载）。
type ProgressEvent struct {
	Op   string `json:"op"`   // add | refresh | import | all
	Sub  string `json:"sub"`  // 订阅名
	Step string `json:"step"` // start|connect|proxy|download|parse|write|reload|done|error
	Msg  string `json:"msg"`
	At   int64  `json:"at"`
	Done bool   `json:"done"`
	Err  bool   `json:"err"`
}

const maxLogLines = 400
const maxTrafficHist = 60

func NewHub(app *App) *Hub {
	return &Hub{
		app:   app,
		tsubs: map[chan TrafficSample]struct{}{},
		lsubs: map[chan LogEntry]struct{}{},
		psubs: map[chan ProgressEvent]struct{}{},
	}
}

func (h *Hub) Start() {
	go h.trafficLoop()
	go h.logLoop()
	go h.maintenanceLoop()
}

// ---------- 流量 ----------

func (h *Hub) publishTraffic(s TrafficSample) {
	h.tmu.Lock()
	if s.UpTotal == 0 && s.DownTotal == 0 && (h.totUp > 0 || h.totDown > 0) {
		s.UpTotal, s.DownTotal = h.totUp, h.totDown
	}
	h.tmu.Unlock()

	h.tmu.Lock()
	h.last = s
	h.hist = append(h.hist, s)
	if len(h.hist) > maxTrafficHist {
		h.hist = h.hist[len(h.hist)-maxTrafficHist:]
	}
	subs := make([]chan TrafficSample, 0, len(h.tsubs))
	for c := range h.tsubs {
		subs = append(subs, c)
	}
	h.tmu.Unlock()

	for _, c := range subs {
		select {
		case c <- s:
		default:
		}
	}
}

func (h *Hub) SubscribeTraffic() (chan TrafficSample, func()) {
	c := make(chan TrafficSample, 8)
	h.tmu.Lock()
	h.tsubs[c] = struct{}{}
	h.tmu.Unlock()
	return c, func() {
		h.tmu.Lock()
		delete(h.tsubs, c)
		h.tmu.Unlock()
		close(c)
	}
}

// SetTotals 记录内核累计上下行（来自 /connections 的 uploadTotal/downloadTotal）。
func (h *Hub) SetTotals(up, down int64) {
	h.tmu.Lock()
	h.totUp, h.totDown = up, down
	h.tmu.Unlock()
}

func (h *Hub) LastTraffic() (TrafficSample, []TrafficSample) {
	h.tmu.RLock()
	defer h.tmu.RUnlock()
	hist := make([]TrafficSample, len(h.hist))
	copy(hist, h.hist)
	last := h.last
	if last.UpTotal == 0 && last.DownTotal == 0 && (h.totUp > 0 || h.totDown > 0) {
		last.UpTotal, last.DownTotal = h.totUp, h.totDown
	}
	return last, hist
}

func (h *Hub) trafficLoop() {
	for {
		if !h.app.engine.Running() {
			h.publishTraffic(TrafficSample{})
			time.Sleep(2 * time.Second)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		_ = h.app.clash.StreamTraffic(ctx, func(up, down, upTotal, downTotal int64) {
			h.publishTraffic(TrafficSample{Up: up, Down: down, UpTotal: upTotal, DownTotal: downTotal})
		})
		cancel()
		time.Sleep(time.Second)
	}
}

// ---------- 日志 ----------

func (h *Hub) addLog(level, msg string) {
	e := LogEntry{
		Type:      level,
		Payload:   fmt.Sprintf("[面板] %s", msg),
		Timestamp: time.Now().Unix(),
	}
	h.pushLog(e)
}

func (h *Hub) pushLog(e LogEntry) {
	h.lmu.Lock()
	h.logs = append(h.logs, e)
	if len(h.logs) > maxLogLines {
		h.logs = h.logs[len(h.logs)-maxLogLines:]
	}
	subs := make([]chan LogEntry, 0, len(h.lsubs))
	for c := range h.lsubs {
		subs = append(subs, c)
	}
	h.lmu.Unlock()

	for _, c := range subs {
		select {
		case c <- e:
		default:
		}
	}
}

func (h *Hub) SubscribeLogs() (chan LogEntry, []LogEntry, func()) {
	c := make(chan LogEntry, 64)
	h.lmu.Lock()
	h.lsubs[c] = struct{}{}
	backlog := make([]LogEntry, len(h.logs))
	copy(backlog, h.logs)
	h.lmu.Unlock()
	return c, backlog, func() {
		h.lmu.Lock()
		delete(h.lsubs, c)
		h.lmu.Unlock()
		close(c)
	}
}

func (h *Hub) logLoop() {
	for {
		if !h.app.engine.Running() {
			time.Sleep(2 * time.Second)
			continue
		}
		h.app.mu.RLock()
		level := h.app.settings.LogLevel
		h.app.mu.RUnlock()

		// 每 5 分钟重连一次，使日志级别改动能生效
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		_ = h.app.clash.StreamLogs(ctx, level, func(e LogEntry) { h.pushLog(e) })
		cancel()
		time.Sleep(time.Second)
	}
}

// ---------- 后台维护 ----------

func (h *Hub) maintenanceLoop() {
	defer func() {
		if r := recover(); r != nil {
			h.addLog("error", fmt.Sprintf("后台任务异常：%v", r))
			go h.maintenanceLoop()
		}
	}()
	tickRefresh := time.NewTicker(10 * time.Minute)
	tickCount := time.NewTicker(3 * time.Minute)
	defer tickRefresh.Stop()
	defer tickCount.Stop()

	h.syncProviderCounts()
	for {
		select {
		case <-tickRefresh.C:
			h.app.refreshDueSubs()
		case <-tickCount.C:
			h.syncProviderCounts()
		}
	}
}

// syncProviderCounts 用 provider 的真实节点数修正订阅显示数量；
// 若某个 provider 加载失败（例如节点参数不被内核接受），把内核报错回填到订阅状态。
func (h *Hub) syncProviderCounts() {
	if !h.app.engine.Running() {
		return
	}
	provs, err := h.app.clash.Providers()
	if err != nil {
		return
	}
	logTail := ""
	if b, err := os.ReadFile(h.app.logPath()); err == nil {
		logTail = tail(string(b), 30000)
	}
	h.app.mu.Lock()
	defer h.app.mu.Unlock()
	changed := false
	for i := range h.app.subs {
		sub := &h.app.subs[i]
		p, ok := provs[sub.ID]
		if !ok || len(p.Proxies) == 0 {
			if reason := extractProviderError(logTail, sub.ID); reason != "" && sub.Status != "error" {
				sub.Status = "error"
				sub.Error = "内核加载节点失败：" + reason
				changed = true
			}
			continue
		}
		if sub.NodeCount != len(p.Proxies) {
			sub.NodeCount = len(p.Proxies)
			changed = true
		}
		if sub.Status == "error" {
			sub.Status = "ok"
			sub.Error = ""
			changed = true
		}
	}
	if changed {
		_ = h.app.saveSubsLocked()
	}
}

// extractProviderError 从内核日志里捞出某个 provider 的加载错误。
func extractProviderError(logText, providerID string) string {
	if logText == "" || providerID == "" {
		return ""
	}
	needle := "proxy provider " + providerID + " error: "
	for _, line := range strings.Split(logText, "\n") {
		if i := strings.Index(line, needle); i >= 0 {
			msg := strings.TrimSpace(line[i+len(needle):])
			msg = strings.TrimSpace(strings.Trim(msg, `"`))
			if len(msg) > 180 {
				msg = msg[:180]
			}
			return msg
		}
	}
	return ""
}

// addLog 面板级日志。
func (a *App) addLog(level, msg string) {
	if a.hub == nil {
		return
	}
	a.hub.addLog(level, msg)
}

var _ = json.Marshal

// ---------- 连接数采样 ----------

type sampler struct {
	app *App
	n   atomic.Int64
}

func newSampler(a *App) *sampler { return &sampler{app: a} }

func (s *sampler) count() int64 { return s.n.Load() }

func (s *sampler) Start() {
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for range t.C {
			if !s.app.engine.Running() {
				s.n.Store(0)
				s.app.hub.SetTotals(0, 0)
				continue
			}
			if c, err := s.app.clash.Connections(); err == nil {
				s.n.Store(int64(len(c.Connections)))
				// 内核 /traffic 只给速率，不给累计值；累计值要从 /connections 拿
				s.app.hub.SetTotals(c.UploadTotal, c.DownloadTotal)
			}
		}
	}()
}

// publishProgress 广播一条进度事件。
func (h *Hub) publishProgress(ev ProgressEvent) {
	h.pmu.RLock()
	subs := make([]chan ProgressEvent, 0, len(h.psubs))
	for c := range h.psubs {
		subs = append(subs, c)
	}
	h.pmu.RUnlock()
	for _, c := range subs {
		select {
		case c <- ev:
		default:
		}
	}
}

// SubscribeProgress 订阅进度事件流。
func (h *Hub) SubscribeProgress() (chan ProgressEvent, func()) {
	c := make(chan ProgressEvent, 32)
	h.pmu.Lock()
	if h.psubs == nil {
		h.psubs = map[chan ProgressEvent]struct{}{}
	}
	h.psubs[c] = struct{}{}
	h.pmu.Unlock()
	return c, func() {
		h.pmu.Lock()
		delete(h.psubs, c)
		h.pmu.Unlock()
		close(c)
	}
}

// prog 便捷方法：发进度 + 写进日志页。
func (a *App) prog(op, sub, step, msg string, done, errFlag bool) {
	if a.hub != nil {
		a.hub.publishProgress(ProgressEvent{
			Op: op, Sub: sub, Step: step, Msg: msg,
			At: time.Now().Unix(), Done: done, Err: errFlag,
		})
	}
}

// Totals 返回累计上传/下载字节数（来自内核 traffic 流的累计值）。
func (h *Hub) Totals() (int64, int64) {
	h.tmu.RLock()
	defer h.tmu.RUnlock()
	return h.last.UpTotal, h.last.DownTotal
}
