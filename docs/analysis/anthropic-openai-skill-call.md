# Anthropic ↔ OpenAI skill-call bridge

Last updated: 2026-07-17

Issue: TokenDanceLab/metapi-go#51  
Upstream: cita-777/metapi#531

## Problem (Claude Code repro)

Claude Code expects Anthropic Messages `tool_use` for the built-in **Skill** tool:

```json
{
  "type": "tool_use",
  "id": "toolu_…",
  "name": "Skill",
  "input": { "skill": "superpowers:using-superpowers" }
}
```

When MetAPI routes Claude Code traffic to an OpenAI-compatible upstream (or reconstructs multi-turn history through OpenAI chat shapes), a broken bridge can return:

```json
{
  "type": "tool_use",
  "name": "Skill",
  "input": {}
}
```

Upstream #531 control group:

| Path | Skill call |
|------|------------|
| Claude Code → Claude-compatible model | pass (input present) |
| Claude Code → MetAPI → GPT/OpenAI-compatible | fail (`input:{}`) before this fix |

Static localization (no live runtime required for the transform gap):

1. OpenAI `role=tool` history was not converted to Anthropic `tool_result` on the OpenAI→Anthropic path (multi-turn Skill history dropped linkage).
2. Tool arguments emitted as objects (not JSON strings) could be dropped by stream helpers that only accepted strings.
3. Tool schema `parameters.required` / `input_schema.required` must survive Claude↔OpenAI tool conversion so models still see that `skill` is required.

## Implementation (metapi-go)

| Surface | Behavior |
|---------|----------|
| `transform/anthropic/messages/conversion.go` `ConvertOpenAiBodyToAnthropicMessagesBody` | Maps assistant `tool_calls` → `tool_use` (id/name/input). Coalesces consecutive OpenAI `role=tool` rows into user `tool_result` blocks (optional next user text appended). |
| same file `convertOpenAiToolsToAnthropic` | Preserves full function `parameters` as Anthropic `input_schema` (incl. `required`). |
| same file `sanitizeAnthropicContentBlock` | Keeps `tool_use` / `server_tool_use` id/name/input; preserves `tool_result.is_error`. |
| `transform/shared/chatFormatsCore.go` Claude→OpenAI | `tool_use` and `server_tool_use` become OpenAI `tool_calls`; arguments coerced via `coerceToolArgumentsText`. |
| same package normalize/stream | OpenAI stream `function.arguments` objects preserved; Claude `input_json_delta` still accumulates. Final Claude serialize uses `ParseJSONLike` on accumulated arguments. |
| `transform/canonical/openai_bridge.go` | Canonical tool_call `ArgumentsJSON` preserves string or object args; tool schemas keep `required`. |

## Round-trip contract

1. **OpenAI → Anthropic (request / history)**  
   `tool_calls[].id|function.name|function.arguments` → `tool_use.id|name|input`  
   `role=tool.tool_call_id|content` → `tool_result.tool_use_id|content`

2. **Anthropic → OpenAI (Claude Code inbound to OpenAI upstream)**  
   `tool_use` / `server_tool_use` → `tool_calls`  
   `tool_result` → `role=tool`  
   `input_schema` → `function.parameters` (required preserved)

3. **Canonical bridge**  
   Same id/name/arguments linkage through `PartToolCall` / `PartToolResult`.

4. **Response serialize**  
   Normalized `ToolCall.Arguments` JSON → Anthropic `tool_use.input` object for Claude Code clients.

## Residual client / model limits (not fully fixable in proxy)

| Limit | Notes |
|-------|-------|
| Model returns empty Skill arguments | If the upstream OpenAI model emits `function.arguments: "{}"` despite a schema with `required: ["skill"]`, the proxy **preserves** empty `input:{}` rather than inventing a skill name. Claude Code will still see a Skill tool_use block, but skill selection may fail until the model emits parameters. |
| Non-streaming argument object quirks | Proxy now stringifies object arguments; providers that send malformed non-JSON strings still pass through as `{ "value": "…" }` via `ParseJSONLike`. |
| Server tools vs Skill | Anthropic `server_tool_use` (e.g. web_search) is preserved as a tool_call/tool_use shape for bridges; it is not the Claude Code Skill tool, and OpenAI upstreams may not support native server tools. |
| Streaming partial JSON | Skill input is only complete after all `input_json_delta` / argument deltas accumulate. Mid-stream clients that inspect incomplete partial JSON may briefly see incomplete skill names — expected SSE semantics. |
| Live Claude Code end-to-end | Fixtures cover transform round-trips. A full Claude Code UI repro against a specific GPT deployment still depends on model compliance with the Skill schema. |

## Tests

```bash
go test ./transform/anthropic/... ./transform/canonical/ ./transform/shared/ -count=1
```

Coverage highlights:

- OpenAI multi-turn Skill tool_call + tool_result → Anthropic tool_use/tool_result
- Object-shaped `function.arguments` preserved
- Empty `{}` residual preserved (not dropped)
- Claude Skill multi-turn → OpenAI tool_calls + tool role linkage + schema required
- Canonical OpenAI Skill round-trip
- Claude final serialize Skill input

## Out of scope

- `service/oauth/**`, Responses compact multi-turn (#50)
- `web/**`
- Inventing Skill parameters when the upstream model omits them
