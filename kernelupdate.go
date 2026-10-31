package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// 内核在线更新来源：本仓库 fork 的 slim 构建
// （tag 形如 slim-v1.19.33，资产名形如 mihomo-linux-armv7-slim-v1.19.33）
var (
	kernelRepo       = "fanghengxuan/mihomo"
	kernelTagPrefix  = "slim-"
	kernelAssetHint  = "mihomo-linux-armv7"
	kernelReleaseAPI = "https://api.github.com/repos/%s/releases?per_page=20"
	kernelAtomURL    = "https://github.com/%s/releases.atom"
	kernelAssetFmt   = "https://github.com/%s/releases/download/%s/mihomo-linux-armv7-slim-%s"
	// 国内直连 GitHub 经常超时，按顺序回退到镜像（都是「前缀 + 原始 URL」形式）
	kernelMirrors = []string{
		"https://ghfast.top/",
		"https://gh-proxy.com/",
		"https://ghproxy.net/",
		"https://hub.gitmirror.com/",
	}
	kernelAssetURL = "https://api.github.com/repos/%s/releases/assets/%d"
	maxKernelFetch = int64(96 << 20)
)

type kernelRelease struct {
	Tag         string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	Body        string `json:"body"`
	Assets      []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		APIURL string `json:"url"`
	} `json:"assets"`
}

// findSlimAsset 从 release 里挑出 armv7 的 slim 资产。
func findSlimAsset(rel kernelRelease) (name string, size int64, apiURL string, ok bool) {
	for _, asset := range rel.Assets {
		n := strings.ToLower(asset.Name)
		if !strings.Contains(n, kernelAssetHint) {
			continue
		}
		if strings.Contains(n, "checksums") {
			continue
		}
		if strings.HasSuffix(n, ".sha256") || strings.HasSuffix(n, ".txt") {
			continue
		}
		return asset.Name, asset.Size, asset.APIURL, true
	}
	return "", 0, "", false
}

// fetchKernelReleases 直接请求 GitHub API（不走代理：面板自己的外呼不该依赖内核）。
func (a *App) fetchKernelReleases() ([]kernelRelease, error) {
	client := a.newHTTPClient(false, false)
	client.Timeout = 25 * time.Second
	req, err := http.NewRequest("GET", fmt.Sprintf(kernelReleaseAPI, kernelRepo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mihomo-merlin-panel/"+panelVersion)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("访问 GitHub 失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("GitHub API 限流（403），稍后再试")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}
	var list []kernelRelease
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("解析 release 列表失败：%v", err)
	}
	return list, nil
}

// latestSlimRelease 取最新的、带 armv7 资产的 slim release。
func (a *App) latestSlimRelease() (*kernelRelease, string, int64, string, error) {
	list, err := a.fetchKernelReleases()
	if err != nil {
		return nil, "", 0, "", err
	}
	for _, rel := range list {
		if rel.Draft || !strings.HasPrefix(rel.Tag, kernelTagPrefix) {
			continue
		}
		name, size, apiURL, ok := findSlimAsset(rel)
		if !ok {
			continue
		}
		r := rel
		return &r, name, size, apiURL, nil
	}
	return nil, "", 0, "", fmt.Errorf("没有找到带 %s 资产的 slim release", kernelAssetHint)
}

// fetchCandidates 返回依次尝试的 URL：GitHub 直连、各镜像、以及（可选）走本机代理。
func fetchCandidates(rawURL string) []string {
	out := []string{rawURL}
	for _, m := range kernelMirrors {
		out = append(out, m+rawURL)
	}
	return out
}

// getWithFallback 取一个 URL 的内容；直连失败就换镜像，最后再试「经本机代理」。
// 返回值带上实际成功的地址，方便日志与排查。
func (a *App) getWithFallback(rawURL string, maxBytes int64, timeout time.Duration) ([]byte, string, error) {
	return a.getWithFallbackProgress(rawURL, maxBytes, timeout, nil)
}

// getWithFallbackProgress 同上，但把下载字节数回调出去（用于进度条）。
func (a *App) getWithFallbackProgress(rawURL string, maxBytes int64, timeout time.Duration, onProgress func(read, total int64)) ([]byte, string, error) {
	var lastErr error
	try := func(u string, viaProxy bool) ([]byte, error) {
		client := a.newHTTPClient(viaProxy, false)
		client.Timeout = timeout
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "mihomo-merlin-panel/"+panelVersion)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		total := resp.ContentLength
		if onProgress == nil {
			return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
		}
		buf := make([]byte, 0, 1<<20)
		chunk := make([]byte, 64<<10)
		var read int64
		for {
			n, err := resp.Body.Read(chunk)
			if n > 0 {
				read += int64(n)
				if read > maxBytes {
					return nil, fmt.Errorf("内容超过上限")
				}
				buf = append(buf, chunk[:n]...)
				onProgress(read, total)
			}
			if err != nil {
				if err == io.EOF {
					return buf, nil
				}
				if len(buf) > 0 {
					return buf, nil
				}
				return nil, err
			}
		}
	}

	for _, u := range fetchCandidates(rawURL) {
		b, err := try(u, false)
		if err == nil && len(b) > 0 {
			return b, u, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	// 最后一种手段：借道内核代理（前提是内核在跑，且订阅代理可用）
	if a.engine.Running() {
		if b, err := try(rawURL, true); err == nil && len(b) > 0 {
			return b, rawURL + "（经本机代理）", nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("所有下载地址都失败")
	}
	return nil, "", lastErr
}

// latestSlimFromAtom 用 releases.atom 拿最新 slim tag，再 HEAD 资产拿大小。
// GitHub 匿名 API 每小时只有 60 次，atom 与资产下载都不限量，更适合设备侧轮询。
func (a *App) latestSlimFromAtom() (tag, version, assetURL string, size int64, published string, err error) {
	body, usedURL, ferr := a.getWithFallback(fmt.Sprintf(kernelAtomURL, kernelRepo), 1<<20, 25*time.Second)
	if ferr != nil {
		return "", "", "", 0, "", fmt.Errorf("获取版本信息失败：%v", ferr)
	}
	if usedURL != fmt.Sprintf(kernelAtomURL, kernelRepo) {
		// 镜像成功也照常用，只在日志里留痕
		a.addLog("info", "版本信息经镜像获取："+usedURL)
	}

	// 从 <link href=".../releases/tag/<tag>"> 里取第一个 slim-* 的 tag
	// （atom 的 <title> 是 release 标题，可能带中文，不能当 tag 用）
	re := regexp.MustCompile(`releases/tag/([^"'<>\s]+)`)
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		t := strings.TrimSpace(m[1])
		if strings.HasPrefix(t, kernelTagPrefix) {
			tag = t
			break
		}
	}
	if tag == "" {
		return "", "", "", 0, "", fmt.Errorf("atom 里没有找到 slim-* 版本")
	}
	version = strings.TrimPrefix(tag, kernelTagPrefix)
	assetURL = fmt.Sprintf(kernelAssetFmt, kernelRepo, tag, version)

	// 大小：HEAD 拿不到就留 -1，不影响下载
	return tag, version, assetURL, size, published, nil
}

// handleKernelCheck 检查更新：GET /api/kernel/check
func (a *App) handleKernelCheck(w http.ResponseWriter, r *http.Request) {
	cur := a.currentKernelInfo()
	tag, latest, assetURL, size, published, err := a.latestSlimFromAtom()
	if err != nil {
		// 回退到 API（可能被限流）
		if rel, assetName, apiSize, _, aerr := a.latestSlimRelease(); aerr == nil {
			tag = rel.Tag
			latest = strings.TrimPrefix(rel.Tag, kernelTagPrefix)
			assetURL = "https://github.com/" + kernelRepo + "/releases/download/" + rel.Tag + "/" + assetName
			size = apiSize
			published = rel.PublishedAt
			err = nil
		} else {
			writeJSON(w, 200, map[string]any{"ok": false,
				"error":   err.Error() + "；" + aerr.Error(),
				"current": cur.Version, "repo": kernelRepo})
			return
		}
	}
	assetName := "mihomo-linux-armv7-slim-" + latest
	_ = published
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"repo":        kernelRepo,
		"current":     cur.Version,
		"latest":      latest,
		"tag":         tag,
		"asset":       assetName,
		"size":        size,
		"publishedAt": published,
		"notes":       "slim 构建：with_low_memory，无 gVisor / Tailscale / EasyTier / ZeroTier",
		"hasUpdate":   !sameVersion(cur.Version, latest),
		"download":    assetURL,
		"url":         "https://github.com/" + kernelRepo + "/releases/tag/" + tag,
	})
}

// sameVersion 粗比较：当前内核版本里包含目标版本号即视为同一个。
func sameVersion(current, latest string) bool {
	c := strings.TrimSpace(current)
	l := strings.TrimSpace(latest)
	if c == "" || l == "" {
		return false
	}
	return strings.Contains(c, strings.TrimPrefix(l, "v"))
}

// handleKernelUpdate 在线安装最新 slim 内核：POST /api/kernel/update
func (a *App) handleKernelUpdate(w http.ResponseWriter, r *http.Request) {
	a.kernelMu.Lock()
	defer a.kernelMu.Unlock()

	tag, version, downloadURL, size, _, err := a.latestSlimFromAtom()
	if err != nil {
		// 回退 API
		if rel, assetName, apiSize, _, aerr := a.latestSlimRelease(); aerr == nil {
			tag = rel.Tag
			version = strings.TrimPrefix(rel.Tag, kernelTagPrefix)
			downloadURL = "https://github.com/" + kernelRepo + "/releases/download/" + rel.Tag + "/" + assetName
			size = apiSize
			err = nil
		} else {
			writeErr(w, 502, err.Error()+"；"+aerr.Error())
			return
		}
	}
	assetName := "mihomo-linux-armv7-slim-" + version
	if size > maxKernelFetch {
		writeErr(w, 502, fmt.Sprintf("远端内核 %d 字节，超过 %d 上限", size, maxKernelFetch))
		return
	}

	a.progPct("kernel", assetName, "download", "开始下载内核…", 0, false, false)
	lastPct := -1
	raw, usedURL, derr := a.getWithFallbackProgress(downloadURL, maxKernelFetch, 3*time.Minute,
		func(read, total int64) {
			pct := 0
			if total > 0 {
				pct = int(read * 100 / total)
			}
			if pct == lastPct || pct%5 != 0 { // 每 5% 报一次，避免刷屏
				return
			}
			lastPct = pct
			a.progPct("kernel", assetName, "download",
				fmt.Sprintf("已下载 %s / %s", humanBytes(read), humanBytes(total)), pct, false, false)
		})
	if derr != nil || len(raw) == 0 {
		if derr == nil {
			derr = fmt.Errorf("下载内容为空")
		}
		writeErr(w, 502, "下载内核失败（已尝试 GitHub 直连与镜像，可稍后重试或手动上传）："+derr.Error())
		return
	}
	if usedURL != downloadURL {
		a.addLog("info", "内核经备用地址下载："+usedURL)
	}
	a.progPct("kernel", assetName, "verify", "下载完成，校验内核…", 92, false, false)
	payload, wasGz, err := maybeGunzip(raw)
	if err != nil {
		writeErr(w, 502, "下载的内容不是有效内核："+err.Error())
		return
	}

	a.progPct("kernel", assetName, "install", "写入并重启内核…", 96, false, false)
	res, ierr := a.installKernelBytes(payload, assetName)
	if ierr != nil {
		writeErr(w, ierr.code, ierr.msg)
		return
	}
	a.addLog("info", fmt.Sprintf("在线更新内核：%s（%s，%s）", res.Version, assetName,
		map[bool]string{true: "gzip 解压", false: "原始 ELF"}[wasGz]))
	writeJSON(w, 200, map[string]any{
		"ok":         res.OK,
		"message":    res.Message,
		"version":    res.Version,
		"tag":        tag,
		"asset":      assetName,
		"restarted":  res.Restarted,
		"rolledBack": res.RolledBack,
		"error":      res.Error,
		"kernel":     a.currentKernelInfo(),
	})
}
