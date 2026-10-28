package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCIDRList(t *testing.T) {
	in := `# 注释行
// 另一种注释
1.2.3.0/24
5.6.7.8
  9.9.9.9/32   # 行尾注释
2001:db8::/32
not-an-ip
1.2.3.0/24
`
	list, err := parseCIDRList([]byte(in))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := []string{"1.2.3.0/24", "5.6.7.8/32", "9.9.9.9/32", "2001:db8::/32"}
	if len(list) != len(want) {
		t.Fatalf("期望 %d 条，实际 %d：%v", len(want), len(list), list)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("第 %d 条应为 %s，实际 %s", i, want[i], list[i])
		}
	}
}

// 规则订阅：从给定地址下载、校验、落盘，并把元信息写入 meta.json
func TestRuleSetUpdateFromURL(t *testing.T) {
	a := newTestApp(t)

	// 造 1000 条看起来合理的 CIDR
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("10.")
		sb.WriteString(itoa(i / 256))
		sb.WriteString(".")
		sb.WriteString(itoa(i % 256))
		sb.WriteString(".0/24\n")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# test\n" + sb.String()))
	}))
	defer srv.Close()

	meta, err := a.UpdateRuleSet(srv.URL)
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if meta.Count < 900 {
		t.Fatalf("条数不合理: %d", meta.Count)
	}
	if !fileExists(a.ruleSetPath()) {
		t.Fatal("规则文件没有落盘")
	}
	got := a.loadRuleMeta()
	if got.Source != srv.URL || got.Count != meta.Count {
		t.Fatalf("meta 未正确保存: %+v", got)
	}
}

// 下载到错误页（内容不是 CIDR）时必须拒绝，不能覆盖原有规则
func TestRuleSetRejectsGarbage(t *testing.T) {
	a := newTestApp(t)
	orig := "1.2.3.0/24\n"
	if err := os.MkdirAll(filepath.Dir(a.ruleSetPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.ruleSetPath(), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>404 not found</html>"))
	}))
	defer srv.Close()

	if _, err := a.UpdateRuleSet(srv.URL); err == nil {
		t.Fatal("内容非法时应当报错")
	}
	if got := string(readFileOrNil(a.ruleSetPath())); got != orig {
		t.Fatalf("非法内容不应覆盖原规则，现内容：%q", got)
	}
}

// 来源优先级：显式 url > 显式 preset > 自定义 > 默认预设
func TestResolveRuleCandidates(t *testing.T) {
	if got := resolveRuleCandidates("https://x/custom.txt", "17mon", "https://y/custom.txt"); len(got) != 1 || got[0] != "https://x/custom.txt" {
		t.Errorf("显式 url 应优先，实际 %v", got)
	}
	if got := resolveRuleCandidates("", "17mon", ""); len(got) != 2 || !strings.Contains(got[0], "17mon") {
		t.Errorf("应使用 17mon 预设的镜像，实际 %v", got)
	}
	if got := resolveRuleCandidates("", "custom", "https://z/mine.txt"); len(got) != 1 || got[0] != "https://z/mine.txt" {
		t.Errorf("自定义应生效，实际 %v", got)
	}
	if got := resolveRuleCandidates("", "", ""); len(got) == 0 || !strings.Contains(got[0], "chnroutes2") {
		t.Errorf("默认应回落到 chnroutes2，实际 %v", got)
	}
	t.Logf("✓ 来源优先级正确（默认 %s）", got0Of(resolveRuleCandidates("", "", "")))
}

func got0Of(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

// 预设来源能真正用起来：把预设地址换成测试服务器
func TestPresetUpdateUsesMirrors(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 800; i++ {
		sb.WriteString("10.")
		sb.WriteString(itoa(i / 256))
		sb.WriteString(".")
		sb.WriteString(itoa(i % 256))
		sb.WriteString(".0/24\n")
	}
	// 第一个镜像故意失败，第二个成功，验证回退
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer good.Close()

	saved := ruleSourcePresets[0].URLs
	ruleSourcePresets[0].URLs = []string{bad.URL, good.URL}
	defer func() { ruleSourcePresets[0].URLs = saved }()

	a := newTestApp(t)
	meta, err := a.updateRuleSetWith(findRulePreset("chnroutes2").URLs)
	if err != nil {
		t.Fatalf("预设更新失败: %v", err)
	}
	if meta.Source != good.URL {
		t.Errorf("应在镜像间回退到可用地址，实际 %s", meta.Source)
	}
	if meta.Count < 700 {
		t.Errorf("条数偏少: %d", meta.Count)
	}
	t.Logf("✓ 预设镜像回退生效：%s（%d 条）", meta.Source, meta.Count)
}

// 换规则来源时应当立刻重新下载，而不是只保存设置
func TestRuleSourceChangeTriggersFetch(t *testing.T) {
	hits := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hits <- struct{}{}:
		default:
		}
		var b strings.Builder
		for i := 0; i < 600; i++ {
			fmt.Fprintf(&b, "10.%d.%d.0/24\n", i/256, i%256)
		}
		_, _ = io.WriteString(w, b.String())
	}))
	defer srv.Close()

	a := newTestApp(t)
	// 初始为默认预设，PUT 改成自定义来源 → 应检测到变化并重新下载
	body := `{"ruleSourcePreset":"custom","ruleSourceURL":"` + srv.URL + `"}`
	req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handlePutSettings(rec, req)
	if rec.Code != 200 {
		t.Fatalf("保存设置应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}

	select {
	case <-hits:
		t.Log("✓ 切换规则来源后立即重新下载")
	case <-time.After(5 * time.Second):
		t.Fatal("切换规则来源后没有触发下载")
	}
}
