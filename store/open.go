package store

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"

	// Register pure-Go SQLite driver (no CGO).
	_ "modernc.org/sqlite"

	// Register PostgreSQL driver.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Dialect name constants.
const (
	DialectSQLite   = "sqlite"
	DialectPostgres = "postgres"
)

// DB wraps a *sqlx.DB connection pool and provides convenience methods.
type DB struct {
	*sqlx.DB
	Dialect string
}

// ResolveSQLitePath resolves the SQLite database file path.
// Mirrors TS index.ts resolveSqlitePath() behavior:
//   - ":memory:" stays ":memory:"
//   - empty DB_URL defaults to {DATA_DIR}/hub.db
//   - "file://..." / "sqlite://..." prefixes are stripped
//   - Other values are treated as file paths (resolved against cwd).
func ResolveSQLitePath(dbURL string, dataDir string) string {
	raw := strings.TrimSpace(dbURL)
	if raw == "" {
		return filepath.Join(dataDir, "hub.db")
	}
	if raw == ":memory:" {
		return raw
	}
	if strings.HasPrefix(raw, "file://") {
		path := raw[len("file://"):]
		decoded, err := decodeURIPath(path)
		if err == nil {
			return decoded
		}
		return path
	}
	if strings.HasPrefix(raw, "sqlite://") {
		rel := strings.TrimSpace(raw[len("sqlite://"):])
		abs, err := filepath.Abs(rel)
		if err != nil {
			return rel
		}
		return abs
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return raw
	}
	return abs
}

// decodeURIPath decodes percent-encoded characters in a URI path.
func decodeURIPath(path string) (string, error) {
	result := strings.Builder{}
	i := 0
	for i < len(path) {
		if path[i] == '%' && i+2 < len(path) {
			var b byte
			_, err := fmt.Sscanf(path[i:i+3], "%%%02x", &b)
			if err == nil {
				result.WriteByte(b)
				i += 3
				continue
			}
		}
		result.WriteByte(path[i])
		i++
	}
	return result.String(), nil
}

// Open opens a database connection pool for the given dialect and DSN.
//   - "sqlite" uses modernc.org/sqlite (pure Go, no CGO).
//   - "postgres" uses jackc/pgx/v5.
//
// SQLite connections are configured with WAL journal mode and foreign_keys=ON.
func Open(dialect string, dsn string, sslMode bool) (*DB, error) {
	var driverName string
	var connStr string

	switch dialect {
	case DialectSQLite:
		driverName = "sqlite"
		connStr = dsn
	case DialectPostgres:
		driverName = "pgx"
		connStr = dsn
		if sslMode {
			if strings.Contains(connStr, "?") {
				connStr += "&sslmode=require"
			} else {
				connStr += "?sslmode=require"
			}
		}
	default:
		return nil, fmt.Errorf("store: unsupported dialect %q (expected %q or %q)", dialect, DialectSQLite, DialectPostgres)
	}

	sqldb, err := sqlx.Open(driverName, connStr)
	if err != nil {
		return nil, fmt.Errorf("store: failed to open %s database: %w", dialect, err)
	}

	// Bind the pgx driver to DOLLAR ($1, $2, ...) so that all "?"
	// placeholders are automatically rebound for PostgreSQL.
	if driverName == "pgx" {
		sqlx.BindDriver("pgx", sqlx.DOLLAR)
	}

	db := &DB{DB: sqldb, Dialect: dialect}

	// Configure driver-specific settings.
	switch dialect {
	case DialectSQLite:
		db.SetMaxOpenConns(1) // SQLite :memory: databases are per-connection
		if err := applySQLitePragmas(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: failed to apply SQLite pragmas: %w", err)
		}
	case DialectPostgres:
		if err := configurePostgresPool(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: failed to configure Postgres pool: %w", err)
		}
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: failed to ping %s database: %w", dialect, err)
	}

	slog.Info("store: database opened", "dialect", dialect)
	return db, nil
}

// applySQLitePragmas enables WAL mode and foreign key enforcement.
// These are CRITICAL for SQLite correctness (FKs default to OFF in SQLite).
func applySQLitePragmas(db *DB) error {
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("PRAGMA journal_mode=WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("PRAGMA foreign_keys=ON: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		slog.Warn("store: failed to set SQLite busy_timeout", "error", err)
	}
	return nil
}

// configurePostgresPool sets connection pool defaults for PostgreSQL.
// Default pool_max=20 mirrors TS pg.Pool behavior.
func configurePostgresPool(db *DB) error {
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	return nil
}

// EnsureDataDir creates the data directory if it does not exist.
func EnsureDataDir(dataDir string) error {
	if dataDir == "" || dataDir == ":memory:" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(dataDir), 0755)
}
