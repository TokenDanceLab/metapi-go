# P0-585 production / staging cascade e2e procedure (#557)

**Date**: 2026-07-21  
**Issue**: [#557](https://github.com/TokenDanceLab/metapi-go/issues/557)  
**Milestone**: [53 REL-HONESTY](https://github.com/TokenDanceLab/metapi-go/milestone/53)  
**Status**: **procedure + dry-run harness present** · inventory **P0-585 stays partial** until a signed live soak report is attached  

> **Do not** flip residual / matrix P0-585 → **present** from unit tests or `e2e/e2e_p0585_cascade_test.go` alone.  
> **Do not** auto pin/up production (`0.8.45` soak still needs admin auth — server `projects/metapi/STATE.md`).

---

## 1. Why this exists

| Evidence layer | What it proves | Enough to flip present? |
|:---------------|:---------------|:------------------------|
| Unit conductor load-proof | Channel-scoped exclude + MaxAttempts under 5xx storm | **No** |
| HTTP-path e2e (`e2e/e2e_p0585_cascade_test.go`) | Real auth→handler→upstream path with mock multi-channel 5xx / recover | **No** |
| **This procedure + live report** | Same isolation under **staging/prod topology** (multi site/account/credential) | **Required** for stronger claim |

SSOT residual table: [`failover-isolation.md`](./failover-isolation.md).

---

## 2. Preconditions (staging preferred)

1. **Target process running** (`/health` + `/ready` ok).  
2. **At least 2 enabled channels** for the same downstream model pattern (different `channel_id`, preferably different accounts/credentials so usage-limit sibling cool is not confounded).  
3. Operator holds:
   - `PROXY_TOKEN` (or a downstream key) for `POST /v1/chat/completions`
   - Optional `AUTH_TOKEN` for `GET /api/stats/proxy-logs` evidence harvest  
4. **Max attempts** known: env / config `ProxyMaxChannelAttempts` (default product path uses config-driven retries).  
5. **Controlled fault** available (pick one):
   - **A. Staging mock upstream**: route one channel to a local/staging HTTP that always returns **503** / **500**.  
   - **B. Temporary bad endpoint**: set one site API base to a closed port / always-5xx URL, then restore.  
   - **C. Upstream provider test mode** (only if provider offers safe 5xx injection — rare).  

**Forbidden in production without explicit admin approval**: long-lived break of a revenue path; multi-channel site-wide breaker thrash; auto compose pin.

---

## 3. Pass / fail criteria (honest)

### Pass (channel isolation under storm)

| # | Criterion |
|:-:|:----------|
| P1 | Request with multi-channel 5xx storm fails **within** MaxAttempts (does not hang). |
| P2 | Failover `exclude` is **channel IDs only** (no invent site-wide exclude in product code — already unit-locked; live: no silent success on failed-only topology without sibling). |
| P3 | When one channel 5xx and a sibling is healthy: request **succeeds** on sibling; failed channel is not reselected in the same request after exclude. |
| P4 | `proxy_logs` (if collected) show **distinct** channel/account progression for the same client `request_id` / time window when failover happens. |
| P5 | Unrelated sites remain healthy (no forced site-wide outage from single-channel 5xx). |

### Explicitly **not** pass conditions (residual)

| Residual | Live observation may show | Status |
|:---------|:--------------------------|:-------|
| Site/model breaker | After ≥3 transient fails, **all** channels on site/model soft-filtered | Intentional — document, do not “fix” by inventing disable |
| Credential usage-limit cool | Shared credential siblings cool together | Intentional |
| Empty-filter global fallback | When all candidates dirty, full set may reappear | Intentional starvation guard |

---

## 4. Procedure (operator)

### 4.1 Baseline

```bash
# health
curl -sS "$METAPI_BASE_URL/health"
curl -sS "$METAPI_BASE_URL/ready"

# optional: list routes/channels via admin (AUTH_TOKEN)
curl -sS -H "Authorization: Bearer $AUTH_TOKEN" \
  "$METAPI_BASE_URL/api/routes" | head
```

Record: tip/image tag, `ProxyMaxChannelAttempts`, channel IDs for model `M`.

### 4.2 Inject single-channel 5xx (staging)

1. Identify channel **C_bad** and sibling **C_good** for model `M`.  
2. Point **C_bad** upstream at always-5xx (or closed listener).  
3. Keep **C_good** on a working upstream.  
4. Confirm no accidental disable of the whole site unless testing breaker residual separately.

### 4.3 Storm + recover probes

Use the dry-run script (section 5) or manual curls:

```bash
# recover path: expect 200 when C_good works
curl -sS -D- -o /tmp/out.json \
  -H "Authorization: Bearer $PROXY_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"'"$METAPI_PROBE_MODEL"'","messages":[{"role":"user","content":"p0585-recover"}]}' \
  "$METAPI_BASE_URL/v1/chat/completions"
```

Storm (all channels 5xx): expect non-200 within attempt budget; no infinite retry.

### 4.4 Evidence harvest

With `AUTH_TOKEN`:

```bash
curl -sS -H "Authorization: Bearer $AUTH_TOKEN" \
  "$METAPI_BASE_URL/api/stats/proxy-logs?view=full&limit=50"
```

Attach to #557 comment:

- timestamps, model, http status sequence  
- channel/account ids if present in log payload  
- whether recover succeeded after single-channel fault  
- image tag / tip (`/ready` or About)  
- statement: **P0-585 remains partial** unless AC of #557 fully checked

### 4.5 Restore

1. Restore **C_bad** endpoint.  
2. Re-run one successful completion.  
3. Confirm unrelated sites still `/ready` and admin list healthy.

---

## 5. Harness script

Repo script: [`../../scripts/p0585_cascade_probe.py`](../../scripts/p0585_cascade_probe.py)

| Env | Required | Meaning |
|:----|:---------|:--------|
| `METAPI_BASE_URL` | yes for live | e.g. `https://metapi.example` (no trailing slash required) |
| `PROXY_TOKEN` | yes for live | Bearer for `/v1/chat/completions` |
| `METAPI_PROBE_MODEL` | no (default `gpt-4o-mini`) | Model with ≥2 channels in target env |
| `AUTH_TOKEN` | no | If set, fetch recent proxy-logs summary |
| `METAPI_P0585_LIVE` | no | Must be `1` to send real requests; otherwise **dry-run** |
| `METAPI_P0585_REQUESTS` | no (default `3`) | Number of completion probes |

```bash
# dry-run (default) — prints plan, no network side effects beyond optional --check-only health
python scripts/p0585_cascade_probe.py

# live (staging / authorized only)
METAPI_P0585_LIVE=1 \
METAPI_BASE_URL=https://staging.example \
PROXY_TOKEN=sk-... \
METAPI_PROBE_MODEL=gpt-4o-mini \
AUTH_TOKEN=admin-... \
python scripts/p0585_cascade_probe.py
```

Exit codes:

| Code | Meaning |
|-----:|:--------|
| 0 | Dry-run ok, or live probes completed without harness error |
| 2 | Live mode missing env / base URL unreachable |
| 3 | Live probe unexpected (e.g. all success when storm expected is **not** auto-detected — operator must interpret) |

The script **never** flips inventory status and **never** mutates pin/compose.

---

## 6. Automated evidence already in CI

```bash
go test ./e2e/ -count=1 -run 'P0585HTTP' -timeout 60s
```

- `TestP0585HTTP_MultiChannel5xxStorm_ChannelScopedExclude`  
- `TestP0585HTTP_5xxThenHealthySiblingSucceeds`  

These run under pre-push `go test ./... -race` as part of `./e2e`.

---

## 7. Closing bar for inventory

Only after #557 AC checkboxes are complete **and** a live report is linked:

1. Update residual-next P0-585 evidence (still may remain partial if breaker residuals dominate).  
2. Optionally strengthen formal-readiness Track B gate B1 language.  
3. **Still separate**: ops pin 0.8.45 soak (admin auth).

**Honest default if live not run**: keep **partial**.

---

## 8. Related

| Doc / code | Role |
|:-----------|:-----|
| [`failover-isolation.md`](./failover-isolation.md) | Mechanism + residual table |
| [`residual-next-candidates.md`](./residual-next-candidates.md) | Inventory row P0-585 |
| `e2e/e2e_p0585_cascade_test.go` | HTTP automated load-proof |
| `proxy/conductor_test.go` `TestConductor_P0585LoadProof_*` | Unit load-proof |
| server `projects/metapi/STATE.md` | Ops pin SSOT |
