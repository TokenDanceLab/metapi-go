package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"

	"github.com/tokendancelab/metapi-go/store"
)

var localSchedulerLeases = struct {
	sync.Mutex
	held map[string]struct{}
}{held: make(map[string]struct{})}

type schedulerLease struct {
	name   string
	key    int64
	conn   *sql.Conn
	local  bool
	closed bool
}

func schedulerLeaseKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("metapi-go:scheduler:"))
	_, _ = h.Write([]byte(name))
	return int64(h.Sum64())
}

func tryAcquireSchedulerLease(ctx context.Context, db *store.DB, name string) (*schedulerLease, bool, error) {
	if db == nil {
		return nil, false, fmt.Errorf("database not available")
	}
	if db.Dialect != store.DialectPostgres {
		return tryAcquireLocalSchedulerLease(name)
	}

	key := schedulerLeaseKey(name)
	conn, err := db.DB.DB.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("open lease connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("acquire postgres advisory lock: %w", err)
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}
	return &schedulerLease{name: name, key: key, conn: conn}, true, nil
}

func tryAcquireLocalSchedulerLease(name string) (*schedulerLease, bool, error) {
	localSchedulerLeases.Lock()
	defer localSchedulerLeases.Unlock()
	if _, exists := localSchedulerLeases.held[name]; exists {
		return nil, false, nil
	}
	localSchedulerLeases.held[name] = struct{}{}
	return &schedulerLease{name: name, local: true}, true, nil
}

func (l *schedulerLease) Release(ctx context.Context) {
	if l == nil || l.closed {
		return
	}
	l.closed = true
	if l.local {
		localSchedulerLeases.Lock()
		delete(localSchedulerLeases.held, l.name)
		localSchedulerLeases.Unlock()
		return
	}
	if l.conn != nil {
		var released bool
		if err := l.conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", l.key).Scan(&released); err != nil {
			slog.Warn("scheduler: failed to release postgres advisory lock", "name", l.name, "error", err)
		}
		if err := l.conn.Close(); err != nil {
			slog.Warn("scheduler: failed to close lease connection", "name", l.name, "error", err)
		}
	}
}

func runWithSchedulerLease(ctx context.Context, db *store.DB, name string, fn func()) {
	lease, acquired, err := tryAcquireSchedulerLease(ctx, db, name)
	if err != nil {
		slog.Error("scheduler: failed to acquire lease", "name", name, "error", err)
		return
	}
	if !acquired {
		slog.Info("scheduler: lease held by another instance, skipping run", "name", name)
		return
	}
	defer lease.Release(ctx)
	fn()
}
