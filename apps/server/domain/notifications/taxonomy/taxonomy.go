// Package taxonomy defines the notification event-key registry and the
// delivery-resolution rules that turn a taxonomy entry (plus a user's opt-in
// preference) into a delivered/suppressed decision.
//
// It is a leaf package: it must not import the notifications package so that
// producer domains (e.g. provider) can import it without pulling the whole
// notifications domain into a cycle.
package taxonomy

// Scope classifies which inbox an event belongs to.
type Scope string

const (
	ScopeAccount Scope = "account"
	ScopeProject Scope = "project"
)

// Delivery describes the default delivery behaviour for an event key.
type Delivery string

const (
	// DeliveryMandatory marks account-scope events that are always delivered
	// and never suppressible by preferences.
	DeliveryMandatory Delivery = "mandatory"
	// DeliveryRequired marks project-scope events that are always delivered
	// regardless of preferences (they require the user to act).
	DeliveryRequired Delivery = "required"
	// DeliveryOptIn marks project-scope events delivered only when the user
	// has opted in for that event key in that project.
	DeliveryOptIn Delivery = "optin"
	// DeliveryOptOut marks project-scope events delivered by default and
	// suppressed only when the user has stored a disabled preference. It exists
	// because neither neighbour fits an informational project event: `required`
	// would make routine activity unsubscribable, and `optin` would ship it dark
	// until every member found the settings page.
	DeliveryOptOut Delivery = "optout"
)

// Entry is a single event-key registration in the taxonomy.
type Entry struct {
	Key            string
	Scope          Scope
	Delivery       Delivery
	RequiresAction bool
	Category       string
	Actionable     bool
}

// EventDataSourceReconnectRequired is the event key a parked data source
// announces. It is a named constant because the pivot between the producer
// (the data-source connect/sync path) and the taxonomy table is exactly where a
// typo becomes a silent no-op: Create rejects an unknown key, and the
// project-member fan-out only logs that rejection at warn.
const EventDataSourceReconnectRequired = "data_source.reconnect_required"

// Document lifecycle event keys. They are named constants for the same reason
// as the data-source key above: producers in the documents and extraction
// domains reference them across package boundaries, and an unknown key is
// rejected by Create only to be logged at warn by the project fan-out — a typo
// would silently deliver nothing.
const (
	// EventDocumentUploaded announces a new document created by an upload.
	EventDocumentUploaded = "document.uploaded"
	// EventDocumentVersionSynced announces a new version ingested from a data
	// source sync. A source's first ingest does not announce.
	EventDocumentVersionSynced = "document.version_synced"
	// EventDocumentExtractionCompleted announces that extraction finished and
	// produced objects that were merged. It is not emitted when the run leaves a
	// staging branch for review — that is extraction.review_ready instead.
	EventDocumentExtractionCompleted = "document.extraction_completed"
	// EventDocumentProcessingFailed announces that an upload's parsing or
	// extraction reached dead letter. Retries do not announce.
	EventDocumentProcessingFailed = "document.processing_failed"
)

// Registry is the documented set of stable notification event keys.
var Registry = []Entry{
	// Account scope — always delivered, never suppressible.
	{Key: "project.member.added", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: false, Category: "membership"},
	{Key: "project.member.removed", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: false, Category: "membership"},
	{Key: "user.role.changed", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: false, Category: "permissions"},
	{Key: "user.access.granted", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: false, Category: "permissions"},
	{Key: "user.access.revoked", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: false, Category: "permissions"},
	{Key: "invite.received", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: true, Category: "invites", Actionable: true},
	{Key: "account.usage.budget_alert", Scope: ScopeAccount, Delivery: DeliveryMandatory, RequiresAction: false, Category: "budget"},

	// Project scope — required keys are always delivered; opt-in keys only
	// when the user opts in.
	{Key: "task.assigned", Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: true, Category: "tasks", Actionable: true},
	{Key: "approval.requested", Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: true, Category: "approvals", Actionable: true},
	{Key: "mention", Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: true, Category: "mentions", Actionable: true},
	{Key: "agent.question", Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: true, Category: "agents", Actionable: true},
	{Key: "comment.reply", Scope: ScopeProject, Delivery: DeliveryOptIn, RequiresAction: false, Category: "comments"},
	{Key: "project.agent_config.changed", Scope: ScopeProject, Delivery: DeliveryOptIn, RequiresAction: false, Category: "agents"},
	{Key: "extraction.review_ready", Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: true, Category: "extractions", Actionable: true},
	// A data source parked as needs_reconnect cannot sync until someone repairs
	// its credential, so its remediation is required delivery: the member cannot
	// opt out of being told the fix (#2414).
	{Key: EventDataSourceReconnectRequired, Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: true, Category: "data-sources", Actionable: true},

	// Document lifecycle — informational project activity, on by default so the
	// other members of a shared project learn that the knowledge base changed,
	// and suppressible because a busy document source must not be forced into
	// everyone's inbox. Failures are required: a run that died needs attention.
	{Key: EventDocumentUploaded, Scope: ScopeProject, Delivery: DeliveryOptOut, RequiresAction: false, Category: "documents"},
	{Key: EventDocumentVersionSynced, Scope: ScopeProject, Delivery: DeliveryOptOut, RequiresAction: false, Category: "documents"},
	{Key: EventDocumentExtractionCompleted, Scope: ScopeProject, Delivery: DeliveryOptOut, RequiresAction: false, Category: "documents"},
	{Key: EventDocumentProcessingFailed, Scope: ScopeProject, Delivery: DeliveryRequired, RequiresAction: false, Category: "documents"},
}

// byKey is built once from Registry for O(1) lookups.
var byKey = func() map[string]Entry {
	m := make(map[string]Entry, len(Registry))
	for _, e := range Registry {
		m[e.Key] = e
	}
	return m
}()

// Lookup returns the taxonomy entry for an event key and whether it exists.
func Lookup(key string) (Entry, bool) {
	e, ok := byKey[key]
	return e, ok
}

// Keys returns the event keys for a given scope, in registry order.
func Keys(scope Scope) []string {
	out := make([]string, 0, len(Registry))
	for _, e := range Registry {
		if e.Scope == scope {
			out = append(out, e.Key)
		}
	}
	return out
}

// ResolveDelivery decides whether an event should be delivered given its
// taxonomy entry and the recipient's stored preference for that key.
//
// pref is the stored preference: nil means the user has no row for this event
// key in this project, a non-nil value is the row's enabled flag. The tri-state
// matters because an opt-out key must deliver when unconfigured but suppress
// when explicitly disabled, which a bool cannot express.
//
// Account-scope and mandatory events are always delivered. Required project
// events are always delivered. Opt-out project events are delivered unless
// explicitly disabled. Opt-in project events are delivered only when the user
// has an enabled preference.
func ResolveDelivery(entry Entry, pref *bool) bool {
	if entry.Scope == ScopeAccount {
		return true
	}
	if entry.Delivery == DeliveryRequired {
		return true
	}
	if entry.Delivery == DeliveryOptOut {
		return pref == nil || *pref
	}
	return pref != nil && *pref
}

// DefaultEnabled reports the delivery default for an event key the user has not
// configured: opt-out keys are on, everything else is off. Callers that
// materialise an effective preference list use it so the settings UI shows the
// default the resolver actually applies.
func DefaultEnabled(entry Entry) bool {
	return entry.Delivery == DeliveryOptOut
}
