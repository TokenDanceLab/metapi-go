package admin

import (
	"database/sql"
	"testing"

	"github.com/jmoiron/sqlx"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRebindAdminQueryForPostgres(t *testing.T) {
	sqlx.BindDriver("pgx", sqlx.DOLLAR)
	dsn := "postgres" + "://postgres:test@localhost:5432/metapi_test?sslmode=disable"
	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open pgx handle: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	db := sqlx.NewDb(rawDB, "pgx")

	query := "SELECT * FROM token_routes WHERE id = ? AND model_pattern LIKE ? LIMIT ?"
	got := rebindAdminQuery(db, query)
	want := "SELECT * FROM token_routes WHERE id = $1 AND model_pattern LIKE $2 LIMIT $3"
	if got != want {
		t.Fatalf("rebindAdminQuery() = %q, want %q", got, want)
	}
}
