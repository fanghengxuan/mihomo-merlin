package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 直连失败 → 自动换镜像；返回的 usedURL 要能看出走了哪个
func TestGetWithFallbackUsesMirror(t *testing.T) {
	orig := kernelMirrors
	defer func() { kernelMirrors = orig }()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 镜像收到的是「前缀 + 原始地址」形式，这里回一层内容即可
		_, _ = w.Write([]byte("FROM-MIRROR"))
	}))
	defer mirror.Close()

	// 直连指向一个必然失败的地址（端口 1），镜像指向上面这个服务器
	kernelMirrors = []string{mirror.URL + "/"}

	a := newTestApp(t)
	b, used, err := a.getWithFallback("http://127.0.0.1:1/atom", 1<<20, 3*time.Second)
	if err != nil {
		t.Fatalf("应回退到镜像，实际报错：%v", err)
	}
	if string(b) != "FROM-MIRROR" {
		t.Fatalf("内容不对：%q", string(b))
	}
	if !strings.HasPrefix(used, mirror.URL) {
		t.Fatalf("usedURL 应是镜像地址，实际 %s", used)
	}
	t.Logf("✓ 直连失败后走镜像：%s", used)
}

// 所有地址都失败时要返回错误，不能假装成功
func TestGetWithFallbackAllFail(t *testing.T) {
	orig := kernelMirrors
	defer func() { kernelMirrors = orig }()
	kernelMirrors = nil

	a := newTestApp(t)
	_, _, err := a.getWithFallback("http://127.0.0.1:1/x", 1024, 2*time.Second)
	if err == nil {
		t.Fatal("全部失败时应返回错误")
	}
	t.Logf("✓ 全部失败返回错误：%v", err)
}

// 镜像候选顺序：直连在最前，镜像前缀拼接正确
func TestFetchCandidatesOrder(t *testing.T) {
	orig := kernelMirrors
	defer func() { kernelMirrors = orig }()
	kernelMirrors = []string{"https://m1/", "https://m2/"}

	raw := "https://github.com/o/r/releases/download/t/a.bin"
	c := fetchCandidates(raw)
	if len(c) != 3 {
		t.Fatalf("应有 3 个候选，实际 %d", len(c))
	}
	if c[0] != raw {
		t.Errorf("第一个应是直连：%s", c[0])
	}
	if c[1] != "https://m1/https://github.com/o/r/releases/download/t/a.bin" {
		t.Errorf("镜像拼接不对：%s", c[1])
	}
	t.Logf("✓ 候选顺序：直连 → 镜像")
}
