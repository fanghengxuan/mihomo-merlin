package main

import (
	"encoding/json"
	"testing"
)

// 版本比较：当前内核版本里包含目标版本即认为相同（内核 -v 输出形如 "Mihomo Meta v1.19.33 ..."）
func TestSameVersion(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"Mihomo Meta v1.19.33 linux arm", "v1.19.33", true},
		{"v1.19.33", "v1.19.33", true},
		{"Mihomo Meta v1.19.30 linux arm", "v1.19.33", false},
		{"", "v1.19.33", false},
		{"v1.19.33", "", false},
	}
	for _, c := range cases {
		if got := sameVersion(c.current, c.latest); got != c.want {
			t.Errorf("sameVersion(%q,%q)=%v，期望 %v", c.current, c.latest, got, c.want)
		}
	}
	t.Log("✓ 版本比较正确")
}

// 只挑 armv7 的 slim 资产，忽略 checksums 与非 armv7 架构
func TestFindSlimAsset(t *testing.T) {
	rel := kernelRelease{Tag: "slim-v1.19.33"}
	rel.Assets = append(rel.Assets,
		struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			APIURL string `json:"url"`
		}{ID: 1, Name: "checksums.txt", Size: 300, APIURL: "u1"},
	)
	mk := func(id int64, name string, size int64) {
		rel.Assets = append(rel.Assets, struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			APIURL string `json:"url"`
		}{ID: id, Name: name, Size: size, APIURL: "u" + name})
	}
	mk(2, "mihomo-linux-arm64-slim-v1.19.33", 8_000_000)
	mk(3, "mihomo-linux-armv7-slim-v1.19.33", 7_870_336)

	name, size, apiURL, ok := findSlimAsset(rel)
	if !ok {
		t.Fatal("应找到 armv7 资产")
	}
	if name != "mihomo-linux-armv7-slim-v1.19.33" || size != 7_870_336 {
		t.Fatalf("挑错资产：%s %d", name, size)
	}
	if apiURL == "" {
		t.Error("资产下载地址为空")
	}
	t.Logf("✓ 选中资产：%s（%d 字节）", name, size)
}

// release JSON 解析（真实 API 结构，带大量无关字段）
func TestParseKernelReleaseJSON(t *testing.T) {
	raw := `[{"tag_name":"slim-v1.19.33","name":"slim","published_at":"2026-10-29T06:40:00Z",
	 "draft":false,"prerelease":false,"body":"notes",
	 "assets":[{"id":42,"name":"mihomo-linux-armv7-slim-v1.19.33","size":7870336,"url":"https://api.x/42"}]}]`
	var list []kernelRelease
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(list) != 1 || list[0].Tag != "slim-v1.19.33" {
		t.Fatalf("解析结果不对：%+v", list)
	}
	name, _, _, ok := findSlimAsset(list[0])
	if !ok || name != "mihomo-linux-armv7-slim-v1.19.33" {
		t.Fatalf("资产解析不对：%v %s", ok, name)
	}
	t.Log("✓ release JSON 解析正确")
}
