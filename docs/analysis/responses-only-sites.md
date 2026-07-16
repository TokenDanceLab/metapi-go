# Responses-only site streaming preference (#56 / upstream #340)

## Operator marks
Custom headers on the site (not forwarded upstream):
- `x-metapi-responses-only: true` — only `/v1/responses`
- `x-metapi-endpoint-preference: responses` — prefer responses first
- `x-metapi-stream-only` / stream preference — force stream on responses

## Platform/URL heuristics
Codex platform and known responses-only relay hosts prefer `/v1/responses`.

## Client policy
Chat/messages clients against responses-only sites are rewritten to responses candidates (or receive a clear unsupported message when transform is impossible).
