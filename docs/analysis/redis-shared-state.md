# Optional Redis shared state (#118, #245)

## Config
- `REDIS_URL` or `METAPI_REDIS_URL` (empty = disabled)
- Examples: `redis://127.0.0.1:6379/0`, `redis://:pass@host:6379/1`, `host:6379`

## What is shared
- Downstream-key **RPM** admission counters (`metapi:rpm:{keyID}`) via fixed-window `INCR` + `PEXPIRE`.
- Downstream-key **TPM** admission counters (`metapi:tpm:{keyID}`) via fixed-window `INCRBY` + `PEXPIRE` (#245).
- Implementation: `internal/sharedcount` (memory + minimal RESP Redis client, no third-party dep).
- `ConfigureSharedAdmissionFromRedisURL` wires **both** RPM and TPM on the same `RedisCounter` instance (distinct key namespaces).

## Failure mode
- **Fail-open**: if Redis is unreachable or returns errors at request time, admission falls back to process-local sliding window (same as #116).
- Bad `REDIS_URL` at startup disables shared mode and logs a warning.

## Residual
- `Snapshot()` remains **process-local** for both RPM and TPM (admin display may under-report multi-instance usage).
- Channel cooldown remains DB-backed (`cooldown_until`); not moved to Redis.
- Fixed-window Redis approximation differs slightly from local sliding window (both RPM and TPM).

## Single-node
Leave `REDIS_URL` empty. No Redis process required.
