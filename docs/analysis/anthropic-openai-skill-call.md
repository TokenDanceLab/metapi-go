# Anthropic ↔ OpenAI skill-call bridge (#51 / upstream #531)

## Fix
- OpenAI `role=tool` rows convert to Anthropic `tool_result` blocks (coalesced; next user text appended).
- Assistant `tool_calls` keep id/name/arguments (empty `{}` args preserved as empty maps).
- Tool definitions deep-clone full `parameters` into `input_schema` (required/properties kept).

## Residual client limits
- Claude Code may still reject history if the **client** omits required Skill.input fields entirely and the upstream model never returned them; the proxy preserves empty object args but cannot invent skill parameters.
