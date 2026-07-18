package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
// Query helpers override sqlx.DB methods to rebind ? placeholders to $N for
// PostgreSQL before queries reach pgx.
type DB struct {
	*sqlx.DB
	Dialect string
}

// PostgresPoolConfig is the application-side connection budget. MaxOpenConns
// must not exceed the production role CONNECTION LIMIT.
type PostgresPoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func DefaultPostgresPoolConfig() PostgresPoolConfig {
	return PostgresPoolConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// Exec executes a query that does not return rows. For PostgreSQL, the query
// string is rebound from ? to $N placeholders before execution.
func (db *DB) Exec(query string, args ...any) (sql.Result, error) {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.Exec(query, args...)
}

// Query executes a query that returns rows. For PostgreSQL, the query string
// is rebound from ? to $N placeholders before execution.
func (db *DB) Query(query string, args ...any) (*sql.Rows, error) {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.Query(query, args...)
}

// QueryRow executes a query that returns at most one row. For PostgreSQL, the
// query string is rebound from ? to $N placeholders before execution.
func (db *DB) QueryRow(query string, args ...any) *sql.Row {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.QueryRow(query, args...)
}

// Queryx executes a query and returns sqlx rows. For PostgreSQL, the query
// string is rebound from ? to $N placeholders before execution.
func (db *DB) Queryx(query string, args ...any) (*sqlx.Rows, error) {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.Queryx(query, args...)
}

// QueryRowx executes a query and returns an sqlx row. For PostgreSQL, the query
// string is rebound from ? to $N placeholders before execution.
func (db *DB) QueryRowx(query string, args ...any) *sqlx.Row {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.QueryRowx(query, args...)
}

// Get loads one row into dest. For PostgreSQL, the query string is rebound
// from ? to $N placeholders before execution.
func (db *DB) Get(dest any, query string, args ...any) error {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.Get(dest, query, args...)
}

// Select loads all rows into dest. For PostgreSQL, the query string is rebound
// from ? to $N placeholders before execution.
func (db *DB) Select(dest any, query string, args ...any) error {
	if db.Dialect == DialectPostgres {
		query = db.Rebind(query)
	}
	return db.DB.Select(dest, query, args...)
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
	mode := ""
	if sslMode {
		mode = "require"
	}
	return OpenWithPostgresSSLMode(dialect, dsn, mode)
}

// OpenWithPostgresSSLMode opens a database connection pool and, for PostgreSQL,
// applies an explicit sslmode without duplicating existing DSN parameters.
func OpenWithPostgresSSLMode(dialect string, dsn string, sslMode string) (*DB, error) {
	return OpenWithPostgresSSLModeAndPool(dialect, dsn, sslMode, DefaultPostgresPoolConfig())
}

// OpenWithPostgresSSLModeAndPool opens a database connection pool and applies
// an explicit PostgreSQL budget. SQLite ignores the PostgreSQL pool settings.
func OpenWithPostgresSSLModeAndPool(
	dialect string,
	dsn string,
	sslMode string,
	pool PostgresPoolConfig,
) (*DB, error) {
	var driverName string
	var connStr string

	switch dialect {
	case DialectSQLite:
		driverName = "sqlite"
		connStr = dsn
	case DialectPostgres:
		driverName = "pgx"
		normalizedSSLMode := normalizePostgresSSLMode(sslMode)
		if sslMode != "" && normalizedSSLMode == "" {
			return nil, fmt.Errorf("store: unsupported postgres sslmode %q", sslMode)
		}
		connStr = applyPostgresSSLMode(dsn, normalizedSSLMode)
	default:
		return nil, fmt.Errorf("store: unsupported dialect %q (expected %q or %q)", dialect, DialectSQLite, DialectPostgres)
	}

	// Bind the pgx driver to DOLLAR ($1, $2, ...) so that all "?"
	// placeholders are automatically rebound for PostgreSQL.
	// Must be called BEFORE sqlx.Open so the DB's BindType is set correctly.
	if driverName == "pgx" {
		sqlx.BindDriver("pgx", sqlx.DOLLAR)
	}

	sqldb, err := sqlx.Open(driverName, connStr)
	if err != nil {
		return nil, fmt.Errorf("store: failed to open %s database: %w", dialect, err)
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
		if err := configurePostgresPool(db, pool); err != nil {
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

func normalizePostgresSSLMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return ""
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return ""
	}
}

func applyPostgresSSLMode(dsn string, mode string) string {
	if mode == "" {
		return dsn
	}
	trimmed := strings.TrimSpace(dsn)
	if strings.HasPrefix(strings.ToLower(trimmed), "postgres://") || strings.HasPrefix(strings.ToLower(trimmed), "postgresql://") {
		parsed, err := url.Parse(trimmed)
		if err == nil {
			q := parsed.Query()
			q.Set("sslmode", mode)
			parsed.RawQuery = q.Encode()
			return parsed.String()
		}
	}

	if postgresKeywordSSLModeRE.MatchString(dsn) {
		return postgresKeywordSSLModeRE.ReplaceAllString(dsn, "${1}sslmode="+mode)
	}
	if strings.TrimSpace(dsn) == "" {
		return "sslmode=" + mode
	}
	return strings.TrimRight(dsn, " ") + " sslmode=" + mode
}

var postgresKeywordSSLModeRE = regexp.MustCompile(`(^|\s)sslmode=\S+`)

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
func configurePostgresPool(db *DB, pool PostgresPoolConfig) error {
	if pool.MaxOpenConns < 1 {
		return fmt.Errorf("max open connections must be >= 1")
	}
	if pool.MaxIdleConns < 0 || pool.MaxIdleConns > pool.MaxOpenConns {
		return fmt.Errorf("max idle connections must be between 0 and max open connections")
	}
	if pool.ConnMaxLifetime < 0 || pool.ConnMaxIdleTime < 0 {
		return fmt.Errorf("connection lifetimes must be >= 0")
	}
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
	return nil
}

// EnsureDataDir creates the data directory if it does not exist.
func EnsureDataDir(dataDir string) error {
	if dataDir == "" || dataDir == ":memory:" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(dataDir), 0755)
}
