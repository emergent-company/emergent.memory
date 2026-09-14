# Connector: accept numeric JSON-RPC request ids

**Status:** proposed
**Created:** 2026-09-11
**Source:** [2026-09-11-source-audit-hardening](../sessions/2026-09-11-source-audit-hardening.md)

## What

`connector/internal/relay/frames.go` decodes `RequestFrame.ID` as a `string`. A JSON-RPC
request whose `id` is a number (e.g. `1`, `2`) therefore fails to unmarshal and is dropped,
so the connector never answers it. `ResponseFrame` already uses `json.RawMessage`; make the
request id type-agnostic (or `json.RawMessage`) and echo it back verbatim.

## Why

The relay is a JSON-RPC peer; numeric ids are legal and common. Silently dropping such
requests means tools appear to hang. (Identified during the 2026-09-11 source audit; not
fixed then because it touches the wire contract between the hub and the connector.)

## Depends on

- Hub-side request builder must be confirmed to send ids the connector can correlate
  (it may always send string ids today, in which case this is a latent-only bug).

## Notes

- Read both ends (`connector/internal/relay/client.go` and the gateway hub relay code)
  before changing the frame type; update `frames_test.go` accordingly.
- Keep backwards compatibility: a string id must still round-trip byte-for-byte.
