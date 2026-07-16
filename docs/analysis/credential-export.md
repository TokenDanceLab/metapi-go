# Credential export adapters (#120)

## API
GET /api/downstream-keys/{id}/export?profile=openai|cherry|generic|all

Admin-only. Returns formatVersion, baseUrl, and profiles.

## Profiles
- openai: shell env for OPENAI_BASE_URL / OPENAI_API_KEY
- cherry: JSON provider block (apiHost + apiKey)
- generic: portable JSON with baseUrl/apiBase/headers

## Security
Only exports a key already readable by the authenticated admin.
Bump formatVersion when profile schema changes.
