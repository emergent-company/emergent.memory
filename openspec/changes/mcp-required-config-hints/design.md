## Context

`mcp-required-config` (merged) resolves required keys from the installed transport's
`isRequired`/`isSecret` declarations and persists them on
`kb.mcp_servers.required_config` as `[{name, kind, isSecret, isRequired}]`. The
registry's `RegistryHeader` and `RegistryEnvVar` types already parse `description`
(and `RegistryHeader` parses the `value` template), but
`collectRegistryRequiredConfig` copies only four fields, so the guidance is lost.

## Goals

- Persist and expose the registry's own guidance for each required key.
- Show it in the configuration UI without any credential ever being prefilled.

## Non-Goals

- Fetching live registry metadata at render time: the guidance is captured at install
  time and persisted, consistent with how required config already works.
- Changing the required/secret resolution rules, the enable gate, or the remote-only
  transport policy.

## Decisions

### Persist, don't re-resolve

`RequiredConfigKey` gains `Description` (both kinds) and `ValueHint` (header only,
from `RegistryHeader.Value`). Both are `omitempty`, so existing rows stay valid and the
JSONB column needs no shape migration — only a refreshed comment (migration
`00229_update_required_config_comment.sql`). Persisting means the hint survives even if
the catalog entry later changes or the live registry is unreachable.

### Value template is a placeholder, never a value

`RegistryHeader.Value` is a template like `Bearer {API_TOKEN}`; it is not a usable
credential. The UI MUST render it as the value input's `placeholder`, never as a
prefilled `value`. This mirrors the existing install behaviour, which stores secret
headers as a named empty placeholder rather than the template.

### Placeholder precedence

When a required row has a `ValueHint`, it wins over the generic state copy
("Enter value" / "Leave blank to keep stored value") because it is strictly more
informative about the expected shape. Rows without a template keep the existing copy.

## Risks / Trade-offs

- Registry descriptions are community-authored and rendered as text. They are escaped
  by templ (not `templ.Raw`), so no HTML injection risk.
- Env vars have no value template in the registry schema, so `ValueHint` is
  header-only; env rows still get the description hint.
