# Minimax thinking separation (#52 / upstream #511)

## Behavior
- Inline `<think>...</think>` extraction separates reasoning from assistant content.
- Stream parser emits confirmed content immediately; only partial tag suffixes buffer.
- Open-but-unclosed think blocks emit progressive reasoning (not only on close).
- Orphan `</think>` (open tag omitted) treats prefix as reasoning — MiniMax common shape.
- `reasoning_details` / `reasoning_detail` nested text is folded into reasoning candidates.

## Residual
- Provider-specific non-tag reasoning formats beyond reasoning_details may need more fixtures.
