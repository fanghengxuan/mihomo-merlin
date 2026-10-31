package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- 基础响应 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"ok": false, "error": msg})
}

func okBody(extra map[string]any) map[string]any {
	m := map[string]any{"ok": true}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func publicSettings(s Settings) map[string]any {
	return map[string]any{
		"mode":              s.Mode,
		"allowLan":          s.AllowLan,
		"redirPort":         s.RedirPort,
		"mixedPort":         s.MixedPort,
		"logLevel":          s.LogLevel,
		"autoStart":         s.AutoStart,
		"blockQuic":         s.BlockQuic,
		"dnsEnable":         s.DNSEnable,
		"dnsMode":           s.DNSMode,
		"dnsListen":         s.DNSListen,
		"dnsNameserver":     s.DNSNameserver,
		"dnsFallback":       s.DNSFallback,
		"dnsFakeIpRange":    s.DNSFakeIPRange,
		"routerSelf":        s.RouterSelf,
		"subInterval":       s.SubInterval,
		"panelPassword":     s.PanelPassword,
		"rulePreset":        s.RulePreset,
		"sniffer":           s.Sniffer,
		"autoRestart":       s.AutoRestart,
		"restartEveryHours": s.RestartEveryHours,
		"restartAt":         s.RestartAt,
		"autoIntervalSec":   s.AutoIntervalSec,
		"autoToleranceMs":   s.AutoToleranceMs,

		// 订阅拉取策略
		"subUA":        s.SubUA,
		"subUACustom":  s.SubUACustom,
		"subInsecure":  s.SubInsecure,
		"subProxyMode": s.SubProxyMode,

		// 国内 IP 规则订阅
		"ruleSourcePreset": s.RuleSourcePreset,
		"ruleSourceURL":    s.RuleSourceURL,
		"ruleAutoUpdate":   s.RuleAutoUpdate,
		"ruleIntervalH":    s.RuleIntervalH,
	}
}

// ---------- 状态 / 登录 / 设置 ----------

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	s := a.settings
	subCount := len(a.subs)
	nodeCount := 0
	var lastRefresh int64
	for _, sb := range a.subs {
		nodeCount += sb.NodeCount
		if sb.UpdatedAt > lastRefresh {
			lastRefresh = sb.UpdatedAt
		}
	}
	a.mu.RUnlock()

	last, _ := a.hub.LastTraffic()
	engine := map[string]any{
		"running":       a.engine.Running(),
		"lastRestartAt": a.lastRestart(),
		"nextRestartAt": a.nextRestartAt(time.Now()),
		// 用户是否期望它运行（enabled 标记）：用于解释「为什么没在跑 / 会不会自动拉起」
		"wanted":       engineWanted(a.dataDir),
		"autoRecover":  a.engine.RecoverState(),
		"autoRestart":  s.AutoRestart,
		"restartEvery": s.RestartEveryHours,
		"restartAt":    s.RestartAt,
	}
	if a.engine.Running() {
		pi := a.engine.PID()
		ver, cfg, mem := a.coreStatus(pi)
		engine["version"] = ver
		engine["pid"] = pi
		engine["uptime"] = a.engine.Uptime()
		engine["mode"] = s.Mode
		// 内核实际生效的模式（用于确认设置真的应用了）
		if cfg != nil {
			engine["modeActual"] = cfg.Mode
			engine["mixedPortActual"] = cfg.MixedPort
		}
		engine["redirPort"] = s.RedirPort
		engine["mixedPort"] = s.MixedPort
		engine["memory"] = mem
	}

	writeJSON(w, 200, map[string]any{
		"ok": true,
		"panel": map[string]any{
			"version":  panelVersion,
			"uptime":   int64(time.Since(a.startedAt).Seconds()),
			"platform": "linux/arm",
			"dataDir":  a.dataDir,
		},
		"engine": engine,
		"net": map[string]any{
			"uploadSpeed":   last.Up,
			"downloadSpeed": last.Down,
			"upTotal":       last.UpTotal,
			"downTotal":     last.DownTotal,
			"connections":   a.samples.count(),
		},
		"iptables": map[string]any{
			"applied":   a.engine.Applied(),
			"lastApply": a.engine.LastApply(),
			"blockQuic": s.BlockQuic || (a.engine.Applied() && s.BlockQuic),
		},
		"subs": map[string]any{
			"count":       subCount,
			"nodeCount":   nodeCount,
			"lastRefresh": lastRefresh,
		},
	})
}

// coreStatus 返回内核版本、实际配置与内存占用，带 TTL 缓存。
// 面板每 3 秒轮询一次 /api/status，若每次都外呼内核，会在路由器上造成
// 不必要的 CPU/网络开销；这里把 version(60s)/configs(10s)/内存(5s) 分层缓存。
func (a *App) coreStatus(pid int) (string, *CoreConfig, float64) {
	a.coreMu.Lock()
	defer a.coreMu.Unlock()

	now := time.Now()
	if a.coreVersionAt.IsZero() || now.Sub(a.coreVersionAt) >= time.Minute {
		if v, err := a.clash.Version(); err == nil {
			a.coreVersion = v
		}
		// 失败也更新时间：避免内核 API 不可用时每个请求都重试。
		a.coreVersionAt = now
	}
	if a.coreCfg == nil || now.Sub(a.coreCfgAt) >= 10*time.Second {
		if cfg, err := a.clash.Configs(); err == nil {
			a.coreCfg = cfg
		}
		a.coreCfgAt = now
	}
	if a.memPID != pid || now.Sub(a.memAt) >= 5*time.Second {
		a.memMB = memUsageMB(pid)
		a.memPID = pid
		a.memAt = now
	}
	return a.coreVersion, a.coreCfg, a.memMB
}

// invalidateCoreCache 在启停内核或保存设置后让缓存立即失效。
func (a *App) invalidateCoreCache() {
	a.coreMu.Lock()
	a.coreVersionAt = time.Time{}
	a.coreCfg = nil
	a.coreCfgAt = time.Time{}
	a.memAt = time.Time{}
	a.coreMu.Unlock()
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	a.mu.RLock()
	pwd := a.settings.PanelPassword
	a.mu.RUnlock()

	if pwd == "" {
		writeJSON(w, 200, okBody(nil))
		return
	}
	if body.Password != pwd {
		writeErr(w, http.StatusUnauthorized, "密码错误")
		return
	}
	token := randomToken(16)
	a.sessionMu.Lock()
	a.session = token
	a.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "mmp_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   7 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, 200, okBody(nil))
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	s := a.settings
	a.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"ok": true, "settings": publicSettings(s)})
}

func (a *App) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, 400, "请求体不是合法 JSON")
		return
	}

	a.mu.Lock()
	prev := a.settings
	s := a.settings
	if err := s.applyPatch(patch); err != nil {
		a.mu.Unlock()
		writeErr(w, 400, err.Error())
		return
	}
	a.settings = s
	err := a.saveSettings()
	a.mu.Unlock()

	// 规则来源换了就要立刻重新下载，否则只是「设置里写了新来源、实际还用旧的 chnroute.txt」
	ruleSourceChanged := prev.RuleSourcePreset != s.RuleSourcePreset || prev.RuleSourceURL != s.RuleSourceURL
	if ruleSourceChanged {
		a.bgTasks.Add(1)
		go func() {
			defer a.bgTasks.Done()
			if _, rerr := a.UpdateRuleSet(""); rerr != nil {
				a.addLog("warning", "规则来源已切换，但下载失败："+rerr.Error())
				return
			}
			a.addLog("info", "规则来源已切换并重新下载完成")
			if a.engine.Running() {
				_ = a.engine.Reload()
			}
		}()
	}
	if err != nil {
		writeErr(w, 500, "保存设置失败："+err.Error())
		return
	}
	a.invalidateCoreCache()
	if prev.LogLevel != s.LogLevel && a.hub != nil {
		a.hub.SetLogLevel(s.LogLevel)
	}
	a.addLog("info", "设置已更新")

	// 同步应用：设置里的 mode / 端口 / 规则集都会影响运行中的内核，
	// 异步重载会让界面读到“设置已保存但其实没生效”的假象。
	if a.engine.Running() {
		if err := a.engine.Reload(); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "设置已保存，但应用到内核失败：" + err.Error(),
				"settings": publicSettings(s)})
			return
		}
		_ = a.engine.ApplyIptables()
	}
	writeJSON(w, 200, map[string]any{"ok": true, "settings": publicSettings(s)})
}

// ---------- 引擎 ----------

func (a *App) handleEngineStart(w http.ResponseWriter, r *http.Request) {
	if err := a.engine.Start(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	go a.hub.syncProviderCounts()
	writeJSON(w, 200, okBody(map[string]any{"message": "已启动"}))
}

func (a *App) handleEngineStop(w http.ResponseWriter, r *http.Request) {
	if err := a.engine.Stop(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, okBody(map[string]any{"message": "已停止"}))
}

func (a *App) handleEngineRestart(w http.ResponseWriter, r *http.Request) {
	if err := a.engine.Restart(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	a.noteRestart()
	go a.hub.syncProviderCounts()
	writeJSON(w, 200, okBody(map[string]any{"message": "已重启"}))
}

// handleIptablesApply 重放转发规则，供 Merlin 的 nat-start 钩子即时调用。
// 鉴权由 withAuth 处理：只放行回环地址，其余请求仍需登录。
func (a *App) handleIptablesApply(w http.ResponseWriter, r *http.Request) {
	if err := a.engine.ApplyIptables(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleEngineReload(w http.ResponseWriter, r *http.Request) {
	if err := a.appReload(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, okBody(map[string]any{"message": "配置已重载"}))
}

func (a *App) appReload() error {
	if !a.engine.Running() {
		if err := a.WriteConfig(); err != nil {
			return err
		}
		return nil
	}
	return a.engine.Reload()
}

// ---------- 订阅 ----------

func subDTO(s Sub, reveal bool, globalInterval int) map[string]any {
	return map[string]any{
		"id":            s.ID,
		"name":          s.Name,
		"url":           maskURL(s.URL, reveal),
		"interval":      s.Interval,
		"enabled":       s.Enabled,
		"nodeCount":     s.NodeCount,
		"format":        s.Format,
		"updatedAt":     s.UpdatedAt,
		"status":        s.Status,
		"error":         s.Error,
		"traffic":       s.Traffic,
		"skipped":       s.Skipped,
		"autoUpdate":    autoUpdateInfo(s, globalInterval),
		"includeFilter": s.IncludeFilter,
		"excludeFilter": s.ExcludeFilter,
	}
}

// autoUpdateInfo 计算该订阅的自动更新状态与下次时间。
func autoUpdateInfo(s Sub, globalInterval int) map[string]any {
	eff := s.Interval
	if eff <= 0 {
		eff = globalInterval
	}
	manual := strings.TrimSpace(s.URL) == ""
	out := map[string]any{
		"enabled":  eff > 0 && !manual,
		"interval": eff,
		"nextAt":   int64(0),
		"manual":   manual,
	}
	if manual || eff <= 0 {
		out["interval"] = 0
		return out
	}
	if s.UpdatedAt > 0 {
		out["nextAt"] = s.UpdatedAt + int64(eff)*3600
	}
	return out
}

func (a *App) handleListSubs(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	globalInterval := a.settings.SubInterval
	out := make([]map[string]any, 0, len(a.subs))
	for _, s := range a.subs {
		out = append(out, subDTO(s, false, globalInterval))
	}
	a.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"ok": true, "subs": out})
}

func (a *App) handleGetSub(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	reveal := r.URL.Query().Get("reveal") == "1"
	a.mu.RLock()
	sub, _ := a.findSub(id)
	var dto map[string]any
	if sub != nil {
		dto = subDTO(*sub, reveal, a.settings.SubInterval)
	}
	a.mu.RUnlock()
	if dto == nil {
		writeErr(w, 404, "订阅不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "sub": dto})
}

// subNameExists 判断订阅名是否已被占用（忽略大小写与首尾空格），excludeID 用于改名时排除自己。
// 调用方需持有 a.mu。
func (a *App) subNameExists(name, excludeID string) bool {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return false
	}
	for _, s := range a.subs {
		if s.ID == excludeID {
			continue
		}
		if strings.ToLower(strings.TrimSpace(s.Name)) == want {
			return true
		}
	}
	return false
}

func (a *App) handleCreateSub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string `json:"name"`
		URL           string `json:"url"`
		Interval      int    `json:"interval"`
		IncludeFilter string `json:"includeFilter"`
		ExcludeFilter string `json:"excludeFilter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "请求体不是合法 JSON")
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	if u, err := url.Parse(body.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		writeErr(w, 400, "订阅地址必须是 http/https 链接")
		return
	}
	if body.Name == "" {
		if u, err := url.Parse(body.URL); err == nil {
			body.Name = u.Hostname()
		} else {
			body.Name = "新订阅"
		}
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Interval < 0 {
		body.Interval = 0
	}

	sub := Sub{
		ID:            "s_" + randomToken(4),
		Name:          body.Name,
		URL:           body.URL,
		Interval:      body.Interval,
		Enabled:       true,
		Status:        "never",
		IncludeFilter: strings.TrimSpace(body.IncludeFilter),
		ExcludeFilter: strings.TrimSpace(body.ExcludeFilter),
	}

	// 查重与追加放在同一把锁里，避免两个并发请求同时通过查重。
	a.mu.Lock()
	if a.subNameExists(body.Name, "") {
		a.mu.Unlock()
		writeErr(w, 400, fmt.Sprintf("订阅名称「%s」已存在，请换一个", body.Name))
		return
	}
	a.subs = append(a.subs, sub)
	a.mu.Unlock()

	// 网络拉取不持有 a.mu：期间 /api/subs、/api/status 等读接口不受阻塞。
	err := a.refreshSubByID(sub.ID)

	a.mu.RLock()
	cur, _ := a.findSub(sub.ID)
	var dto map[string]any
	var finalCount int
	if cur != nil {
		dto = subDTO(*cur, true, a.settings.SubInterval)
		finalCount = cur.NodeCount
	}
	a.mu.RUnlock()

	if err != nil {
		// 创建成功但拉取失败：保留订阅，状态里带错误
		errMsg := err.Error()
		a.addLog("error", fmt.Sprintf("添加订阅 %s 失败：%s", sub.Name, errMsg))
		writeJSON(w, 200, map[string]any{
			"ok": true, "id": sub.ID, "error": errMsg, "sub": dto,
		})
		return
	}

	// 解析出 0 个节点时补一条可操作的提示，并刷新 DTO。
	a.mu.Lock()
	cur, _ = a.findSub(sub.ID)
	if cur != nil {
		finalCount = cur.NodeCount
		if finalCount == 0 {
			cur.Status = "error"
			cur.Error = "解析出 0 个节点：可能是暂不支持的协议，点「原始内容」看订阅返回了什么"
		}
		_ = a.saveSubsLocked()
		dto = subDTO(*cur, true, a.settings.SubInterval)
	}
	a.mu.Unlock()
	if finalCount == 0 {
		a.prog("add", sub.Name, "error", "解析出 0 个节点，可在订阅卡片点「原始内容」查看返回内容", true, true)
	} else {
		a.prog("add", sub.Name, "nodes", fmt.Sprintf("内核已加载 %d 个节点", finalCount), false, false)
	}
	a.addLog("info", fmt.Sprintf("已添加订阅 %s（%d 个节点）", sub.Name, finalCount))
	if a.engine.Running() {
		a.prog("add", sub.Name, "reload", "重新生成配置并热重载内核…", false, false)
		_ = a.appReload()
		a.hub.syncProviderCounts()
	}
	a.prog("add", sub.Name, "done", "订阅已添加", true, false)
	writeJSON(w, 200, map[string]any{"ok": true, "id": sub.ID, "sub": dto})
}

func (a *App) handleUpdateSub(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name          *string `json:"name"`
		URL           *string `json:"url"`
		Interval      *int    `json:"interval"`
		Enabled       *bool   `json:"enabled"`
		IncludeFilter *string `json:"includeFilter"`
		ExcludeFilter *string `json:"excludeFilter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "请求体不是合法 JSON")
		return
	}

	a.mu.Lock()
	sub, _ := a.findSub(id)
	if sub == nil {
		a.mu.Unlock()
		writeErr(w, 404, "订阅不存在")
		return
	}
	refetch := false
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		newName := strings.TrimSpace(*body.Name)
		if a.subNameExists(newName, id) {
			a.mu.Unlock()
			writeErr(w, 400, fmt.Sprintf("订阅名称「%s」已存在，请换一个", newName))
			return
		}
		sub.Name = newName
	}
	if body.URL != nil {
		nu := strings.TrimSpace(*body.URL)
		// 前端编辑时可能把脱敏后的 *** 回传：视为未修改，避免把订阅地址写坏。
		if nu != "" && !strings.Contains(nu, "***") {
			if u, err := url.Parse(nu); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				a.mu.Unlock()
				writeErr(w, 400, "订阅地址必须是 http/https 链接")
				return
			}
			if nu != sub.URL {
				sub.URL = nu
				refetch = true
			}
		}
	}
	if body.Interval != nil {
		iv := *body.Interval
		if iv < 0 {
			iv = 0
		}
		sub.Interval = iv
	}
	if body.Enabled != nil {
		sub.Enabled = *body.Enabled
	}
	if body.IncludeFilter != nil {
		sub.IncludeFilter = strings.TrimSpace(*body.IncludeFilter)
	}
	if body.ExcludeFilter != nil {
		sub.ExcludeFilter = strings.TrimSpace(*body.ExcludeFilter)
	}
	var err error
	if !refetch {
		err = a.saveSubsLocked()
	}
	a.mu.Unlock()

	if refetch {
		// 网络拉取不持锁；refreshSubByID 内部用 subMu 串行化。
		err = a.refreshSubByID(id)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	a.mu.RLock()
	var dto map[string]any
	if cur, _ := a.findSub(id); cur != nil {
		dto = subDTO(*cur, false, a.settings.SubInterval)
	}
	a.mu.RUnlock()

	if a.engine.Running() {
		_ = a.appReload()
	}
	writeJSON(w, 200, okBody(map[string]any{"sub": dto}))
}

func (a *App) handleDeleteSub(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a.subMu.Lock()
	a.mu.Lock()
	_, idx := a.findSub(id)
	if idx < 0 {
		a.mu.Unlock()
		a.subMu.Unlock()
		writeErr(w, 404, "订阅不存在")
		return
	}
	name := a.subs[idx].Name
	a.subs = append(a.subs[:idx], a.subs[idx+1:]...)
	_ = a.saveSubsLocked()
	a.mu.Unlock()

	// 仍在 subMu 内：避免正在进行的刷新把刚删掉的 provider 文件又写回来。
	_ = removeIfExists(a.providerPath(id))
	_ = removeIfExists(filepath.Join(a.dataDir, "cache", id+".sub"))
	a.subMu.Unlock()
	a.addLog("info", "已删除订阅 "+name)
	if a.engine.Running() {
		_ = a.appReload()
	}
	writeJSON(w, 200, okBody(nil))
}

func (a *App) handleRefreshSub(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	err := a.refreshSubByID(id)

	a.mu.RLock()
	var dto map[string]any
	name := ""
	if cur, _ := a.findSub(id); cur != nil {
		dto = subDTO(*cur, false, a.settings.SubInterval)
		name = cur.Name
	}
	a.mu.RUnlock()

	if err != nil {
		if err.Error() == "订阅不存在" {
			writeErr(w, 404, err.Error())
		} else {
			writeErr(w, 502, err.Error())
		}
		return
	}
	if a.engine.Running() {
		a.prog("refresh", name, "reload", "重新生成配置并热重载内核…", false, false)
		_ = a.appReload()
		a.hub.syncProviderCounts()
	}
	a.prog("refresh", name, "done", "订阅已更新", true, false)
	writeJSON(w, 200, okBody(map[string]any{"sub": dto}))
}

func (a *App) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	ids := make([]string, 0, len(a.subs))
	for _, s := range a.subs {
		ids = append(ids, s.ID)
	}
	a.mu.RUnlock()

	refreshed, failed := 0, 0
	var firstErr string
	for _, id := range ids {
		a.mu.RLock()
		sub, _ := a.findSub(id)
		manual := sub == nil || strings.TrimSpace(sub.URL) == ""
		a.mu.RUnlock()
		if manual {
			continue
		}
		err := a.refreshSubByID(id)
		if err != nil {
			failed++
			if firstErr == "" {
				firstErr = err.Error()
			}
		} else {
			refreshed++
		}
	}
	if a.engine.Running() && refreshed > 0 {
		_ = a.appReload()
		go a.hub.syncProviderCounts()
	}
	resp := okBody(map[string]any{"refreshed": refreshed, "failed": failed})
	if firstErr != "" {
		resp["error"] = firstErr
	}
	writeJSON(w, 200, resp)
}

// ---------- 节点 ----------

type nodeDTO struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	IsGroup bool   `json:"isGroup,omitempty"`
	Sub     string `json:"sub"`
	Delay   int    `json:"delay"`
	DelayAt int64  `json:"delayAt"`
}

type groupDTO struct {
	Name  string    `json:"name"`
	Type  string    `json:"type"`
	Now   string    `json:"now"`
	Nodes []nodeDTO `json:"nodes"`
}

func isGroupType(t string) bool {
	switch t {
	case "Selector", "URLTest", "Fallback", "LoadBalance", "Relay":
		return true
	}
	return false
}

func isPseudoProxy(t string) bool {
	switch t {
	case "Direct", "Reject", "RejectDrop", "Compatible", "Pass", "Dns":
		return true
	}
	return false
}

func (a *App) handleNodes(w http.ResponseWriter, r *http.Request) {
	if !a.engine.Running() {
		// 内核没启动也要能看到节点：直接读订阅的节点文件做预览
		groups := a.previewGroups()
		writeJSON(w, 200, map[string]any{"ok": true, "groups": groups, "preview": true})
		return
	}
	proxies, err := a.clash.Proxies()
	if err != nil {
		writeErr(w, 502, "读取节点失败："+err.Error())
		return
	}

	// proxy-provider 里的节点不会出现在 /proxies 的顶层表里，必须从
	// /providers/proxies 合并进来，否则分组里只剩组名、看不到真实节点。
	nodeIdx := map[string]ProxyInfo{}
	if provs, err := a.clash.Providers(); err == nil {
		for provName, pr := range provs {
			for _, p := range pr.Proxies {
				if p.Name == "" {
					continue
				}
				p.ProviderName = provName
				nodeIdx[p.Name] = p
			}
		}
	}
	for name, p := range proxies {
		if _, exists := nodeIdx[name]; !exists {
			nodeIdx[name] = p
		}
	}

	a.mu.RLock()
	subNames := map[string]string{}
	for _, s := range a.subs {
		subNames[s.ID] = s.Name
	}
	a.mu.RUnlock()

	var groups []groupDTO
	for name, p := range proxies {
		if !isGroupType(p.Type) {
			continue
		}
		// GLOBAL / 同名内置组不展示，避免和我们的「节点选择」重复
		if name == "GLOBAL" {
			continue
		}
		g := groupDTO{Name: name, Type: p.Type, Now: p.Now}
		seen := map[string]bool{}
		for _, n := range p.All {
			if seen[n] {
				continue
			}
			pi, ok := nodeIdx[n]
			if ok && isPseudoProxy(pi.Type) {
				continue
			}
			seen[n] = true
			if !ok {
				pi = ProxyInfo{Name: n, Type: "未知"}
			}
			delay := 0
			if len(pi.History) > 0 {
				delay = pi.History[len(pi.History)-1].Delay
			}
			isGroupMember := isGroupType(pi.Type) || strings.EqualFold(n, "DIRECT") ||
				strings.EqualFold(n, "REJECT") || strings.EqualFold(n, "GLOBAL")
			g.Nodes = append(g.Nodes, nodeDTO{
				Name:    n,
				Type:    pi.Type,
				IsGroup: isGroupMember,
				Sub:     subNames[pi.ProviderName],
				Delay:   delay,
			})
		}
		groups = append(groups, g)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		rank := func(n string) int {
			switch {
			case n == groupMain:
				return 0
			case n == groupAuto:
				return 1
			case strings.HasPrefix(n, "📦 "):
				return 2
			}
			return 3
		}
		ri, rj := rank(groups[i].Name), rank(groups[j].Name)
		if ri != rj {
			return ri < rj
		}
		return groups[i].Name < groups[j].Name
	})

	writeJSON(w, 200, map[string]any{"ok": true, "groups": groups})
}

func (a *App) handleSelect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Group string `json:"group"`
		Node  string `json:"node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "请求体不是合法 JSON")
		return
	}
	if body.Group == "" || body.Node == "" {
		writeErr(w, 400, "group 与 node 不能为空")
		return
	}
	if !a.engine.Running() {
		writeErr(w, 400, "内核未运行")
		return
	}
	if err := a.clash.Select(body.Group, body.Node); err != nil {
		writeErr(w, 502, "切换节点失败："+err.Error())
		return
	}
	a.addLog("info", fmt.Sprintf("切换 %s → %s", body.Group, body.Node))
	writeJSON(w, 200, okBody(nil))
}

func (a *App) handleDelayOne(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, 400, "缺少 name 参数")
		return
	}
	testURL := firstNonEmpty(r.URL.Query().Get("url"), testURLConst)
	if !a.engine.Running() {
		writeJSON(w, 200, map[string]any{"ok": true, "name": name, "delay": 0})
		return
	}
	d, err := a.clash.Delay(name, testURL, 3000)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": true, "name": name, "delay": 0})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": name, "delay": d})
}

func (a *App) handleDelayBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Names []string `json:"names"`
		Group string   `json:"group"`
		URL   string   `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "请求体不是合法 JSON")
		return
	}
	if !a.engine.Running() {
		writeErr(w, 400, "内核未运行")
		return
	}
	names := body.Names
	if len(names) == 0 && body.Group != "" {
		proxies, err := a.clash.Proxies()
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		if g, ok := proxies[body.Group]; ok {
			for _, n := range g.All {
				if pi, ok := proxies[n]; ok && !isPseudoProxy(pi.Type) {
					names = append(names, n)
				}
			}
		}
	}
	if len(names) == 0 {
		writeErr(w, 400, "没有可测速的节点")
		return
	}
	if len(names) > 200 {
		names = names[:200]
	}
	testURL := firstNonEmpty(body.URL, testURLConst)

	results := make(map[string]int, len(names))
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, err := a.clash.Delay(name, testURL, 3000)
			mu.Lock()
			if err != nil {
				results[name] = 0
			} else {
				results[name] = d
			}
			mu.Unlock()
		}(n)
	}
	wg.Wait()
	writeJSON(w, 200, map[string]any{"ok": true, "results": results})
}

// ---------- 规则 / 连接 ----------

func (a *App) handleRules(w http.ResponseWriter, r *http.Request) {
	if !a.engine.Running() {
		writeJSON(w, 200, map[string]any{"ok": true, "rules": []any{}})
		return
	}
	rules, err := a.clash.Rules()
	if err != nil {
		writeErr(w, 502, "读取规则失败："+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "rules": rules})
}

// connRow 是连接列表用的扁平结构（前端已按此处字段写死）。
type connRow struct {
	ID          string   `json:"id"`
	Host        string   `json:"host"`
	Dest        string   `json:"dest"` // destinationIP:port（IPv6 带方括号）
	Network     string   `json:"network"`
	Type        string   `json:"type"` // 入口类型：Redir / Tun / Socks…
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
	Chains      []string `json:"chains"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
	Start       string   `json:"start"`
}

// joinDest 拼出 destinationIP:port；没有端口时只给 IP。
func joinDest(ip, port string) string {
	ip = strings.TrimSpace(ip)
	port = strings.TrimSpace(port)
	if ip == "" {
		return ""
	}
	if port == "" {
		return ip
	}
	return net.JoinHostPort(ip, port)
}

// handleConnections 返回连接明细：rows 是给新面板的扁平列表，
// connections 保留旧的 clash 风格快照以兼容老前端。
// 内核未运行或读取失败时同样返回 200，只是 rows 为空并带 err 说明。
func (a *App) handleConnections(w http.ResponseWriter, r *http.Request) {
	// 新前端传 legacy=0 时跳过旧 connections 数组，响应体约减半。
	legacy := r.URL.Query().Get("legacy") != "0"
	rows := make([]connRow, 0)
	var conns []map[string]any
	if legacy {
		conns = make([]map[string]any, 0)
	}
	var up, down, upTotal, downTotal int64
	errText := ""

	if !a.engine.Running() {
		errText = "内核未运行"
	} else if snap, err := a.clash.Connections(); err != nil {
		errText = "读取连接失败：" + err.Error()
	} else {
		upTotal, downTotal = snap.UploadTotal, snap.DownloadTotal
		for _, c := range snap.Connections {
			dest := joinDest(c.Metadata.DestIP, c.Metadata.DestPort)
			host := firstNonEmpty(c.Metadata.SniffHost, c.Metadata.Host)
			chains := c.Chains
			if chains == nil {
				chains = []string{}
			}
			rows = append(rows, connRow{
				ID:          c.ID,
				Host:        firstNonEmpty(host, dest),
				Dest:        dest,
				Network:     c.Metadata.Network,
				Type:        c.Metadata.Type,
				Rule:        c.Rule,
				RulePayload: c.RulePayload,
				Chains:      chains,
				Upload:      c.Upload,
				Download:    c.Download,
				Start:       c.Start,
			})
			up += c.Upload
			down += c.Download
			if legacy {
				conns = append(conns, map[string]any{
					"id":       c.ID,
					"host":     firstNonEmpty(c.Metadata.SniffHost, c.Metadata.Host, c.Metadata.DestIP),
					"network":  c.Metadata.Network,
					"rule":     firstNonEmpty(c.RulePayload, c.Rule),
					"chains":   chains,
					"upload":   c.Upload,
					"download": c.Download,
					"start":    c.Start,
				})
			}
		}
	}

	body := map[string]any{
		"ok":            true,
		"count":         len(rows),
		"up":            up,
		"down":          down,
		"upText":        humanBytes(up),
		"downText":      humanBytes(down),
		"uploadTotal":   upTotal,
		"downloadTotal": downTotal,
		"rows":          rows,
	}
	if legacy {
		body["connections"] = conns
	}
	if errText != "" {
		body["err"] = errText
	}
	writeJSON(w, 200, body)
}

// handleService 汇总内核可达性 / iptables 状态 / 端口与出站模式。
// 内核没起来时返回 200 + 零值，只把原因写进 err。
func (a *App) handleService(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	s := a.settings
	a.mu.RUnlock()

	start := time.Now()
	ver, verr := a.clash.Version()
	latency := time.Since(start).Milliseconds()

	core := map[string]any{"reachable": verr == nil, "version": "", "apiLatencyMs": 0}
	modeActual := ""
	if verr == nil {
		core["version"] = ver
		core["apiLatencyMs"] = latency
		// 内核真实生效的模式（用于确认设置真的应用到了内核）
		if cfg, err := a.clash.Configs(); err == nil {
			modeActual = cfg.Mode
		}
	}

	body := map[string]any{
		"ok":   true,
		"core": core,
		"iptables": map[string]any{
			"applied":   a.engine.Applied(),
			"lastApply": a.engine.LastApply(),
		},
		"ports": map[string]any{
			"redir": s.RedirPort,
			"mixed": s.MixedPort,
		},
		"mode":       s.Mode,
		"modeActual": modeActual,
	}
	if verr != nil {
		body["err"] = "内核 API 不可达：" + verr.Error()
	}
	writeJSON(w, 200, body)
}

// handleIntranet 返回面板所在设备的 LAN 侧信息（路由器上就是 LAN IP）。
func (a *App) handleIntranet(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	iface := a.settings.LanIf
	a.mu.RUnlock()
	iface = firstNonEmpty(strings.TrimSpace(iface), "br0")

	ipv4 := ifaceIPv4(iface)
	lanIP := nvramLanIP()
	if lanIP == "" && len(ipv4) > 0 {
		// nvram 不可用（非路由器环境）时退化为接口地址
		lanIP = ipv4[0]
	}
	hostname := ""
	if h, err := os.Hostname(); err == nil {
		hostname = h
	}

	a.mu.RLock()
	mixed := a.settings.MixedPort
	a.mu.RUnlock()
	panelURL := ""
	if lanIP != "" {
		panelURL = "http://" + lanIP + ":" + itoa(panelPort)
	}

	body := map[string]any{
		"ok":        true,
		"lanIp":     lanIP,
		"iface":     iface,
		"ipv4":      ipv4,
		"hostname":  hostname,
		"panelUrl":  panelURL,
		"mixedPort": mixed,
	}
	if lanIP == "" {
		body["err"] = "未能获取 LAN IP：nvram 不可用，接口 " + iface + " 也没有 IPv4 地址"
	}
	writeJSON(w, 200, body)
}

// handleIPQuality 返回当前出口 IP 的质量画像；查询失败也返回 200（level=unknown）。
func (a *App) handleIPQuality(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") != ""

	a.ipqMu.Lock()
	if !force && a.ipqData != nil && time.Since(a.ipqAt) < ipQualityCacheTTL {
		q := a.ipqData
		a.ipqMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "quality": q, "cached": true})
		return
	}
	a.ipqMu.Unlock()

	q := a.probeIPQuality()
	a.ipqMu.Lock()
	a.ipqAt = time.Now()
	a.ipqData = q
	a.ipqMu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "quality": q})
}

// ---------- SSE ----------

func sseHeaders(w http.ResponseWriter) bool {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "当前连接不支持流式输出")
		return false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()
	return true
}

func (a *App) handleTraffic(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	fl := w.(http.Flusher)
	ch, unsub := a.hub.SubscribeTraffic()
	defer unsub()

	last, _ := a.hub.LastTraffic()
	writeSSE(w, map[string]any{"up": last.Up, "down": last.Down,
		"upTotal": last.UpTotal, "downTotal": last.DownTotal})
	fl.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case s, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, map[string]any{"up": s.Up, "down": s.Down,
				"upTotal": s.UpTotal, "downTotal": s.DownTotal})
			fl.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	fl := w.(http.Flusher)
	ch, backlog, unsub := a.hub.SubscribeLogs()
	defer unsub()

	for _, e := range backlog {
		if logLevelRank(e.Type) < a.hub.LogRank() {
			continue
		}
		writeSSE(w, map[string]any{"type": e.Type, "payload": e.Payload, "time": e.Timestamp})
	}
	fl.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if logLevelRank(e.Type) < a.hub.LogRank() {
				continue
			}
			writeSSE(w, map[string]any{"type": e.Type, "payload": e.Payload, "time": e.Timestamp})
			fl.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", b)
}

const testURLConst = "http://www.gstatic.com/generate_204"

// handleImportSub 直接导入配置文本（Clash YAML 或 base64/明文节点列表）。
// 用于订阅地址在路由器上拉不动（被墙 / MTU / DNS）时的手动兜底。
func (a *App) handleImportSub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&body); err != nil {
		writeErr(w, 400, "请求体不合法或内容过大（上限 16MB）")
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeErr(w, 400, "导入内容为空")
		return
	}

	res, err := ParseSubscription([]byte(body.Content))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "导入配置"
	}
	a.mu.Lock()
	dup := a.subNameExists(name, "")
	a.mu.Unlock()
	if dup {
		writeErr(w, 400, fmt.Sprintf("订阅名称「%s」已存在，请换一个", name))
		return
	}
	sub := Sub{
		ID:        "s_" + randomToken(4),
		Name:      name,
		URL:       "",
		Enabled:   true,
		Format:    res.Format,
		NodeCount: res.NodeCount,
		Skipped:   res.Skipped,
		Status:    "ok",
		UpdatedAt: time.Now().Unix(),
	}
	if len(res.Errs) > 0 {
		sub.Error = "部分节点不支持：" + strings.Join(res.Errs, "；")
	}
	if err := os.MkdirAll(filepath.Dir(a.providerPath(sub.ID)), 0o755); err != nil {
		writeErr(w, 500, "创建目录失败："+err.Error())
		return
	}
	if err := os.WriteFile(a.providerPath(sub.ID), res.ProviderBytes, 0o600); err != nil {
		writeErr(w, 500, "写入节点文件失败："+err.Error())
		return
	}

	a.mu.Lock()
	a.subs = append(a.subs, sub)
	err = a.saveSubsLocked()
	a.mu.Unlock()
	if err != nil {
		writeErr(w, 500, "保存订阅失败："+err.Error())
		return
	}

	a.prog("import", name, "parse", fmt.Sprintf("识别为 %s，解析出 %d 个节点", res.Format, res.NodeCount), false, false)
	a.prog("import", name, "write", "节点已保存", false, false)
	a.addLog("info", fmt.Sprintf("已导入配置 %s（%d 个节点，格式 %s）", name, res.NodeCount, res.Format))
	if a.engine.Running() {
		a.prog("import", name, "reload", "重新生成配置并热重载内核…", false, false)
		_ = a.appReload()
		go a.hub.syncProviderCounts()
	}
	a.prog("import", name, "done", "导入完成", true, false)
	a.mu.RLock()
	globalInterval := a.settings.SubInterval
	a.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"ok": true, "id": sub.ID, "sub": subDTO(sub, true, globalInterval)})
}

// handleProgress 推送订阅任务进度（拉取/解析/写盘/重载），避免用户只能干等。
func (a *App) handleProgress(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	fl := w.(http.Flusher)
	ch, unsub := a.hub.SubscribeProgress()
	defer unsub()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, ev)
			fl.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// handleSubRaw 返回订阅最近一次拉取到的原始内容（截断展示），用于判断格式。
func (a *App) handleSubRaw(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	raw, ok := a.readSubRaw(id)
	if !ok {
		writeErr(w, 404, "还没有保存过这个订阅的原始内容，先点一次「更新」")
		return
	}
	// 只做轻量判断，不跑完整解析
	guess := "未知"
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && isClashConfig(trimmed) {
		guess = "Clash 配置"
	} else if _, ok := tryBase64(trimmed); ok && bytes.Contains(trimmed, []byte("://")) {
		guess = "可能是 base64 链接列表"
	} else if bytes.Contains(trimmed, []byte("://")) {
		guess = "明文链接列表"
	}
	preview := string(raw)
	if len(preview) > 6000 {
		preview = preview[:6000] + "\n…（已截断）"
	}
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"bytes":   len(raw),
		"guess":   guess,
		"preview": preview,
	})
}

// handleRuleSetInfo 返回规则集现状与相关设置。
func (a *App) handleRuleSetInfo(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	custom := a.settings.RuleSourceURL
	auto := a.settings.RuleAutoUpdate
	hours := a.settings.RuleIntervalH
	presetID := a.settings.RuleSourcePreset
	a.mu.RUnlock()
	if presetID == "" {
		if custom != "" {
			presetID = "custom"
		} else {
			presetID = defaultRulePresetID
		}
	}

	presets := make([]map[string]any, 0, len(ruleSourcePresets))
	for _, p := range ruleSourcePresets {
		presets = append(presets, map[string]any{
			"id": p.ID, "name": p.Name, "note": p.Note, "lines": p.Lines,
		})
	}
	writeJSON(w, 200, map[string]any{
		"ok":        true,
		"meta":      a.loadRuleMeta(),
		"preset":    presetID,
		"presets":   presets,
		"customURL": custom,
		"auto":      auto,
		"hours":     hours,
	})
}

// handleRuleSetUpdate 立即更新规则集（可传入临时自定义地址）。
func (a *App) handleRuleSetUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL    string `json:"url"`
		Preset string `json:"preset"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	// 显式传 preset 时按该预设的镜像列表下载（保留回退）
	var meta *RuleSetMeta
	var err error
	if body.URL == "" && body.Preset != "" {
		if p := findRulePreset(body.Preset); p != nil {
			meta, err = a.updateRuleSetWith(p.URLs)
		} else {
			writeErr(w, 400, "未知的规则来源预设："+body.Preset)
			return
		}
	} else {
		meta, err = a.UpdateRuleSet(body.URL)
	}
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	if a.engine.Running() {
		_ = a.appReload()
	}
	writeJSON(w, 200, map[string]any{"ok": true, "meta": meta})
}

// handleStorage 返回数据目录占用明细。
func (a *App) handleStorage(w http.ResponseWriter, r *http.Request) {
	rep := a.storageReport()
	writeJSON(w, 200, map[string]any{"ok": true, "report": rep})
}

// handleStorageClean 立即清理无用文件。
func (a *App) handleStorageClean(w http.ResponseWriter, r *http.Request) {
	freed, steps := a.CleanupStorage()
	writeJSON(w, 200, map[string]any{"ok": true, "freed": freed, "steps": steps})
}
