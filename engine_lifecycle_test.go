package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKernel 写一个可执行的假内核：
//   - -t      配置校验直接成功
//   - -v      打印版本
//   - 其他    把启动次数追加到 startLog，然后一直运行到 SIGTERM
func fakeKernel(t *testing.T, dir string) (path string, startLog string) {
	t.Helper()
	path = filepath.Join(dir, "mihomo-fake")
	startLog = filepath.Join(dir, "starts.log")
	script := `#!/bin/sh
dir=""
args="$*"
while [ $# -gt 0 ]; do
  case "$1" in
    -t) exit 0 ;;
    -v) echo "fake-1.0"; exit 0 ;;
    -d) dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "started $args" >> "$dir/starts.log"
trap 'exit 0' TERM INT
while :; do sleep 1; done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("写假内核失败: %v", err)
	}
	return path, startLog
}

// fakeClashAPI 提供 waitReady 需要的 /version。
func fakeClashAPI(t *testing.T, a *App) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"version":"fake"}`))
			return
		}
		http.NotFound(w, r)
	}))
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("解析测试服务地址失败: %v", err)
	}
	host, portStr, _ := strings.Cut(u.Host, ":")
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	a.settings.APIPort = port
	a.clash = NewClashClient(host+":"+portStr, a.settings.ClashSecret)
	t.Cleanup(srv.Close)
	return srv
}

func setupFakeEngine(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("MM_NO_IPTABLES", "1")
	a := newBackendTestApp(t)
	path, startLog := fakeKernel(t, a.dataDir)
	a.mihomoPath = path
	fakeClashAPI(t, a)
	t.Cleanup(func() { _ = a.engine.Stop() })
	return a, startLog
}

// 并发 Start 只能真正拉起一个进程：配置校验期间不能只靠 running 标志互斥。
func TestEngineConcurrentStartSingleProcess(t *testing.T) {
	a, startLog := setupFakeEngine(t)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- a.engine.Start()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发启动失败: %v", err)
		}
	}

	// waitReady 只等 REST API（测试里是假服务），进程可能还没执行到写日志这一步。
	// 给子进程一点时间，再统计真实启动次数。
	var data []byte
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err = os.ReadFile(startLog)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("读取启动日志失败: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // 给可能的重复进程留下写日志的时间
	data, _ = os.ReadFile(startLog)
	if n := strings.Count(string(data), "started"); n != 1 {
		t.Fatalf("并发 Start 拉起了 %d 个进程，期望 1 个", n)
	}
}

// Stop 后再 Start 必须恢复运行状态，且旧代际不会清掉新实例的规则。
func TestEngineStartStopStart(t *testing.T) {
	a, _ := setupFakeEngine(t)

	if err := a.engine.Start(); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	if !a.engine.Running() || !a.engine.Applied() {
		t.Fatal("启动后应为运行中且已应用规则")
	}

	if err := a.engine.Stop(); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	if a.engine.Running() || a.engine.Applied() {
		t.Fatal("停止后不应运行、规则应已清理")
	}

	if err := a.engine.Start(); err != nil {
		t.Fatalf("再次启动失败: %v", err)
	}
	if !a.engine.Running() || !a.engine.Applied() {
		t.Fatal("再次启动后应为运行中且已应用规则")
	}
}
