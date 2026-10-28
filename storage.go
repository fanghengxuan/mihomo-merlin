package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// 存储限额：jffs 分区很小，这些上限要保守。
const (
	maxKernelBackups = 1         // 内核备份只留最新 1 份（每份约 8MB）
	maxLogBytes      = 1 << 20   // 日志文件超过 1MB 就截断
	logKeepBytes     = 256 << 10 // 截断时保留最后 256KB
	maxRawCacheBytes = 1 << 20   // 原始订阅缓存总量上限 1MB
	maxRawCacheEach  = 256 << 10 // 单个原始订阅缓存上限 256KB
)

type StorageItem struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

type StorageReport struct {
	DataDir    string        `json:"dataDir"`
	Total      int64         `json:"total"`
	Free       int64         `json:"free"`
	Items      []StorageItem `json:"items"`
	Backups    int           `json:"backups"`
	LogBytes   int64         `json:"logBytes"`
	CacheBytes int64         `json:"cacheBytes"`
}

func dirStats(p string) (int64, int) {
	var size int64
	var files int
	_ = filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		size += info.Size()
		files++
		return nil
	})
	return size, files
}

func freeSpace(p string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

// storageReport 汇总数据目录占用，供设置页展示。
func (a *App) storageReport() StorageReport {
	rep := StorageReport{DataDir: a.dataDir, Free: freeSpace(a.dataDir)}

	sub := func(name, rel string) StorageItem {
		p := filepath.Join(a.dataDir, rel)
		b, f := dirStats(p)
		return StorageItem{Name: name, Bytes: b, Files: f}
	}

	items := []StorageItem{
		sub("节点文件", "providers"),
		sub("规则集", "rules"),
		sub("原始订阅缓存", "cache"),
		sub("内核备份", "kernel"),
		sub("运行日志", "logs"),
	}
	for _, it := range items {
		rep.Total += it.Bytes
		switch it.Name {
		case "内核备份":
			rep.Backups = it.Files
		case "运行日志":
			rep.LogBytes = it.Bytes
		case "原始订阅缓存":
			rep.CacheBytes = it.Bytes
		}
	}
	// 顶层零散文件
	for _, f := range []string{"config.json", "settings.json", "subs.json", "cache.db", ".config.stamp"} {
		if fi, err := os.Stat(filepath.Join(a.dataDir, f)); err == nil {
			rep.Total += fi.Size()
			items = append(items, StorageItem{Name: f, Bytes: fi.Size(), Files: 1})
		}
	}
	rep.Items = items
	return rep
}

// CleanupStorage 清理无用文件，返回释放的字节数与执行明细。
func (a *App) CleanupStorage() (int64, []string) {
	var freed int64
	var steps []string

	// 1) 订阅已删除的节点文件与原始缓存
	a.mu.RLock()
	alive := map[string]bool{}
	for _, s := range a.subs {
		alive[s.ID] = true
	}
	a.mu.RUnlock()

	removeOrphans := func(dir, suffix, label string) {
		entries, err := os.ReadDir(filepath.Join(a.dataDir, dir))
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasSuffix(e.Name(), suffix) {
				continue
			}
			id := strings.TrimSuffix(e.Name(), suffix)
			if alive[id] {
				continue
			}
			p := filepath.Join(a.dataDir, dir, e.Name())
			if fi, err := os.Stat(p); err == nil {
				if os.Remove(p) == nil {
					freed += fi.Size()
					steps = append(steps, fmt.Sprintf("删除无主%s：%s", label, e.Name()))
				}
			}
		}
	}
	removeOrphans("providers", ".json", "节点文件")
	removeOrphans("cache", ".sub", "原始订阅缓存")

	// 2) 残留临时文件
	_ = filepath.Walk(a.dataDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".tmp") {
			if os.Remove(p) == nil {
				freed += info.Size()
				steps = append(steps, "删除临时文件："+filepath.Base(p))
			}
		}
		return nil
	})

	// 3) 内核备份只保留最新 1 份
	backupDir := a.kernelDir()
	if entries, err := os.ReadDir(backupDir); err == nil {
		var backups []os.DirEntry
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "mihomo.backup-") {
				backups = append(backups, e)
			}
		}
		sort.Slice(backups, func(i, j int) bool { return backups[i].Name() < backups[j].Name() })
		for len(backups) > maxKernelBackups {
			p := filepath.Join(backupDir, backups[0].Name())
			if fi, err := os.Stat(p); err == nil {
				if os.Remove(p) == nil {
					freed += fi.Size()
					steps = append(steps, "删除旧内核备份："+backups[0].Name())
				}
			}
			backups = backups[1:]
		}
	}

	// 4) 日志截断
	logDir := filepath.Join(a.dataDir, "logs")
	if entries, err := os.ReadDir(logDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			p := filepath.Join(logDir, e.Name())
			fi, err := os.Stat(p)
			if err != nil || fi.Size() <= maxLogBytes {
				continue
			}
			if n, err := truncateFileKeepTail(p, logKeepBytes); err == nil {
				freed += n
				steps = append(steps, fmt.Sprintf("截断日志 %s（释放 %s）", e.Name(), humanBytes(n)))
			}
		}
	}

	// 5) 原始订阅缓存总量超限时按时间删最旧的
	cacheDir := filepath.Join(a.dataDir, "cache")
	if entries, err := os.ReadDir(cacheDir); err == nil {
		type cf struct {
			path string
			size int64
			mod  time.Time
		}
		var files []cf
		var total int64
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sub") {
				continue
			}
			p := filepath.Join(cacheDir, e.Name())
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			files = append(files, cf{p, fi.Size(), fi.ModTime()})
			total += fi.Size()
		}
		sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
		for _, f := range files {
			// 单文件过大也一并回收
			if total <= maxRawCacheBytes && f.size <= maxRawCacheEach {
				continue
			}
			if os.Remove(f.path) == nil {
				freed += f.size
				total -= f.size
				steps = append(steps, "回收原始订阅缓存："+filepath.Base(f.path))
			}
		}
	}

	// 6) 配置校验戳在配置丢失时没用了
	if !fileExists(a.configPath()) {
		p := filepath.Join(a.dataDir, ".config.stamp")
		if fi, err := os.Stat(p); err == nil {
			if os.Remove(p) == nil {
				freed += fi.Size()
				steps = append(steps, "删除失效的配置校验缓存")
			}
		}
	}

	if len(steps) > 0 {
		a.addLog("info", fmt.Sprintf("存储清理：释放 %s（%d 项）", humanBytes(freed), len(steps)))
	}
	return freed, steps
}

// truncateFileKeepTail 只保留文件末尾 keep 字节，返回释放的字节数。
func truncateFileKeepTail(path string, keep int64) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if fi.Size() <= keep {
		return 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(fi.Size()-keep, 0); err != nil {
		return 0, err
	}
	tmp := path + ".tail"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, 64<<10)
	var written int64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return 0, werr
			}
			written += int64(n)
		}
		if rerr != nil {
			break
		}
	}
	out.Close()
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return fi.Size() - written, nil
}

// storageJanitor 定期清理，避免小分区的路由器被日志与备份撑满。
func (a *App) storageJanitor() {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for range t.C {
		a.CleanupStorage()
	}
}
