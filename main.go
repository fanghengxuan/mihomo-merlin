package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const panelVersion = "1.7.2"

// panelPort 管理面板监听端口（插件安装脚本与 ASP 都按此端口访问）
const panelPort = 9380

//go:embed web
var webAssets embed.FS

type App struct {
	// 后台规则下载的等待组：方便测试等到异步任务真正结束，避免和临时目录清理赛跑
	bgTasks   sync.WaitGroup
	netInfo   netInfoCache
	reach     reachCache
	mu        sync.RWMutex
	dataDir   string
	settings  Settings
	subs      []Sub
	startedAt time.Time

	sessionMu sync.Mutex
	session   string // 登录成功后下发的 cookie 值；密码为空时不启用鉴权

	mihomoPath string
	engine     *Engine
	clash      *ClashClient
	hub        *Hub

	subMu     sync.Mutex // 串行化订阅的网络刷新
	kernelMu  sync.Mutex // 串行化内核替换/回滚
	samples   *sampler
	logRing   *ringBuffer
	iptablesE string

	// IP 质量探测缓存（120 秒）
	ipqMu   sync.Mutex
	ipqAt   time.Time
	ipqData *IPQuality

	// 定时重启的进度（持久化在 dataDir/restart.json）
	restartMu       sync.Mutex
	lastRestartAt   int64
	lastDailyDate   string
	restartCooldown time.Time
}

func main() {
	listen := flag.String("listen", envOr("MM_LISTEN", ":9380"), "监听地址，如 :9380")
	dataDir := flag.String("data", envOr("MM_DATA_DIR", ""), "数据目录")
	showVer := flag.Bool("v", false, "显示版本号")
	flag.Parse()

	if *showVer {
		fmt.Printf("mihomo-merlin panel %s (%s/%s)\n", panelVersion, runtime.GOOS, runtime.GOARCH)
		return
	}

	app := &App{startedAt: time.Now()}
	app.dataDir = resolveDataDir(*dataDir)
	app.mihomoPath = resolveMihomo()
	app.logRing = newRingBuffer(400)

	if err := os.MkdirAll(app.dataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	for _, d := range []string{"providers", "rules", "logs"} {
		_ = os.MkdirAll(filepath.Join(app.dataDir, d), 0o755)
	}

	app.loadSettings()
	app.loadRestartState()
	app.loadSubs()
	app.clash = NewClashClient(fmt.Sprintf("127.0.0.1:%d", app.settings.APIPort), app.settings.ClashSecret)
	app.engine = NewEngine(app)
	app.hub = NewHub(app)
	app.samples = newSampler(app)
	app.ensureRuleSet()

	if err := app.engine.Prepare(); err != nil {
		log.Printf("引擎准备失败: %v", err)
	}

	// 自启：面板被 init.d 拉起时，若引擎此前处于启用状态则自动恢复
	if app.settings.AutoStart && engineWanted(app.dataDir) {
		go func() {
			time.Sleep(2 * time.Second)
			if err := app.engine.Start(); err != nil {
				log.Printf("自动启动引擎失败: %v", err)
			}
		}()
	}

	app.samples.Start()
	app.hub.Start()
	go app.ruleScheduler()
	go app.restartScheduler()
	go app.storageJanitor()
	app.CleanupStorage()

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	sub, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatalf("静态资源加载失败: %v", err)
	}
	// 静态资源：index 不缓存，其余每次校验（避免面板升级后前端仍旧缓存）
	staticFiles := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		staticFiles.ServeHTTP(w, r)
	}))

	srv := &http.Server{
		Addr:              *listen,
		Handler:           app.withAuth(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("mihomo-merlin panel %s 监听 %s，数据目录 %s", panelVersion, *listen, app.dataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务退出: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Print("收到退出信号，停止引擎…")
	app.engine.Stop()
	_ = app.engine.ClearIptables()
}

func resolveDataDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("MM_DATA_DIR"); v != "" {
		return v
	}
	if fi, err := os.Stat("/koolshare"); err == nil && fi.IsDir() {
		return "/koolshare/mihomo"
	}
	return "./data"
}

func resolveMihomo() string {
	if v := os.Getenv("MM_MIHOMO"); v != "" {
		return v
	}
	candidates := []string{
		"/koolshare/bin/mihomo",
		"./mihomo",
		"/opt/bin/mihomo",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return "mihomo"
}

// engineWanted 读取引擎启用标记（安装/启动时写入）。
func engineWanted(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "enabled"))
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

func setEngineWanted(dir string, on bool) {
	p := filepath.Join(dir, "enabled")
	if on {
		_ = os.WriteFile(p, []byte("1"), 0o644)
		return
	}
	_ = os.Remove(p)
}

// withAuth 面板密码鉴权；密码为空时全部放行（仅建议在内网使用）。
func (a *App) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		pwd := a.settings.PanelPassword
		a.mu.RUnlock()
		if pwd == "" {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if p == "/api/login" || p == "/api/status" || !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie("mmp_token"); err == nil {
			a.sessionMu.Lock()
			ok := c.Value != "" && c.Value == a.session
			a.sessionMu.Unlock()
			if ok {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeErr(w, http.StatusUnauthorized, "未登录")
	})
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("GET /api/settings", a.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", a.handlePutSettings)

	mux.HandleFunc("POST /api/engine/start", a.handleEngineStart)
	mux.HandleFunc("POST /api/engine/stop", a.handleEngineStop)
	mux.HandleFunc("POST /api/engine/restart", a.handleEngineRestart)
	mux.HandleFunc("POST /api/engine/reload", a.handleEngineReload)
	mux.HandleFunc("POST /api/config/reload", a.handleEngineReload)

	mux.HandleFunc("GET /api/subs", a.handleListSubs)
	mux.HandleFunc("POST /api/subs", a.handleCreateSub)
	mux.HandleFunc("GET /api/subs/{id}", a.handleGetSub)
	mux.HandleFunc("PUT /api/subs/{id}", a.handleUpdateSub)
	mux.HandleFunc("DELETE /api/subs/{id}", a.handleDeleteSub)
	mux.HandleFunc("POST /api/subs/{id}/refresh", a.handleRefreshSub)
	mux.HandleFunc("POST /api/subs/refresh", a.handleRefreshAll)
	mux.HandleFunc("POST /api/subs/import", a.handleImportSub)
	mux.HandleFunc("GET /api/subs/{id}/raw", a.handleSubRaw)

	mux.HandleFunc("GET /api/nodes", a.handleNodes)
	mux.HandleFunc("PUT /api/select", a.handleSelect)
	mux.HandleFunc("GET /api/delay", a.handleDelayOne)
	mux.HandleFunc("POST /api/delay", a.handleDelayBatch)

	mux.HandleFunc("GET /api/kernel", a.handleKernelInfo)
	mux.HandleFunc("POST /api/kernel/upload", a.handleKernelUpload)
	mux.HandleFunc("POST /api/kernel/rollback", a.handleKernelRollback)

	mux.HandleFunc("GET /api/rules", a.handleRules)
	mux.HandleFunc("GET /api/netinfo", a.handleNetInfo)
	mux.HandleFunc("GET /api/kernel/check", a.handleKernelCheck)
	mux.HandleFunc("POST /api/kernel/update", a.handleKernelUpdate)
	mux.HandleFunc("GET /api/backup", a.handleBackup)
	mux.HandleFunc("POST /api/restore", a.handleRestore)
	mux.HandleFunc("GET /api/storage", a.handleStorage)
	mux.HandleFunc("POST /api/storage/clean", a.handleStorageClean)
	mux.HandleFunc("GET /api/rules/set", a.handleRuleSetInfo)
	mux.HandleFunc("POST /api/rules/update", a.handleRuleSetUpdate)
	mux.HandleFunc("GET /api/connections", a.handleConnections)
	mux.HandleFunc("GET /api/service", a.handleService)
	mux.HandleFunc("GET /api/intranet", a.handleIntranet)
	mux.HandleFunc("GET /api/ipquality", a.handleIPQuality)
	mux.HandleFunc("GET /api/reachability", a.handleReachability)
	mux.HandleFunc("DELETE /api/connections", a.handleCloseAllConnections)
	mux.HandleFunc("DELETE /api/connections/{id}", a.handleCloseConnection)
	mux.HandleFunc("GET /api/progress", a.handleProgress)
	mux.HandleFunc("GET /api/logs/stream", a.handleLogs)
	mux.HandleFunc("GET /api/traffic/stream", a.handleTraffic)
}
