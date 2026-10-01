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
// taxonomy entry and whether the user has opted in.
//
// Account-scope and mandatory events are always delivered. Required project
// events are always delivered. Opt-in project events are delivered only when
// the user has an enabled preference.
func ResolveDelivery(entry Entry, prefEnabled bool) bool {
	if entry.Scope == ScopeAccount {
		return true
	}
	if entry.Delivery == DeliveryRequired {
		return true
	}
	return prefEnabled
}
