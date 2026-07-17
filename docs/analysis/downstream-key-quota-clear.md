# Downstream key maxCost/maxRequests clear semantics (#226)

## Contract

| Input on create/update | Stored value | Meaning |
|------------------------|--------------|---------|
| Omitted on **update** | previous value | Partial update preserves quota |
| Omitted on **create** | `NULL` | Unlimited |
| JSON `null` | `NULL` | Unlimited (explicit clear) |
| `0` / negative / `""` | `NULL` | Unlimited (clear) |
| Positive number or numeric string | stored | Hard lifetime cap |

Helpers: `normalizeQuotaFloatOrNull` (`maxCost`), `normalizeQuotaIntOrNull` (`maxRequests`).

Update path uses raw JSON `hasField` so present `null`/`0`/`""` clear, while omitted fields keep existing DB values.

## API surface

- `POST /api/downstream-keys` — set or leave unlimited
- `PUT /api/downstream-keys/{id}` — set, clear, or omit (preserve)

Same null/0 clear applies to `maxRpm` / `maxTpm` via the int helper; focused #226 tests cover lifetime `maxCost` / `maxRequests`.

## Tests

`handler/admin/downstream_keys_test.go`:

- `TestDownstreamKeysQuotaClearNullAndZero` — create → clear null → reset → clear zero
- `TestDownstreamKeysQuotaPartialUpdatePreserves` — name/enabled-only PUT keeps quotas
- `TestDownstreamKeysQuotaCreatePositiveAndClearStringEmpty` — string numbers + empty-string clear
- unit helpers for normalize functions

```bash
go test ./handler/admin -count=1 -run 'DownstreamKeys|Quota'
```
