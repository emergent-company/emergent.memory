#!/usr/bin/env bash
# Swagger raw-JSON guard (issue #162).
#
# swag cannot type a json.RawMessage: absent a swaggertype tag it emits
# `"items": {"type": "integer"}`, so a generated client decodes the value as
# []int and fails on the first real object. That lie has now been fixed by hand
# three times (#156, #158/#159) and reintroduced twice in the same sitting
# (#136 added MergePreviewPack.ObjectTypes, #164 added three objectTypes
# aliases). Reviewers only catch it when a PR happens to regenerate swagger.
#
# This guard has two halves:
#
#   1. Source-level (primary): every json.RawMessage field with a `json:` tag in
#      the schema request/response structs must also carry a `swaggertype` whose
#      value is one of the object-shaped forms (`object` or `array,object`). It
#      fails while the author is editing the struct, before the spec is
#      regenerated, and names the field, the value found and the accepted values.
#   2. Spec-level (secondary): the committed swagger.json must not render any
#      of those fields as an array of integers. This catches a present-but-wrong
#      tag that happens to render the item type, which the source check alone
#      cannot: presence is not correctness.
#
# The scope is the schema-pack payload structs fixed by #156/#159/#161/#164
# plus the schemaregistry request/response DTOs that #162 names. Together they
# are the schema types published in swagger definitions. Definition-published
# types are the rule: ProjectObjectSchemaRegistryVersion is deliberately
# excluded because it is only registered for schema-drift
# (internal/schemadrift/models.go:135), never a swagger definition, even though
# its raw-JSON fields mirror ObjectTypeVersionDTO's. A domain-wide gate is
# still avoided: the tree has 86 json.RawMessage fields with json tags (68 with
# no swaggertype) across 40+ packages, most unrelated to schemas. The in-scope
# fields that are still pre-existing debt are pinned in the allowlist with a
# reason and a follow-up issue (#170), so the list can only shrink.
#
# Allowlist entries live in scripts/preflight/swagger-raw-json.allow, one
# reason-pinned key per line. An entry is only valid while its field still
# exists and still lacks the declaration, so removing a fix without removing
# the entry (or adding an entry for a field that is already typed) fails. That
# makes adding a waiver a visible, deliberate diff.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

swagger_json="apps/server/docs/swagger/swagger.json"
allow_file="scripts/preflight/swagger-raw-json.allow"

if [ ! -f "$swagger_json" ]; then
  echo "swagger raw-JSON guard: $swagger_json is missing; regenerate the spec first" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "swagger raw-JSON guard: jq is required" >&2
  exit 1
fi

# <file> <definition package prefix> <Type...>
#
# Only schema request/response structs are in scope:
#   - the stored pack row and the installed-pack row (#161)
#   - the create/update/merge request and merge-response payloads
#     (#156/#159/#164)
#   - the nested conflict DTOs those responses return
#   - the schemaregistry type create/update/version/entry DTOs and the
#     registered-type row that #162 names
scope='
apps/server/domain/schemas/entity.go domain_schemas GraphMemorySchema InstalledSchemaItem CreatePackRequest UpdatePackRequest MergePreviewPack MergePreviewConflict SchemaConflict PropertyConflict ObjectTypeSchema
apps/server/domain/schemaregistry/dto.go domain_schemaregistry SchemaRegistryEntryDTO ObjectTypeVersionDTO CreateTypeRequest UpdateTypeRequest
apps/server/domain/schemaregistry/entity.go domain_schemaregistry GraphMemorySchema ProjectObjectSchemaRegistry
'

declare -A allow_reason=()
declare -A allow_seen=()

if [ -f "$allow_file" ]; then
  line_no=0
  while IFS= read -r line; do
    line_no=$((line_no + 1))
    case "$line" in
      ''|'#'*) continue ;;
    esac
    if [[ "$line" =~ ^([^[:space:]]+)[[:space:]]+--[[:space:]]+(.+)$ ]]; then
      key="${BASH_REMATCH[1]}"
      reason="${BASH_REMATCH[2]}"
      if [ -n "${allow_reason[$key]:-}" ]; then
        echo "swagger raw-JSON guard: duplicate allowlist entry for $key ($allow_file:$line_no)" >&2
        exit 1
      fi
      allow_reason["$key"]="$reason"
      allow_seen["$key"]=0
    else
      echo "swagger raw-JSON guard: malformed allowlist entry at $allow_file:$line_no" >&2
      echo "  expected: <path>:<Type>.<jsonField>  -- <reason>" >&2
      echo "  got:      $line" >&2
      exit 1
    fi
  done < "$allow_file"
fi

failures=0
checked=0
allowlisted=0

report_source() {
  local file="$1" type="$2" field="$3" line_no="$4"
  echo "  ${file}:${line_no}: ${type}.${field} is json.RawMessage but has no swaggertype tag." >&2
  echo "      swag renders it as items:{type:integer}. Declare the shape on the field:" >&2
  echo '        swaggertype:"object"         JSON object / map keyed by name' >&2
  echo '        swaggertype:"array,object"   JSON array of objects' >&2
  echo "      or pin it in ${allow_file} with a reason." >&2
}

report_bad_value() {
  local file="$1" type="$2" field="$3" line_no="$4" value="$5"
  echo "  ${file}:${line_no}: ${type}.${field} has swaggertype:\"${value}\", which does not declare an object-shaped value." >&2
  echo "      found:    swaggertype:\"${value}\"" >&2
  echo '      accepted: swaggertype:"object" (JSON object / map) or swaggertype:"array,object" (JSON array of objects)' >&2
  echo "      Every in-scope field holds an object or an array of objects; pick the matching form." >&2
}

accepted_swaggertype() {
  case "$1" in
    object|array,object) return 0 ;;
    *) return 1 ;;
  esac
}

while read -r file pkg types; do
  [ -z "${file:-}" ] && continue
  if [ ! -f "$file" ]; then
    echo "swagger raw-JSON guard: scoped file $file is missing" >&2
    failures=$((failures + 1))
    continue
  fi

  # Emit one tab-separated record per raw-JSON field in a scoped struct:
  #   Type <TAB> jsonName <TAB> line <TAB> swaggertypeValue (empty when absent)
  fields="$(awk -v types="$types" '
    BEGIN {
      n = split(types, parts, " ")
      for (i = 1; i <= n; i++) want[parts[i]] = 1
      intype = ""
    }
    /^type[[:space:]]+[A-Za-z0-9_]+[[:space:]]+struct[[:space:]]*\{/ {
      name = $2
      intype = (name in want) ? name : ""
      next
    }
    intype != "" && /^}/ {
      intype = ""
      next
    }
    intype != "" && /json\.RawMessage/ && /json:"/ {
      line = $0
      if (match(line, /json:"[^"]+"/)) {
        tag = substr(line, RSTART + 6, RLENGTH - 7)
        split(tag, tagparts, ",")
        swag = ""
        if (match(line, /swaggertype:"[^"]*"/)) {
          swag = substr(line, RSTART + 13, RLENGTH - 14)
        }
        print intype "\t" tagparts[1] "\t" NR "\t" swag
      }
    }
  ' "$file")"

  [ -z "$fields" ] && continue

  while IFS=$'\t' read -r type field line_no swagvalue; do
    [ -z "${type:-}" ] && continue
    checked=$((checked + 1))
    key="${file}:${type}.${field}"

    if [ -n "${allow_reason[$key]:-}" ]; then
      allow_seen["$key"]=1
      if [ -n "$swagvalue" ]; then
        echo "  ${allow_file}: unnecessary allowlist entry ${key}" >&2
        echo "      Its field already carries swaggertype:\"${swagvalue}\". Remove the entry in this change." >&2
        failures=$((failures + 1))
        continue
      fi
      allowlisted=$((allowlisted + 1))
      continue
    fi

    if [ -z "$swagvalue" ]; then
      report_source "$file" "$type" "$field" "$line_no"
      failures=$((failures + 1))
      continue
    fi
    if ! accepted_swaggertype "$swagvalue"; then
      report_bad_value "$file" "$type" "$field" "$line_no" "$swagvalue"
      failures=$((failures + 1))
      continue
    fi

    # Spec-level assertion: a valid declaration must still render a non-integer
    # item type. Missing definition/property is skipped (the struct may not be
    # reachable from an annotated handler); the drift guard covers staleness.
    definition="domain_${pkg#domain_}.${type}"
    if ! jq -e --arg d "$definition" --arg f "$field" '
      (.definitions[$d].properties[$f]) as $p
      | if $p == null then true
        else (((($p.type // "") == "array") and (($p.items.type // "") == "integer")) | not)
        end
    ' "$swagger_json" >/dev/null; then
      echo "  ${swagger_json}: ${definition}.${field} still renders items:{type:integer}." >&2
      echo "      A swaggertype is present but does not declare an object-shaped value; fix the tag." >&2
      failures=$((failures + 1))
    fi
  done <<< "$fields"
done <<< "$scope"

# A waiver may not outlive the field it waives. If an entry no longer matches
# an in-scope untyped field, the field was fixed (or removed) and the entry must
# be removed in the same diff; otherwise the next raw-JSON field could inherit
# a stale pass.
for key in "${!allow_seen[@]}"; do
  if [ "${allow_seen[$key]}" -eq 0 ]; then
    echo "  ${allow_file}: stale allowlist entry ${key}" >&2
    echo "      Its field is no longer an in-scope untyped json.RawMessage. Remove the entry." >&2
    failures=$((failures + 1))
  fi
done

if [ "$failures" -ne 0 ]; then
  echo "" >&2
  echo "swagger raw-JSON guard: $failures problem(s) across $checked in-scope json.RawMessage fields." >&2
  echo "A json.RawMessage without a swaggertype is published to generated clients as []int." >&2
  exit 1
fi

echo "swagger raw-JSON guard: OK ($checked in-scope json.RawMessage fields typed or declared; ${allowlisted} allowlisted; no items:{type:integer} for them)"
