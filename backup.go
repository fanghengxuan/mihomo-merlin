package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// backupKind 用来识别本插件导出的备份文件，避免误导入别人的 JSON。
const backupKind = "mihomo-merlin-backup"

// BackupBundle 是「设置 + 订阅 + 各订阅节点文件」的完整快照。
// 订阅里含机场地址与令牌，属于敏感信息，导出时会在界面上提示。
type BackupBundle struct {
	Kind       string            `json:"kind"`
	Format     int               `json:"format"`
	Panel      string            `json:"panelVersion"`
	ExportedAt int64             `json:"exportedAt"`
	Settings   json.RawMessage   `json:"settings"`
	Subs       []Sub             `json:"subs"`
	Providers  map[string]string `json:"providers,omitempty"` // 订阅 id → provider 文件内容
	Rules      string            `json:"rulesMeta,omitempty"` // rules/meta.json 原文
}

// buildBackup 汇总当前状态；只读，不改变运行态。
func (a *App) buildBackup() (*BackupBundle, error) {
	a.mu.RLock()
	rawSettings, err := json.Marshal(a.settings)
	if err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	b := &BackupBundle{
		Kind:       backupKind,
		Format:     1,
		Panel:      panelVersion,
		ExportedAt: time.Now().Unix(),
		Settings:   rawSettings,
		Subs:       append([]Sub(nil), a.subs...),
	}
	a.mu.RUnlock()

	b.Providers = map[string]string{}
	for _, sub := range b.Subs {
		raw, err := os.ReadFile(a.providerPath(sub.ID))
		if err != nil {
			continue
		}
		b.Providers[sub.ID] = string(raw)
	}
	if raw, err := os.ReadFile(filepath.Join(a.dataDir, "rules", "meta.json")); err == nil {
		b.Rules = string(raw)
	}
	return b, nil
}

// restoreBackup 覆盖设置与订阅，并把节点文件写回，随后热重载内核。
func (a *App) restoreBackup(b *BackupBundle) error {
	if b == nil || b.Kind != backupKind {
		return fmt.Errorf("不是本插件导出的备份文件")
	}
	if b.Format > 1 {
		return fmt.Errorf("备份文件版本过新（format=%d），请先升级插件", b.Format)
	}
	// 从默认值起步再叠加备份里的字段：老备份缺字段时不会把 allowLan/autoStart 等清成 false
	s := defaultSettings()
	if len(b.Settings) > 0 {
		if err := json.Unmarshal(b.Settings, &s); err != nil {
			return fmt.Errorf("备份里的 settings 不是合法 JSON：%v", err)
		}
	}
	if s.APIPort == 0 {
		s.APIPort = a.settings.APIPort
	}
	if strings.TrimSpace(s.ClashSecret) == "" {
		s.ClashSecret = a.settings.ClashSecret
	}

	subs := make([]Sub, 0, len(b.Subs))
	for _, sub := range b.Subs {
		if strings.TrimSpace(sub.ID) == "" {
			sub.ID = "s_" + randomToken(4)
		}
		if strings.TrimSpace(sub.Name) == "" {
			sub.Name = "恢复的订阅"
		}
		subs = append(subs, sub)
	}

	a.subMu.Lock()
	a.mu.Lock()
	a.settings = s
	a.subs = subs
	err := a.saveSettings()
	if err == nil {
		err = a.saveSubsLocked()
	}
	a.mu.Unlock()
	a.subMu.Unlock()
	if err != nil {
		return err
	}

	for id, content := range b.Providers {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(content) == "" {
			continue
		}
		path := a.providerPath(id)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(path, []byte(content), 0o600)
	}
	if strings.TrimSpace(b.Rules) != "" {
		rulesDir := filepath.Join(a.dataDir, "rules")
		if err := os.MkdirAll(rulesDir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(rulesDir, "meta.json"), []byte(b.Rules), 0o600)
		}
	}

	a.addLog("info", fmt.Sprintf("已从备份恢复：%d 个订阅、%d 份节点文件", len(subs), len(b.Providers)))
	if a.engine.Running() {
		return a.appReload()
	}
	return nil
}

// handleBackup 导出：GET /api/backup
func (a *App) handleBackup(w http.ResponseWriter, r *http.Request) {
	b, err := a.buildBackup()
	if err != nil {
		writeErr(w, 500, "导出失败："+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="mihomo-merlin-backup-%s.json"`, time.Now().Format("20060102-1504")))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(b)
}

// handleRestore 导入：POST /api/restore
func (a *App) handleRestore(w http.ResponseWriter, r *http.Request) {
	var b BackupBundle
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&b); err != nil {
		writeErr(w, 400, "备份文件不是合法 JSON 或超过 16MB")
		return
	}
	if err := a.restoreBackup(&b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, okBody(map[string]any{
		"message": fmt.Sprintf("已恢复 %d 个订阅", len(b.Subs)),
		"subs":    len(b.Subs),
	}))
}
