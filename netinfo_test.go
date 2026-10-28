package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeExitIPParsesAndClassifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","country":"美国","countryCode":"US",` +
			`"isp":"Example Hosting","as":"AS64500 Example","query":"1.2.3.4","hosting":true}`))
	}))
	defer srv.Close()

	old := ipAPIURL
	ipAPIURL = srv.URL
	defer func() { ipAPIURL = old }()

	a := newTestApp(t)
	info := a.probeExitIP(false)
	if info.Err != "" {
		t.Fatalf("探测失败: %s", info.Err)
	}
	if info.IP != "1.2.3.4" || info.CountryCode != "US" {
		t.Fatalf("解析结果不对: %+v", info)
	}
	if info.Kind != "机房/云" {
		t.Errorf("hosting=true 应判定为机房，实际 %q", info.Kind)
	}
	t.Logf("✓ 出口 IP：%s %s %s（%s）", info.IP, info.CountryCode, info.ISP, info.Kind)
}

func TestProbeExitIPFallsBack(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"fail","message":"quota"}`))
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"5.6.7.8","country":"JP","org":"AS123 Example"}`))
	}))
	defer good.Close()

	old1, old2 := ipAPIURL, ipinfoURL
	ipAPIURL, ipinfoURL = bad.URL, good.URL
	defer func() { ipAPIURL, ipinfoURL = old1, old2 }()

	a := newTestApp(t)
	info := a.probeExitIP(false)
	if info.IP != "5.6.7.8" || info.CountryCode != "JP" {
		t.Fatalf("备用源没有生效: %+v", info)
	}
	if info.Source != "ipinfo.io" {
		t.Errorf("应标注备用源，实际 %q", info.Source)
	}
	t.Logf("✓ 备用源生效：%s %s（%s）", info.IP, info.CountryCode, info.Source)
}

// ip-api 是质量推导的主来源：hosting→机房/一般，proxy→风险，普通→住宅/良好
func TestParseIPQualityFromIpAPI(t *testing.T) {
	cases := []struct {
		body  string
		typ   string
		level string
	}{
		{`{"status":"success","query":"1.2.3.4","hosting":true,"mobile":false,"proxy":false}`, "hosting", "normal"},
		{`{"status":"success","query":"5.6.7.8","hosting":false,"mobile":true,"proxy":false}`, "mobile", "good"},
		{`{"status":"success","query":"9.9.9.9","hosting":false,"mobile":false,"proxy":true}`, "residential", "risky"},
		{`{"status":"success","query":"8.8.8.8","hosting":false,"mobile":false,"proxy":false}`, "residential", "good"},
	}
	for _, c := range cases {
		q, err := parseIPQualityIpAPI([]byte(c.body))
		if err != nil {
			t.Fatalf("%s 解析失败: %v", c.body, err)
		}
		if q.Type != c.typ || q.Level != c.level {
			t.Errorf("%s：期望 %s/%s，实际 %s/%s", c.body, c.typ, c.level, q.Type, q.Level)
		}
	}
	if _, err := parseIPQualityIpAPI([]byte(`{"status":"fail","message":"quota"}`)); err == nil {
		t.Error("失败响应应当报错")
	}
	t.Logf("✓ IP 质量推导：%d 种情况正确", len(cases))
}
