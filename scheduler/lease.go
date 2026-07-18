package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tokendancelab/metapi-go/handler/shared"
	"github.com/tokendancelab/metapi-go/store"
)

var localSchedulerLeases = struct {
	sync.Mutex
	held map[string]struct{}
}{held: make(map[string]struct{})}

// leasePressure tracks connection-budget pressure so scheduler ticks do not
// spam Error logs every 30s under SQLSTATE 53300 / pool exhaustion.
var leasePressure = struct {
	mu         sync.Mutex
	until      time.Time
	failures   int
	lastLog    time.Time
	suppressed int
	forceLocal atomic.Bool
}{}

const (
	leaseBackoffBase     = 5 * time.Second
	leaseBackoffMax      = 5 * time.Minute
	leaseLogMinInterval  = 60 * time.Second
	leaseTinyPoolMaxOpen = 2
)

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

	// Tiny pools (MaxOpen ≤ 2) cannot spare a dedicated advisory-lock connection
	// without starving request handlers. Degrade to process-local exclusion:
	// multi-instance safety is lost for this process, but single-node shared-tiny
	// deployments stay quiet and healthy.
	if shouldUseLocalLease(db) {
		return tryAcquireLocalSchedulerLease(name)
	}

	if blocked, remaining := leasePressureActive(); blocked {
		return nil, false, fmt.Errorf("scheduler lease under connection pressure (retry in %s)", remaining.Round(time.Second))
	}

	key := schedulerLeaseKey(name)
	conn, err := db.DB.DB.Conn(ctx)
	if err != nil {
		noteLeaseAcquireFailure(err)
		return nil, false, fmt.Errorf("open lease connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		_ = conn.Close()
		noteLeaseAcquireFailure(err)
		return nil, false, fmt.Errorf("acquire postgres advisory lock: %w", err)
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}
	noteLeaseAcquireSuccess()
	return &schedulerLease{name: name, key: key, conn: conn}, true, nil
}

func shouldUseLocalLease(db *store.DB) bool {
	if leasePressure.forceLocal.Load() {
		return true
	}
	if db == nil || db.DB == nil || db.DB.DB == nil {
		return false
	}
	// sql.DB.Stats().MaxOpenConnections reflects SetMaxOpenConns.
	if maxOpen := db.DB.DB.Stats().MaxOpenConnections; maxOpen > 0 && maxOpen <= leaseTinyPoolMaxOpen {
		return true
	}
	return false
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
		logLeaseAcquireError(name, err)
		return
	}
	if !acquired {
		slog.Info("scheduler: lease held by another instance, skipping run", "name", name)
		return
	}
	defer lease.Release(ctx)
	fn()
}

func leasePressureActive() (bool, time.Duration) {
	leasePressure.mu.Lock()
	defer leasePressure.mu.Unlock()
	if leasePressure.until.IsZero() {
		return false, 0
	}
	remaining := time.Until(leasePressure.until)
	if remaining <= 0 {
		leasePressure.until = time.Time{}
		return false, 0
	}
	return true, remaining
}

func noteLeaseAcquireSuccess() {
	leasePressure.mu.Lock()
	leasePressure.failures = 0
	leasePressure.until = time.Time{}
	leasePressure.mu.Unlock()
}

func noteLeaseAcquireFailure(err error) {
	if !isConnectionBudgetError(err) {
		return
	}
	shared.RecordDBConnError()
	leasePressure.mu.Lock()
	defer leasePressure.mu.Unlock()
	leasePressure.failures++
	// Exponential backoff with jitter: 5s * 2^(n-1), capped.
	exp := float64(leasePressure.failures - 1)
	if exp > 6 {
		exp = 6
	}
	base := float64(leaseBackoffBase) * math.Pow(2, exp)
	if base > float64(leaseBackoffMax) {
		base = float64(leaseBackoffMax)
	}
	// ±20% jitter
	jitter := 0.8 + rand.Float64()*0.4
	delay := time.Duration(base * jitter)
	if delay < leaseBackoffBase {
		delay = leaseBackoffBase
	}
	if delay > leaseBackoffMax {
		delay = leaseBackoffMax
	}
	leasePressure.until = time.Now().Add(delay)
	// After repeated budget failures, force local leases for the rest of the
	// process lifetime so we stop opening extra advisory-lock connections.
	if leasePressure.failures >= 3 {
		leasePressure.forceLocal.Store(true)
	}
}

func logLeaseAcquireError(name string, err error) {
	if err == nil {
		return
	}
	budget := isConnectionBudgetError(err)
	leasePressure.mu.Lock()
	defer leasePressure.mu.Unlock()
	now := time.Now()
	if !leasePressure.lastLog.IsZero() && now.Sub(leasePressure.lastLog) < leaseLogMinInterval {
		leasePressure.suppressed++
		return
	}
	attrs := []any{"name", name, "error", err}
	if leasePressure.suppressed > 0 {
		attrs = append(attrs, "suppressed_since_last_log", leasePressure.suppressed)
		leasePressure.suppressed = 0
	}
	if !leasePressure.until.IsZero() {
		attrs = append(attrs, "backoff_until", leasePressure.until.UTC().Format(time.RFC3339))
	}
	if leasePressure.forceLocal.Load() {
		attrs = append(attrs, "force_local_lease", true)
	}
	if budget {
		slog.Warn("scheduler: lease acquire deferred under connection budget pressure", attrs...)
	} else {
		slog.Error("scheduler: failed to acquire lease", attrs...)
	}
	leasePressure.lastLog = now
}

func isConnectionBudgetError(err error) bool {
	if err == nil {
		return false
	}
	// Walk wrapped errors for text / SQLSTATE fragments from pgx and our own
	// pressure marker.
	var msgs []string
	for e := err; e != nil; e = errors.Unwrap(e) {
		msgs = append(msgs, e.Error())
	}
	text := strings.ToLower(strings.Join(msgs, " | "))
	if strings.Contains(text, "53300") {
		return true
	}
	if strings.Contains(text, "too many connections") {
		return true
	}
	if strings.Contains(text, "connection pressure") {
		return true
	}
	if strings.Contains(text, "remaining connection slots are reserved") {
		return true
	}
	if strings.Contains(text, "sorry, too many clients already") {
		return true
	}
	return false
}

// ResetLeasePressureForTest clears backoff state (tests only).
func ResetLeasePressureForTest() {
	leasePressure.mu.Lock()
	leasePressure.until = time.Time{}
	leasePressure.failures = 0
	leasePressure.lastLog = time.Time{}
	leasePressure.suppressed = 0
	leasePressure.mu.Unlock()
	leasePressure.forceLocal.Store(false)
}
