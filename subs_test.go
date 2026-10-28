package main

import (
	"testing"
	"time"
)

func TestSubUpdateDue(t *testing.T) {
	now := time.Now().Unix()
	hour := int64(3600)

	mk := func(interval int, updatedAgo int64, enabled bool, url string) Sub {
		return Sub{ID: "s", Name: "t", URL: url, Interval: interval, Enabled: enabled,
			UpdatedAt: now - updatedAgo}
	}

	cases := []struct {
		name   string
		sub    Sub
		global int
		want   bool
	}{
		{"全局6小时·刚更新", mk(0, hour, true, "https://a"), 6, false},
		{"全局6小时·7小时前", mk(0, 7*hour, true, "https://a"), 6, true},
		{"全局关闭·单条设1小时且2小时前", mk(1, 2*hour, true, "https://a"), 0, true},
		{"全局关闭·单条也未设", mk(0, 2*hour, true, "https://a"), 0, false},
		{"单条覆盖全局（单条12小时·7小时前）", mk(12, 7*hour, true, "https://a"), 6, false},
		{"禁用不更新", mk(0, 7*hour, false, "https://a"), 6, false},
		{"手动导入不更新", mk(0, 7*hour, true, ""), 6, false},
		{"从未更新过", Sub{ID: "s", Name: "t", URL: "https://a", Enabled: true, UpdatedAt: 0}, 6, true},
	}

	for _, c := range cases {
		if got := subUpdateDue(c.sub, c.global, now); got != c.want {
			t.Errorf("%s：期望 %v，实际 %v", c.name, c.want, got)
		}
	}
	t.Logf("✓ 自动更新到期判断 %d 种情况全部正确", len(cases))
}
