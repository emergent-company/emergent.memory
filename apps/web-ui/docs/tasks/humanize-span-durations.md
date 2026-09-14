# Humanize trace-waterfall span durations

**Status:** done
**Created:** 2026-09-04
**Source:** [2026-09-04-humanize-run-duration](../sessions/2026-09-04-humanize-run-duration.md)

## Resolution (2026-09-09)

`formatSpanDuration` (`gateway/session_trace.go`) now delegates spans ≥ 60s to
`formatRunDuration`, so durations escalate s → m → h → d (75s renders "1m",
2.5h renders "2h 30m"). Sub-second spans keep integer ms precision; seconds
keep 2 decimals. Test cases added in `gateway/session_trace_test.go`.

## What
Apply the human duration unit escalation (`s → m → h → d`) to span durations in the session trace waterfall, replacing or extending `formatSpanDuration` (`gateway/session_trace.go`), which currently renders `1.23s` / `45ms` and would show `75.00s` for spans over a minute.

## Why
Run durations were humanized (see source session) but span durations on the same page still cap at raw seconds, so long spans render inconsistently.

## Depends on
none

## Notes
- `formatSpanDuration` is unit-tested in `gateway/session_trace_test.go` (`TestFormatSpanDuration`); update cases if behavior changes.
- Consider reusing `formatRunDuration` (`gateway/session_dump.go`) or extracting a shared helper. Spans are sub-second to multi-minute, so keep millisecond precision for sub-second spans.
