package sources

// EnumerationCompleteness is implemented by connectors that can report whether
// their most recent enumeration reached the end of the source — no page/depth/
// byte/deadline cap was hit and no page fetch failed. Connectors opt in; the
// sync worker treats every enumeration as complete otherwise, preserving today's
// delete-on-absence behaviour.
type EnumerationCompleteness interface {
	// EnumerationComplete reports whether the last Enumerate saw everything in
	// scope (true) or was cut short by a cap or a page error (false).
	EnumerationComplete() bool
}

// SourceTombstones is implemented by connectors whose enumeration can identify
// definitive deletions: a canonical URL the crawl confirmed gone (404/410).
// Connectors opt in; without it the worker only deletes on a complete
// enumeration, never on a partial one.
type SourceTombstones interface {
	// Tombstones returns the canonical ids the last Enumerate confirmed deleted.
	Tombstones() []string
}
