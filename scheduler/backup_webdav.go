package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	backupWebdavConfigSettingKey    = "backup_webdav_config_v1"
	backupWebdavStateSettingKey     = "backup_webdav_state_v1"
	backupWebdavDefaultAutoSyncCron = "0 */6 * * *"
	backupWebdavFetchTimeout        = 15 * time.Second
	backupVersion                   = "2.1"
)

// BackupWebdavConfig holds the WebDAV backup configuration.
type BackupWebdavConfig struct {
	Enabled        bool   `json:"enabled"`
	FileURL        string `json:"fileUrl"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	ExportType     string `json:"exportType"`
	AutoSyncEnabled bool  `json:"autoSyncEnabled"`
	AutoSyncCron   string `json:"autoSyncCron"`
}

// BackupWebdavScheduler periodically exports backups to a WebDAV endpoint.
type BackupWebdavScheduler struct {
	cfg        *config.Config
	cronRunner *cronRunner
}

// NewBackupWebdavScheduler creates a new WebDAV backup scheduler.
func NewBackupWebdavScheduler(cfg *config.Config) *BackupWebdavScheduler {
	return &BackupWebdavScheduler{cfg: cfg}
}

func (s *BackupWebdavScheduler) Name() string { return "backup-webdav" }

func (s *BackupWebdavScheduler) Start(ctx context.Context) error {
	if err := s.reload(); err != nil {
		slog.Warn("backup-webdav: failed to start", "error", err)
	}
	return nil
}

func (s *BackupWebdavScheduler) Stop() error {
	if s.cronRunner != nil {
		s.cronRunner.stop()
		s.cronRunner = nil
	}
	return nil
}

// Reload reads the latest config and reschedules the cron job.
func (s *BackupWebdavScheduler) Reload() error {
	return s.reload()
}

func (s *BackupWebdavScheduler) reload() error {
	if s.cronRunner != nil {
		s.cronRunner.stop()
		s.cronRunner = nil
	}

	dbw := store.GetDB()
	if dbw == nil {
		return fmt.Errorf("database not available")
	}
	settingsStore := store.NewSettingsStore(dbw)

	cfg, err := loadBackupWebdavConfig(settingsStore)
	if err != nil || !cfg.Enabled || !cfg.AutoSyncEnabled {
		return nil // Not enabled, nothing to schedule
	}

	// Validate config
	if err := validateBackupWebdavConfig(cfg); err != nil {
		slog.Warn("backup-webdav: invalid config", "error", err)
		return nil
	}

	// Ensure cron is valid, fall back to default
	autoSyncCron := cfg.AutoSyncCron
	if !ValidateCronExpr(autoSyncCron) {
		autoSyncCron = backupWebdavDefaultAutoSyncCron
	}

	s.cronRunner = newCronRunner()
	_, err = s.cronRunner.addJob(autoSyncCron, func() {
		s.runExport(cfg)
	})
	if err != nil {
		slog.Error("backup-webdav: failed to add cron job", "error", err)
		return err
	}
	s.cronRunner.start()

	slog.Info("backup-webdav scheduler started", "cron", autoSyncCron)
	return nil
}

func (s *BackupWebdavScheduler) runExport(cfg *BackupWebdavConfig) {
	slog.Info("backup-webdav: starting export")

	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	settingsStore := store.NewSettingsStore(dbw)

	// Build export payload
	payload := map[string]any{
		"version":   backupVersion,
		"timestamp": time.Now().UnixMilli(),
	}

	if cfg.ExportType == "all" || cfg.ExportType == "accounts" {
		payload["type"] = "accounts"
	}
	if cfg.ExportType == "all" || cfg.ExportType == "preferences" {
		payload["type"] = "preferences"
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		slog.Error("backup-webdav: failed to marshal payload", "error", err)
		s.updateState(settingsStore, err)
		return
	}

	// PUT to WebDAV
	client := &http.Client{Timeout: backupWebdavFetchTimeout}
	req, err := http.NewRequest(http.MethodPut, cfg.FileURL, strings.NewReader(string(body)))
	if err != nil {
		slog.Error("backup-webdav: failed to create request", "error", err)
		s.updateState(settingsStore, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Username != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("backup-webdav: request failed", "error", err)
		s.updateState(settingsStore, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 120))
		errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		slog.Error("backup-webdav: upload failed", "status", resp.StatusCode)
		s.updateState(settingsStore, fmt.Errorf("%s", errMsg))
		return
	}

	s.updateState(settingsStore, nil)
	slog.Info("backup-webdav: export complete")
}

func (s *BackupWebdavScheduler) updateState(store *store.SettingsStore, err error) {
	state := map[string]any{
		"lastSyncAt": time.Now().UTC().Format(time.RFC3339),
	}
	if err != nil {
		state["lastError"] = err.Error()
	} else {
		state["lastError"] = nil
	}
	data, _ := json.Marshal(state)
	store.Set(backupWebdavStateSettingKey, string(data))
}

// loadBackupWebdavConfig reads and normalizes the WebDAV config from DB settings.
func loadBackupWebdavConfig(s *store.SettingsStore) (*BackupWebdavConfig, error) {
	raw, err := s.Get(backupWebdavConfigSettingKey)
	if err != nil || raw == "" {
		return &BackupWebdavConfig{}, nil
	}

	var cfg BackupWebdavConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return &BackupWebdavConfig{}, nil
	}

	if cfg.AutoSyncCron == "" {
		cfg.AutoSyncCron = backupWebdavDefaultAutoSyncCron
	}
	if cfg.ExportType == "" {
		cfg.ExportType = "all"
	}
	if !cfg.Enabled {
		cfg.AutoSyncEnabled = false
	}

	return &cfg, nil
}

func validateBackupWebdavConfig(cfg *BackupWebdavConfig) error {
	if cfg.Enabled && !isValidHTTPURL(cfg.FileURL) {
		return fmt.Errorf("WebDAV file URL is invalid")
	}
	if cfg.ExportType != "all" && cfg.ExportType != "accounts" && cfg.ExportType != "preferences" {
		return fmt.Errorf("invalid export type: %s", cfg.ExportType)
	}
	if cfg.AutoSyncEnabled && !cfg.Enabled {
		return fmt.Errorf("auto-sync requires enabled")
	}
	return nil
}

func isValidHTTPURL(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
