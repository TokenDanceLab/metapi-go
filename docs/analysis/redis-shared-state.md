# Optional Redis shared state (#118)

## Config
- `REDIS_URL` or `METAPI_REDIS_URL` (empty = disabled)
- Examples: `redis://127.0.0.1:6379/0`, `redis://:pass@host:6379/1`, `host:6379`

## What is shared
- Downstream-key **RPM** admission counters (`metapi:rpm:{keyID}`) via fixed-window INCR+PEXPIRE.
- Implementation: `internal/sharedcount` (memory + minimal RESP Redis client, no third-party dep).

## Failure mode
- **Fail-open**: if Redis is unreachable or returns errors at request time, admission falls back to process-local sliding window (same as #116).
- Bad `REDIS_URL` at startup disables shared mode and logs a warning.

## Residual
- TPM multi-instance sharing not yet wired.
- Channel cooldown remains DB-backed (`cooldown_until`); not moved to Redis.
- Fixed-window Redis approximation differs slightly from local sliding window.

## Single-node
Leave `REDIS_URL` empty. No Redis process required.
