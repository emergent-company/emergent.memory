## Why

Graph object list/query responses return every object's full `properties` JSONB, including large document bodies. On document-heavy projects this dominates latency because the response is serialized and transferred in full, even when the caller never renders those fields.

Measured on dev (project "Norwegian Law", `?type=EUDirective&limit=25&include_total=false`):

| request | time | bytes |
|---|---|---|
| no projection | **2.4–3.1 s** | **4,647,200 (4.6 MB)** |
| `?fields=key` | **0.29–0.34 s** | **15,771** |

The SQL itself runs in ~0.13 ms — the cost is response serialization/transfer (issue #1106). The endpoints already support an **include** projection (`fields`), but not an **exclude** one, which is what list views need (they want every property except the heavy ones).

## What Changes

- Add an optional `exclude_fields` query parameter to `GET /api/graph/objects/search`: a comma-separated list of property keys to drop from each returned object's `properties`.
- Apply the exclusion server-side before the response is serialized, reusing the existing include/exclude projection helper.
- `exclude_fields` and `fields` compose: include is applied first, then exclude is removed.
- The web UI objects browser — which renders only key/type/labels, no properties — requests `exclude_fields=content` so a list page no longer ships document bodies.

## Capabilities

### New Capabilities
- `graph-object-list-projection`: include/exclude property projection on graph object list/query responses.

### Modified Capabilities
<!-- none -->

## Impact

- `apps/server/domain/graph`: query-param parsing in `ListObjects`, `ListParams`, and the projection step in the list service.
- `apps/web-ui/gateway`: `ListGraphObjects` sends `exclude_fields=content`.
- No change to defaults: requests without the parameter return byte-identical responses.

## Scope

- **In scope**: `exclude_fields` on the object list endpoint; the gateway list caller.
- **Out of scope**: pushing projection into the SQL layer (already noted as a TODO in the service), and other endpoints.
