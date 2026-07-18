package store

import (
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

func TestConfigurePostgresPoolAppliesBudget(t *testing.T) {
	raw, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = raw.Close()
	})

	db := &DB{DB: raw, Dialect: DialectPostgres}
	err = configurePostgresPool(db, PostgresPoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("configurePostgresPool() error = %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != 2 {
		t.Fatalf("MaxOpenConnections = %d, want 2", got)
	}
}

func TestConfigurePostgresPoolRejectsBudgetAboveOpen(t *testing.T) {
	raw, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = raw.Close()
	})

	db := &DB{DB: raw, Dialect: DialectPostgres}
	err = configurePostgresPool(db, PostgresPoolConfig{
		MaxOpenConns: 2,
		MaxIdleConns: 3,
	})
	if err == nil {
		t.Fatal("configurePostgresPool() error = nil, want invalid budget")
	}
}

func TestPostgresPoolConfigFromZeroValueRuntimeConfigUsesLegacyDefaults(t *testing.T) {
	got := postgresPoolConfigFromRuntimeConfig(&config.Config{})
	want := DefaultPostgresPoolConfig()
	if got.MaxOpenConns != want.MaxOpenConns || got.MaxIdleConns != want.MaxIdleConns ||
		got.ConnMaxLifetime != want.ConnMaxLifetime || got.ConnMaxIdleTime != want.ConnMaxIdleTime {
		t.Fatalf("pool budget = %#v, want %#v", got, want)
	}
	if got.ApplicationName == "" || !strings.HasPrefix(got.ApplicationName, "metapi-") {
		t.Fatalf("ApplicationName = %q, want metapi-*", got.ApplicationName)
	}
}

func TestPostgresPoolConfigUsesExplicitApplicationName(t *testing.T) {
	got := postgresPoolConfigFromRuntimeConfig(&config.Config{
		DbMaxOpenConns:       4,
		DbMaxIdleConns:       2,
		DbConnMaxLifetimeSec: 1800,
		DbConnMaxIdleTimeSec: 300,
		DbApplicationName:    "metapi-ops",
	})
	if got.MaxOpenConns != 4 || got.MaxIdleConns != 2 {
		t.Fatalf("pool = %d/%d, want 4/2", got.MaxOpenConns, got.MaxIdleConns)
	}
	if got.ApplicationName != "metapi-ops" {
		t.Fatalf("ApplicationName = %q, want metapi-ops", got.ApplicationName)
	}
}

func TestApplyPostgresApplicationNameURL(t *testing.T) {
	got := applyPostgresApplicationName("postgres://u:p@h/db?sslmode=require", "metapi-hk3")
	if !strings.Contains(got, "application_name") {
		t.Fatalf("expected application_name inject, got %q", got)
	}
	keep := applyPostgresApplicationName("postgres://u:p@h/db?application_name=custom", "metapi-hk3")
	if !strings.Contains(keep, "custom") {
		t.Fatalf("existing application_name lost: %q", keep)
	}
}

func TestApplyPostgresApplicationNameKeyword(t *testing.T) {
	got := applyPostgresApplicationName("host=h user=u dbname=d", "metapi-x")
	if got != "host=h user=u dbname=d application_name=metapi-x" {
		t.Fatalf("got %q", got)
	}
}

