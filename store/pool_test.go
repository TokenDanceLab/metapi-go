package store

import (
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
	if got != want {
		t.Fatalf("pool = %#v, want %#v", got, want)
	}
}
