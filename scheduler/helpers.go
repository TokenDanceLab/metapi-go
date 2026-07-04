package scheduler

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/store"
)

// stringsTrimLower trims whitespace and lowercases. Returns "active" for empty.
func stringsTrimLower(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return "active"
	}
	return strings.ToLower(t)
}

// formatErr is a shorthand for fmt.Errorf.
func formatErr(f string, args ...any) error {
	return fmt.Errorf(f, args...)
}

// getSqlxDB returns the underlying *sqlx.DB from the store singleton.
// Returns nil if the database is not initialized.
func getSqlxDB() *sqlx.DB {
	db := store.GetDB()
	if db == nil {
		return nil
	}
	return db.DB
}
