package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Engine 负责 mihomo 进程生命周期与 iptables 透明代理规则。
type Engine struct {
	app *App

	mu        sync.Mutex
	cmd       *exec.Cmd
	running   bool
	startedAt time.Time
	lastErr   string

	applied   bool
	lastApply int64

	watchOnce sync.Once
	stopWatch chan struct{}

	// 自动恢复（内核意外退出后拉起）
	recoverMu       sync.Mutex
	recoverAttempts int
	recoverNextAt   time.Time
	recoverLastErr  string
	recoverState    recoveryState
}

func NewEngine(app *App) *Engine {
	return &Engine{app: app, stopWatch: make(chan struct{})}
}

func (e *Engine) Prepare() error {
	for _, d := range []string{"providers", "rules", "logs"} {
		_ = os.MkdirAll(filepath.Join(e.app.dataDir, d), 0o755)
	}
	if !fileExists(e.app.mihomoPath) {
		return fmt.Errorf("找不到 mihomo 内核：%s", e.app.mihomoPath)
	}
	return nil
}

func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *Engine) PID() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running || e.cmd == nil || e.cmd.Process == nil {
		return 0
	}
	return e.cmd.Process.Pid
}

func (e *Engine) Uptime() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return 0
	}
	return int64(time.Since(e.startedAt).Seconds())
}

func (e *Engine) Applied() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.applied
}

func (e *Engine) LastApply() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastApply
}

func (e *Engine) rotateLog() {
	p := e.app.logPath()
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		_ = os.Remove(p + ".1")
		_ = os.Rename(p, p+".1")
	}
}

// Start 校验配置后启动内核，并应用透明代理规则。
func (e *Engine) Start() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	if err := e.app.WriteConfig(); err != nil {
		return err
	}
	if !fileExists(e.app.mihomoPath) {
		return fmt.Errorf("找不到 mihomo 内核：%s", e.app.mihomoPath)
	}

	_ = os.MkdirAll(filepath.Join(e.app.dataDir, "logs"), 0o755)

	// 配置校验：把 mihomo 的报错原文回传，避免“启动后秒退”无从排查
	if err := e.validateConfig(); err != nil {
		return err
	}

	e.rotateLog()
	lf, err := os.OpenFile(e.app.logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %v", err)
	}

	cmd := exec.Command(e.app.mihomoPath, "-d", e.app.dataDir, "-f", e.app.configPath())
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.Dir = e.app.dataDir
	cmd.Env = append(os.Environ(), "SAFE_PATHS=1")

	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("启动内核失败: %v", err)
	}

	e.mu.Lock()
	e.cmd = cmd
	e.running = true
	e.startedAt = time.Now()
	e.lastErr = ""
	e.mu.Unlock()

	a := e.app
	setEngineWanted(a.dataDir, true)
	a.addLog("info", fmt.Sprintf("mihomo 已启动（pid %d）", cmd.Process.Pid))

	go func() {
		err := cmd.Wait()
		lf.Close()
		e.mu.Lock()
		e.running = false
		e.cmd = nil
		e.mu.Unlock()
		_ = e.ClearIptables()
		if err != nil {
			a.addLog("error", fmt.Sprintf("mihomo 进程退出：%v", err))
			e.setErr(fmt.Sprintf("进程退出：%v", err))
		} else {
			a.addLog("warning", "mihomo 进程已退出")
		}
	}()

	if err := e.waitReady(20 * time.Second); err != nil {
		_ = e.Stop()
		return err
	}

	if err := e.ApplyIptables(); err != nil {
		a.addLog("warning", "应用转发规则失败："+err.Error())
	}

	e.startWatchdog()
	return nil
}

// validateConfig 调 `mihomo -t` 校验配置。配置内容与内核都没变时复用上次结果，
// 避免每次启动都在路由器上重跑一遍（对 UPX 压缩过的内核尤其省时间）。
func (e *Engine) validateConfig() error {
	cfgData, err := os.ReadFile(e.app.configPath())
	if err != nil {
		return fmt.Errorf("读取配置失败：%v", err)
	}
	binInfo, err := os.Stat(e.app.mihomoPath)
	if err != nil {
		return fmt.Errorf("读取内核失败：%v", err)
	}
	sum := sha256.Sum256(cfgData)
	stamp := fmt.Sprintf("%x|%d|%d", sum, binInfo.Size(), binInfo.ModTime().UnixNano())
	stampPath := filepath.Join(e.app.dataDir, ".config.stamp")
	if b, err := os.ReadFile(stampPath); err == nil && strings.TrimSpace(string(b)) == stamp {
		return nil
	}

	out, err := run(90*time.Second, e.app.mihomoPath, "-t", "-d", e.app.dataDir, "-f", e.app.configPath())
	if err != nil {
		e.setErr("配置校验失败")
		return fmt.Errorf("配置校验失败：%s", tail(out, 700))
	}
	_ = os.WriteFile(stampPath, []byte(stamp), 0o600)
	return nil
}

func (e *Engine) setErr(s string) {
	e.mu.Lock()
	e.lastErr = s
	e.mu.Unlock()
}

func (e *Engine) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !e.Running() {
			return errors.New("内核启动后立即退出，请查看日志")
		}
		if _, err := e.app.clash.Version(); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("内核启动超时（RESTful API 未就绪）")
}

// Stop 停止内核并清理规则。
func (e *Engine) Stop() error {
	e.mu.Lock()
	cmd := e.cmd
	running := e.running
	e.mu.Unlock()

	_ = e.ClearIptables()
	setEngineWanted(e.app.dataDir, false)

	if !running || cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 60; i++ {
			if !e.Running() {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(7 * time.Second):
	}
	if e.Running() {
		_ = cmd.Process.Kill()
		time.Sleep(500 * time.Millisecond)
	}
	e.app.addLog("info", "mihomo 已停止")
	return nil
}

// Restart 重启内核。
func (e *Engine) Restart() error {
	// Restart 是「内部重启」，不能改变用户意图：Stop 会清掉 enabled 标记，
	// 一旦随后的 Start 失败（配置异常/内核起不来），标记就没了，
	// 面板重启和看门狗都不会再拉起它 —— 表现为「插件不知道什么时候自己停了」。
	wanted := engineWanted(e.app.dataDir)
	if err := e.Stop(); err != nil {
		if wanted {
			setEngineWanted(e.app.dataDir, true)
		}
		return err
	}
	time.Sleep(500 * time.Millisecond)
	if err := e.Start(); err != nil {
		if wanted {
			setEngineWanted(e.app.dataDir, true) // 保持期望状态，交给看门狗继续重试
			e.app.addLog("error", "内核重启失败，已保留运行意图，稍后自动重试："+err.Error())
		}
		return err
	}
	return nil
}

// Reload 重新生成配置并热重载；失败时退回重启。
func (e *Engine) Reload() error {
	if !e.Running() {
		return nil
	}
	if err := e.app.WriteConfig(); err != nil {
		return err
	}
	if err := e.app.clash.ReloadConfig(e.app.configPath()); err != nil {
		e.app.addLog("warning", "热重载失败，改为重启内核："+err.Error())
		return e.Restart()
	}
	if err := e.ApplyIptables(); err != nil {
		e.app.addLog("warning", "重载后刷新转发规则失败："+err.Error())
	}
	e.app.addLog("info", "配置已热重载")
	return nil
}

// recoverBackoff 是自动恢复的退避序列（第 N 次失败后等多久再试）。
var recoverBackoff = []time.Duration{
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute,
}

// recoveryState 记录自动恢复的尝试情况，供状态接口与界面展示。
type recoveryState struct {
	Attempts  int    `json:"attempts"`
	LastErr   string `json:"lastErr"`
	NextAt    int64  `json:"nextAt"`
	Recovered int64  `json:"recovered"`
}

// maybeAutoRecover 在内核本该运行却不在运行时尝试拉起（带退避）。
func (e *Engine) maybeAutoRecover(now time.Time) {
	if e.Running() {
		return
	}
	if !engineWanted(e.app.dataDir) {
		return // 用户主动停止过
	}
	e.app.mu.RLock()
	autoStart := e.app.settings.AutoStart
	e.app.mu.RUnlock()
	if !autoStart {
		return // 明确关了开机自启，交给用户手动处理
	}

	e.recoverMu.Lock()
	if now.Before(e.recoverNextAt) {
		e.recoverMu.Unlock()
		return
	}
	e.recoverAttempts++
	attempt := e.recoverAttempts
	e.recoverMu.Unlock()

	e.app.addLog("warning", fmt.Sprintf("检测到内核未运行，尝试自动恢复（第 %d 次）", attempt))
	err := e.Start()

	e.recoverMu.Lock()
	if err != nil {
		e.recoverLastErr = err.Error()
		idx := attempt - 1
		if idx >= len(recoverBackoff) {
			idx = len(recoverBackoff) - 1
		}
		e.recoverNextAt = now.Add(recoverBackoff[idx])
		e.recoverState = recoveryState{Attempts: attempt, LastErr: e.recoverLastErr, NextAt: e.recoverNextAt.Unix()}
		e.recoverMu.Unlock()
		e.app.addLog("error", "自动恢复失败："+err.Error())
		return
	}
	e.recoverAttempts = 0
	e.recoverNextAt = time.Time{}
	e.recoverState = recoveryState{Attempts: 0, Recovered: now.Unix()}
	e.recoverMu.Unlock()
	e.app.addLog("info", "内核已自动恢复运行")
}

// RecoverState 返回自动恢复的可展示状态。
func (e *Engine) RecoverState() recoveryState {
	e.recoverMu.Lock()
	defer e.recoverMu.Unlock()
	return e.recoverState
}

func (e *Engine) startWatchdog() {
	e.watchOnce.Do(func() {
		go func() {
			t := time.NewTicker(45 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-e.stopWatch:
					return
				case <-t.C:
					if e.Running() && !e.Applied() {
						if err := e.ApplyIptables(); err != nil {
							e.app.addLog("warning", "转发规则被重置，恢复失败："+err.Error())
						} else {
							e.app.addLog("info", "转发规则已被重置，已自动恢复")
						}
					}
					// 内核意外退出（崩溃 / 定时重启失败 / 被 OOM 杀掉）时自动拉起：
					// 只有当「用户希望它运行」（enabled 标记存在）且开启了开机自启才动作，
					// 手动点过停止的不会被复活。
					e.maybeAutoRecover(time.Now())
				}
			}
		}()
	})
}

// ---------- 定时重启 ----------

// restartEveryCheck 是定时重启的检查周期。
const restartEveryCheck = 60 * time.Second

// restartFailedCooldown 是重启失败后的冷却时间，避免每分钟重试刷屏。
const restartFailedCooldown = 10 * time.Minute

// restartAtPattern 是「每天固定时刻」的合法格式：HH:MM。
var restartAtPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// restartState 持久化到 dataDir/restart.json，供重启调度与面板展示使用。
type restartState struct {
	LastRestartAt int64  `json:"lastRestartAt"` // 上次成功重启的时间戳（0=没有）
	LastDailyDate string `json:"lastDailyDate"` // 固定时刻重启最近一次执行的日期（YYYY-MM-DD）
}

func (a *App) restartStatePath() string { return filepath.Join(a.dataDir, "restart.json") }

// parseHHMM 解析 "HH:MM"，返回时、分。
func parseHHMM(v string) (int, int, bool) {
	t := strings.TrimSpace(v)
	if !restartAtPattern.MatchString(t) {
		return 0, 0, false
	}
	parts := strings.SplitN(t, ":", 2)
	hh, err1 := atoiSafe(parts[0])
	mm, err2 := atoiSafe(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return hh, mm, true
}

// loadRestartState 读取重启进度；文件不存在或损坏时保持零值，不影响启动。
func (a *App) loadRestartState() {
	b, err := os.ReadFile(a.restartStatePath())
	if err != nil {
		return
	}
	var st restartState
	if json.Unmarshal(b, &st) != nil {
		return
	}
	a.restartMu.Lock()
	a.lastRestartAt = st.LastRestartAt
	a.lastDailyDate = st.LastDailyDate
	a.restartMu.Unlock()
}

// lastRestart 返回上次成功重启的时间戳（0 表示没有记录）。
func (a *App) lastRestart() int64 {
	a.restartMu.Lock()
	defer a.restartMu.Unlock()
	return a.lastRestartAt
}

// noteRestart 记录一次由用户触发的重启（调度会据此顺延周期计时）。
func (a *App) noteRestart() {
	a.restartMu.Lock()
	a.lastRestartAt = time.Now().Unix()
	st := restartState{LastRestartAt: a.lastRestartAt, LastDailyDate: a.lastDailyDate}
	a.restartMu.Unlock()
	_ = writeJSONAtomic(a.restartStatePath(), st)
}

// nextRestartAt 估算下一次定时重启的时间戳；算不出时返回 0。
func (a *App) nextRestartAt(now time.Time) int64 {
	a.mu.RLock()
	s := a.settings
	a.mu.RUnlock()

	var next int64
	if s.RestartAt != "" {
		if hh, mm, ok := parseHHMM(s.RestartAt); ok {
			t := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
			for !t.After(now) {
				t = t.AddDate(0, 0, 1)
			}
			next = t.Unix()
		}
	}
	if s.AutoRestart && s.RestartEveryHours > 0 {
		last := a.lastRestart()
		if last == 0 {
			last = a.startedAt.Unix()
		}
		if at := last + int64(s.RestartEveryHours)*3600; next == 0 || at < next {
			next = at
		}
	}
	return next
}

// restartScheduler 每 60 秒检查一次定时重启条件。
func (a *App) restartScheduler() {
	t := time.NewTicker(restartEveryCheck)
	defer t.Stop()
	for range t.C {
		a.checkScheduledRestart(time.Now())
	}
}

// checkScheduledRestart 判断此刻是否该重启内核；供调度器与单元测试复用。
func (a *App) checkScheduledRestart(now time.Time) {
	a.mu.RLock()
	s := a.settings
	a.mu.RUnlock()
	if !s.AutoRestart && s.RestartAt == "" {
		return
	}
	if !a.engine.Running() {
		return
	}

	a.restartMu.Lock()
	cooldown := a.restartCooldown
	a.restartMu.Unlock()
	if now.Before(cooldown) {
		return
	}

	// 1) 每天固定时刻：当天到点且今天还没执行过
	today := now.Format("2006-01-02")
	if s.RestartAt != "" {
		a.restartMu.Lock()
		doneToday := a.lastDailyDate == today
		a.restartMu.Unlock()
		if hh, mm, ok := parseHHMM(s.RestartAt); ok && !doneToday {
			if now.Hour()*60+now.Minute() >= hh*60+mm {
				a.performScheduledRestart("每天 "+s.RestartAt+" 定时重启", today)
				return
			}
		}
	}

	// 2) 周期重启：距上次重启（没有记录时从面板启动算起）超过 N 小时
	if s.AutoRestart && s.RestartEveryHours > 0 {
		last := a.lastRestart()
		if last == 0 {
			last = a.startedAt.Unix()
		}
		if now.Unix()-last >= int64(s.RestartEveryHours)*3600 {
			a.performScheduledRestart("每 "+itoa(s.RestartEveryHours)+" 小时定时重启", "")
		}
	}
}

// performScheduledRestart 执行一次定时重启并落盘进度；失败只记日志，不 panic。
func (a *App) performScheduledRestart(reason, dailyDate string) {
	a.addLog("info", "定时重启内核："+reason)
	err := a.engine.Restart()

	a.restartMu.Lock()
	if dailyDate != "" {
		a.lastDailyDate = dailyDate
	}
	if err != nil {
		a.restartCooldown = time.Now().Add(restartFailedCooldown)
	} else {
		a.lastRestartAt = time.Now().Unix()
	}
	st := restartState{LastRestartAt: a.lastRestartAt, LastDailyDate: a.lastDailyDate}
	a.restartMu.Unlock()

	if werr := writeJSONAtomic(a.restartStatePath(), st); werr != nil {
		a.addLog("warn", "记录定时重启状态失败："+werr.Error())
	}
	if err != nil {
		a.addLog("error", "定时重启失败："+err.Error())
		return
	}
	a.addLog("info", "定时重启完成")
}

// atoiSafe 解析十进制非负整数（用于 HH:MM 拆分）。
func atoiSafe(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("空数字")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("非数字")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// ---------- iptables ----------

func iptablesBin() string {
	if p, err := exec.LookPath("iptables"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/sbin/iptables", "/sbin/iptables", "/usr/bin/iptables"} {
		if fileExists(p) {
			return p
		}
	}
	return "iptables"
}

func (e *Engine) ipt(args ...string) error {
	out, err := run(8*time.Second, iptablesBin(), args...)
	if err != nil {
		return fmt.Errorf("iptables %s: %s", strings.Join(args, " "), tail(out, 200))
	}
	return nil
}

var (
	reServerJSON = regexp.MustCompile(`"server"\s*:\s*"([^"]+)"`)
	reServerYAML = regexp.MustCompile(`(?m)^\s*-?\s*server\s*:\s*"?([^\s",}]+)`)
)

// proxyServerIPs 扫描 provider 文件里的服务器地址并尽量解析成 IP，
// 用于在规则里放行「到代理服务器本身」的流量，避免回环。
func (e *Engine) proxyServerIPs() []string {
	seen := map[string]bool{}
	var out []string
	entries, err := os.ReadDir(filepath.Join(e.app.dataDir, "providers"))
	if err != nil {
		return nil
	}
	var hosts []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(e.app.dataDir, "providers", ent.Name()))
		if err != nil {
			continue
		}
		text := string(b)
		for _, m := range reServerJSON.FindAllStringSubmatch(text, -1) {
			hosts = append(hosts, m[1])
		}
		for _, m := range reServerYAML.FindAllStringSubmatch(text, -1) {
			hosts = append(hosts, m[1])
		}
	}
	for _, h := range hosts {
		h = strings.Trim(h, `"'`)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if ip := net.ParseIP(h); ip != nil {
			out = append(out, h)
			continue
		}
		ips, err := net.LookupHost(h)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if net.ParseIP(ip) != nil && !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// ApplyIptables 建立 nat 表转发链，将 LAN 的 TCP 流量重定向到 redir 端口。
func (e *Engine) ApplyIptables() error {
	if os.Getenv("MM_NO_IPTABLES") == "1" {
		e.mu.Lock()
		e.applied = true
		e.lastApply = time.Now().Unix()
		e.mu.Unlock()
		return nil
	}

	s := e.app.settings
	lanIf := s.LanIf
	if lanIf == "" {
		lanIf = "br0"
	}

	_ = e.ipt("-t", "nat", "-N", "MIHOMO")
	_ = e.ipt("-t", "nat", "-F", "MIHOMO")

	bypass := []string{
		"0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4",
	}
	for _, ip := range e.proxyServerIPs() {
		bypass = append(bypass, ip+"/32")
	}
	for _, cidr := range bypass {
		if err := e.ipt("-t", "nat", "-A", "MIHOMO", "-d", cidr, "-j", "RETURN"); err != nil {
			return err
		}
	}
	if err := e.ipt("-t", "nat", "-A", "MIHOMO", "-p", "tcp", "-j", "REDIRECT", "--to-ports", itoa(s.RedirPort)); err != nil {
		return err
	}

	if err := e.ipt("-t", "nat", "-C", "PREROUTING", "-i", lanIf, "-j", "MIHOMO"); err != nil {
		if err := e.ipt("-t", "nat", "-I", "PREROUTING", "1", "-i", lanIf, "-j", "MIHOMO"); err != nil {
			return err
		}
	}

	// TCP MSS 钳制：内核转发的 SYN 统一把 MSS 压到路径 MTU，
	// 否则客户端仍按 1500 发大包，遇到 MTU 更小的代理出口时会出现「下载正常、上传到几 MB 就卡死/重置」。
	if err := e.ipt("-t", "mangle", "-C", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN",
		"-j", "TCPMSS", "--clamp-mss-to-pmtu"); err != nil {
		_ = e.ipt("-t", "mangle", "-A", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN",
			"-j", "TCPMSS", "--clamp-mss-to-pmtu")
	}

	// 阻断 QUIC，迫使浏览器/App 回落到 TCP（否则 UDP 会绕过 redir）
	if s.BlockQuic {
		if err := e.ipt("-C", "FORWARD", "-p", "udp", "--dport", "443", "-j", "REJECT"); err != nil {
			_ = e.ipt("-I", "FORWARD", "1", "-p", "udp", "--dport", "443", "-j", "REJECT")
		}
	} else {
		_ = e.ipt("-D", "FORWARD", "-p", "udp", "--dport", "443", "-j", "REJECT")
	}

	// 可选：代理路由器自身流量（experimental）
	if s.RouterSelf {
		_ = e.ipt("-t", "nat", "-N", "MIHOMO_OUT")
		_ = e.ipt("-t", "nat", "-F", "MIHOMO_OUT")
		for _, cidr := range bypass {
			_ = e.ipt("-t", "nat", "-A", "MIHOMO_OUT", "-d", cidr, "-j", "RETURN")
		}
		_ = e.ipt("-t", "nat", "-A", "MIHOMO_OUT", "-p", "tcp", "-j", "REDIRECT", "--to-ports", itoa(s.RedirPort))
		if err := e.ipt("-t", "nat", "-C", "OUTPUT", "-j", "MIHOMO_OUT"); err != nil {
			_ = e.ipt("-t", "nat", "-I", "OUTPUT", "1", "-j", "MIHOMO_OUT")
		}
	} else {
		for i := 0; i < 5; i++ {
			if err := e.ipt("-t", "nat", "-D", "OUTPUT", "-j", "MIHOMO_OUT"); err != nil {
				break
			}
		}
		_ = e.ipt("-t", "nat", "-F", "MIHOMO_OUT")
		_ = e.ipt("-t", "nat", "-X", "MIHOMO_OUT")
	}

	e.mu.Lock()
	e.applied = true
	e.lastApply = time.Now().Unix()
	e.mu.Unlock()
	return nil
}

// ClearIptables 移除全部本插件规则。
func (e *Engine) ClearIptables() error {
	if os.Getenv("MM_NO_IPTABLES") == "1" {
		e.mu.Lock()
		e.applied = false
		e.mu.Unlock()
		return nil
	}
	s := e.app.settings
	lanIf := s.LanIf
	if lanIf == "" {
		lanIf = "br0"
	}
	for i := 0; i < 5; i++ {
		if err := e.ipt("-t", "nat", "-D", "PREROUTING", "-i", lanIf, "-j", "MIHOMO"); err != nil {
			break
		}
	}
	for i := 0; i < 5; i++ {
		if err := e.ipt("-t", "nat", "-D", "OUTPUT", "-j", "MIHOMO_OUT"); err != nil {
			break
		}
	}
	_ = e.ipt("-t", "nat", "-F", "MIHOMO")
	_ = e.ipt("-t", "nat", "-X", "MIHOMO")
	_ = e.ipt("-t", "nat", "-F", "MIHOMO_OUT")
	_ = e.ipt("-t", "nat", "-X", "MIHOMO_OUT")
	_ = e.ipt("-D", "FORWARD", "-p", "udp", "--dport", "443", "-j", "REJECT")
	for i := 0; i < 3; i++ {
		if err := e.ipt("-t", "mangle", "-D", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN",
			"-j", "TCPMSS", "--clamp-mss-to-pmtu"); err != nil {
			break
		}
	}

	e.mu.Lock()
	e.applied = false
	e.mu.Unlock()
	return nil
}
