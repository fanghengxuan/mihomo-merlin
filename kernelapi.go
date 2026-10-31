package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxKernelUpload = 96 << 20 // mihomo 未压缩 56MB，留余量

type kernelInfo struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	ModTime    int64  `json:"modTime"`
	BackupVer  string `json:"backupVersion,omitempty"`
	BackupTime int64  `json:"backupTime,omitempty"`
}

type kernelBackup struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Time    int64  `json:"time"`
}

func (a *App) kernelDir() string { return filepath.Join(a.dataDir, "kernel") }
func (a *App) backupMetaPath() string {
	return filepath.Join(a.kernelDir(), "backup.json")
}

// readKernelVersion 跑 `mihomo -v` 取第一行。
func (a *App) readKernelVersion(path string) (string, error) {
	out, err := run(20*time.Second, path, "-v")
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if err != nil {
		if line == "" {
			return "", fmt.Errorf("无法执行内核：%v", err)
		}
		return "", fmt.Errorf("内核执行失败：%s", line)
	}
	if line == "" {
		return "", fmt.Errorf("内核没有返回版本信息")
	}
	return line, nil
}

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// inspectELF 检查是否是 32 位 ARM Linux 可执行文件。
// 返回错误说明原因；MM_TEST_KERNEL=1 时跳过架构校验（仅本地调试用）。
func inspectELF(b []byte) error {
	if os.Getenv("MM_TEST_KERNEL") == "1" {
		return nil
	}
	if len(b) < 64 {
		return fmt.Errorf("文件太小，不是有效的可执行文件")
	}
	if !bytes.Equal(b[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return fmt.Errorf("不是 ELF 文件（请上传 mihomo 的 linux-armv7 版本，不要传 .zip/.rar）")
	}
	if b[4] != 1 {
		return fmt.Errorf("不是 32 位 ELF（当前是 64 位，请下载 linux-armv7 而不是 linux-arm64）")
	}
	machine := binary.LittleEndian.Uint16(b[18:20])
	if machine != 40 {
		return fmt.Errorf("不是 ARM 架构（ELF machine=%d，期望 40）", machine)
	}
	// 注意：UPX 压缩过的 mihomo 没有 section header，属正常情况，不做进一步要求
	return nil
}

// maybeGunzip 处理 GitHub release 的 .gz 包。
func maybeGunzip(b []byte) ([]byte, bool, error) {
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, true, fmt.Errorf("gzip 解压失败：%v", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, maxKernelUpload))
		if err != nil {
			return nil, true, fmt.Errorf("gzip 解压失败：%v", err)
		}
		return out, true, nil
	}
	return b, false, nil
}

func (a *App) loadBackupMeta() *kernelBackup {
	var kb kernelBackup
	if err := readJSON(a.backupMetaPath(), &kb); err != nil || kb.Path == "" {
		return nil
	}
	if !fileExists(kb.Path) {
		return nil
	}
	return &kb
}

func (a *App) saveBackupMeta(kb *kernelBackup) {
	_ = os.MkdirAll(a.kernelDir(), 0o755)
	_ = writeJSONAtomic(a.backupMetaPath(), kb)
}

func (a *App) currentKernelInfo() kernelInfo {
	fi, _ := os.Stat(a.mihomoPath)
	key := ""
	if fi != nil {
		key = fmt.Sprintf("%s|%d|%d", a.mihomoPath, fi.Size(), fi.ModTime().Unix())
	}

	a.kinfoMu.Lock()
	info := a.kinfoInfo
	cached := key != "" && a.kinfoKey == key
	a.kinfoMu.Unlock()

	if !cached {
		info = kernelInfo{Path: a.mihomoPath}
		if fi != nil {
			info.Size = fi.Size()
			info.ModTime = fi.ModTime().Unix()
		}
		if v, err := a.readKernelVersion(a.mihomoPath); err == nil {
			info.Version = v
		}
		info.SHA256 = sha256File(a.mihomoPath)
		if key != "" {
			a.kinfoMu.Lock()
			a.kinfoKey = key
			a.kinfoInfo = info
			a.kinfoMu.Unlock()
		}
	}
	if kb := a.loadBackupMeta(); kb != nil {
		info.BackupVer = kb.Version
		info.BackupTime = kb.Time
	}
	return info
}

func (a *App) handleKernelInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "kernel": a.currentKernelInfo()})
}

// handleKernelUpload 接收上传的内核（原始 ELF 或 .gz），校验后替换并重启。
func (a *App) handleKernelUpload(w http.ResponseWriter, r *http.Request) {
	a.kernelMu.Lock()
	defer a.kernelMu.Unlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxKernelUpload+8<<20)
	if err := r.ParseMultipartForm(maxKernelUpload); err != nil {
		writeErr(w, 400, "上传失败：文件过大或表单不合法（上限 96MB）")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "没有收到文件字段 file")
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxKernelUpload))
	if err != nil {
		writeErr(w, 400, "读取上传内容失败："+err.Error())
		return
	}
	if len(raw) == 0 {
		writeErr(w, 400, "上传的文件是空的")
		return
	}

	payload, wasGz, err := maybeGunzip(raw)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	res, herr := a.installKernelBytes(payload, hdr.Filename)
	if herr != nil {
		writeErr(w, herr.code, herr.msg)
		return
	}
	a.addLog("info", fmt.Sprintf("内核已更新：%s（上传文件 %s，%s）", res.Version, hdr.Filename,
		map[bool]string{true: "已解压 gzip", false: "原始 ELF"}[wasGz]))
	writeJSON(w, 200, map[string]any{
		"ok":         res.OK,
		"message":    res.Message,
		"version":    res.Version,
		"restarted":  res.Restarted,
		"rolledBack": res.RolledBack,
		"error":      res.Error,
		"kernel":     a.currentKernelInfo(),
	})
}

// installResult 是一次内核安装的结果（上传与在线更新共用）。
type installResult struct {
	OK         bool
	Message    string
	Version    string
	Restarted  bool
	RolledBack bool
	Error      string
}

type installError struct {
	code int
	msg  string
}

// installKernelBytes 校验（ELF / -v / -t）并替换内核；失败不落盘，启动失败自动回滚。
// 调用方需持有 a.kernelMu。
func (a *App) installKernelBytes(payload []byte, label string) (*installResult, *installError) {
	if err := inspectELF(payload); err != nil {
		return nil, &installError{400, err.Error()}
	}
	if err := os.MkdirAll(a.kernelDir(), 0o755); err != nil {
		return nil, &installError{500, "创建内核目录失败：" + err.Error()}
	}
	tmp := filepath.Join(a.kernelDir(), fmt.Sprintf("upload-%d", time.Now().UnixNano()))
	if err := os.WriteFile(tmp, payload, 0o755); err != nil {
		return nil, &installError{500, "写入临时文件失败：" + err.Error()}
	}
	defer func() { _ = os.Remove(tmp) }()

	ver, err := a.readKernelVersion(tmp)
	if err != nil {
		return nil, &installError{400, "这个文件跑不起来：" + err.Error()}
	}
	if err := a.appReloadDryRun(tmp); err != nil {
		return nil, &installError{400, "新内核不接受当前配置：" + err.Error()}
	}

	if kb := a.backupCurrentKernel(); kb != nil {
		a.addLog("info", fmt.Sprintf("已备份旧内核：%s", kb.Version))
	}
	if err := replaceFile(tmp, a.mihomoPath); err != nil {
		return nil, &installError{500, "替换内核失败：" + err.Error()}
	}
	_ = os.Chmod(a.mihomoPath, 0o755)
	_ = removeIfExists(filepath.Join(a.dataDir, ".config.stamp"))

	res := &installResult{OK: true, Version: ver}
	wasRunning := a.engine.Running()
	if wasRunning {
		if err := a.engine.Restart(); err != nil {
			a.addLog("error", "新内核启动失败，自动回滚："+err.Error())
			if kb := a.loadBackupMeta(); kb != nil {
				if err := replaceFile(kb.Path, a.mihomoPath); err == nil {
					_ = a.engine.Restart()
				}
			}
			res.OK = false
			res.RolledBack = true
			res.Error = "新内核已通过校验，但启动失败，已回滚旧版本：" + err.Error()
			res.Message = "启动失败，已回滚"
			return res, nil
		}
		res.Restarted = true
		res.Message = "内核已更新并重启"
	} else {
		res.Message = "内核已更新"
	}
	_ = label
	return res, nil
}

// appReloadDryRun 用指定内核跑一次 `-t` 配置校验。
func (a *App) appReloadDryRun(kernel string) error {
	cfgPath := a.configPath()
	if err := a.WriteConfig(); err != nil {
		// 典型情况：刚装好还没加订阅，生成不出完整配置。
		// 这时不该拦着不让换内核 —— 用一份最小配置验证「这个内核能跑、认得基本结构」即可。
		a.addLog("info", "暂无可用于校验的配置（"+err.Error()+"），改用最小配置校验新内核")
		_ = os.MkdirAll(a.kernelDir(), 0o755)
		minimal := filepath.Join(a.kernelDir(), "dryrun-minimal.yaml")
		if werr := os.WriteFile(minimal, []byte("mixed-port: 7890\nmode: rule\nlog-level: info\n"), 0o600); werr != nil {
			return fmt.Errorf("无法写入临时校验配置：%v", werr)
		}
		defer func() { _ = os.Remove(minimal) }()
		cfgPath = minimal
	}
	out, err := run(90*time.Second, kernel, "-t", "-d", a.dataDir, "-f", cfgPath)
	if err != nil {
		return fmt.Errorf("%s", tail(out, 500))
	}
	return nil
}

// backupCurrentKernel 把当前内核复制到数据目录并记下元信息。
func (a *App) backupCurrentKernel() *kernelBackup {
	if !fileExists(a.mihomoPath) {
		return nil
	}
	_ = os.MkdirAll(a.kernelDir(), 0o755)
	dst := filepath.Join(a.kernelDir(), fmt.Sprintf("mihomo.backup-%d", time.Now().Unix()))
	if err := copyFile(a.mihomoPath, dst); err != nil {
		a.addLog("warning", "备份旧内核失败："+err.Error())
		return nil
	}
	_ = os.Chmod(dst, 0o755)
	ver, _ := a.readKernelVersion(a.mihomoPath)
	kb := &kernelBackup{Path: dst, Version: ver, Time: time.Now().Unix()}
	a.saveBackupMeta(kb)
	// 这台机器的 jffs 分区很小，内核备份只留 1 份（约 8MB）
	if entries, err := os.ReadDir(a.kernelDir()); err == nil {
		var backups []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "mihomo.backup-") {
				backups = append(backups, filepath.Join(a.kernelDir(), e.Name()))
			}
		}
		for len(backups) > 1 {
			_ = os.Remove(backups[0])
			backups = backups[1:]
		}
	}
	return kb
}

func (a *App) handleKernelRollback(w http.ResponseWriter, r *http.Request) {
	a.kernelMu.Lock()
	defer a.kernelMu.Unlock()

	kb := a.loadBackupMeta()
	if kb == nil {
		writeErr(w, 400, "没有可回滚的备份")
		return
	}
	// 先把当前版本也备份一份，便于来回切换
	current := a.mihomoPath
	tmpNext := filepath.Join(a.kernelDir(), fmt.Sprintf("rollback-%d", time.Now().UnixNano()))
	if err := copyFile(current, tmpNext); err == nil {
		_ = os.Chmod(tmpNext, 0o755)
		curVer, _ := a.readKernelVersion(current)
		a.saveBackupMeta(&kernelBackup{Path: tmpNext, Version: curVer, Time: time.Now().Unix()})
	}

	if err := replaceFile(kb.Path, a.mihomoPath); err != nil {
		writeErr(w, 500, "回滚失败："+err.Error())
		return
	}
	_ = os.Chmod(a.mihomoPath, 0o755)
	_ = removeIfExists(filepath.Join(a.dataDir, ".config.stamp"))

	wasRunning := a.engine.Running()
	if wasRunning {
		if err := a.engine.Restart(); err != nil {
			writeErr(w, 500, "回滚后内核启动失败："+err.Error())
			return
		}
	}
	a.addLog("info", "内核已回滚："+kb.Version)
	writeJSON(w, 200, map[string]any{
		"ok":        true,
		"message":   "已回滚到 " + kb.Version,
		"restarted": wasRunning,
		"kernel":    a.currentKernelInfo(),
	})
}

// replaceFile 优先用 rename；跨文件系统时退回复制。
func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return nil
}

var _ = json.Marshal
