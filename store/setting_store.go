package store

import (
	"database/sql"
	"fmt"
)

// SettingsStore provides KV read/write access to the settings table.
// The settings table uses a TEXT primary key (key), not SERIAL.
type SettingsStore struct {
	db *DB
}

// NewSettingsStore creates a new SettingsStore backed by the given DB.
func NewSettingsStore(db *DB) *SettingsStore {
	return &SettingsStore{db: db}
}

// Get returns the value for the given key.
// Returns ("", nil) if the key does not exist.
func (s *SettingsStore) Get(key string) (string, error) {
	var value sql.NullString
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("settings: get %q: %w", key, err)
	}
	if !value.Valid {
		return "", nil
	}
	return value.String, nil
}

// Set upserts a key-value pair into the settings table.
func (s *SettingsStore) Set(key, value string) error {
	query := ""
	switch s.db.Dialect {
	case DialectSQLite:
		query = `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`
	case DialectPostgres:
		query = `INSERT INTO settings (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`
	default:
		query = `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`
	}
	_, err := s.db.Exec(query, key, value)
	if err != nil {
		return fmt.Errorf("settings: set %q: %w", key, err)
	}
	return nil
}

// GetAll returns all key-value pairs from the settings table.
func (s *SettingsStore) GetAll() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("settings: get all: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("settings: scan row: %w", err)
		}
		result[key] = ""
		if value.Valid {
			result[key] = value.String
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: rows iteration: %w", err)
	}
	return result, nil
}
