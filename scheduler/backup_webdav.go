package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	backupsvc "github.com/tokendancelab/metapi-go/service/backup"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	backupWebdavConfigSettingKey    = "backup_webdav_config_v1"
	backupWebdavStateSettingKey     = "backup_webdav_state_v1"
	backupWebdavDefaultAutoSyncCron = "0 */6 * * *"
	backupWebdavFetchTimeout        = 15 * time.Second
)

var allowPrivateBackupWebdavTargets bool

// BackupWebdavConfig holds the WebDAV backup configuration.
type BackupWebdavConfig struct {
	Enabled         bool   `json:"enabled"`
	FileURL         string `json:"fileUrl"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	ExportType      string `json:"exportType"`
	AutoSyncEnabled bool   `json:"autoSyncEnabled"`
	AutoSyncCron    string `json:"autoSyncCron"`
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
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runExportLocked(cfg, dbw)
	})
}

func (s *BackupWebdavScheduler) runExportLocked(cfg *BackupWebdavConfig, dbw *store.DB) {
	settingsStore := store.NewSettingsStore(dbw)

	payload, err := backupsvc.BuildPayload(dbw.DB, cfg.ExportType)
	if err != nil {
		slog.Error("backup-webdav: failed to build payload", "error", err)
		s.updateState(settingsStore, err)
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("backup-webdav: failed to marshal payload", "error", err)
		s.updateState(settingsStore, err)
		return
	}

	// PUT to WebDAV
	client := newBackupWebdavHTTPClient()
	req, err := http.NewRequest(http.MethodPut, cfg.FileURL, bytes.NewReader(body))
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

func newBackupWebdavHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   backupWebdavFetchTimeout,
		Transport: newBackupWebdavHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			if !isValidHTTPURL(req.URL.String()) {
				return fmt.Errorf("refusing WebDAV redirect to unsafe target")
			}
			if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("refusing WebDAV redirect from https to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

func newBackupWebdavHTTPTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if !allowPrivateBackupWebdavTargets {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				if err := rejectUnsafeBackupWebdavDialHost(ctx, host); err != nil {
					return nil, err
				}
			}
			return dialer.DialContext(ctx, network, address)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: backupWebdavFetchTimeout,
		IdleConnTimeout:       30 * time.Second,
	}
}

func rejectUnsafeBackupWebdavDialHost(ctx context.Context, host string) error {
	if !isAllowedBackupWebdavTargetHost(host) {
		return fmt.Errorf("refusing WebDAV request to unsafe host %q", host)
	}
	if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses found for WebDAV host %q", host)
	}
	for _, ip := range ips {
		if isUnsafeBackupWebdavAddr(ip) {
			return fmt.Errorf("refusing WebDAV request to unsafe resolved address %s", ip)
		}
	}
	return nil
}

func (s *BackupWebdavScheduler) updateState(store *store.SettingsStore, err error) {
	previous := map[string]any{}
	if raw, getErr := store.Get(backupWebdavStateSettingKey); getErr == nil && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &previous)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	state := map[string]any{
		"lastSyncAt":    previous["lastSyncAt"],
		"lastAttemptAt": now,
		"lastError":     nil,
	}
	if err != nil {
		state["lastError"] = err.Error()
	} else {
		state["lastSyncAt"] = now
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
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" || parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return isAllowedBackupWebdavTargetHost(parsed.Hostname())
}

func isAllowedBackupWebdavTargetHost(host string) bool {
	if allowPrivateBackupWebdavTargets {
		return true
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.Contains(host, "%") {
		return false
	}
	lower := strings.TrimSuffix(strings.ToLower(host), ".")
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return !isUnsafeBackupWebdavAddr(addr)
	}
	return true
}

func isUnsafeBackupWebdavAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast()
}
