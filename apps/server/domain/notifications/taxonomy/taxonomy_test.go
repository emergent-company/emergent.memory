package taxonomy

import "testing"

func TestRegistryValidity(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Registry {
		if e.Key == "" {
			t.Fatalf("entry with empty key: %+v", e)
		}
		if seen[e.Key] {
			t.Fatalf("duplicate key %q", e.Key)
		}
		seen[e.Key] = true

		if e.Scope != ScopeAccount && e.Scope != ScopeProject {
			t.Fatalf("key %q has invalid scope %q", e.Key, e.Scope)
		}
		switch e.Delivery {
		case DeliveryMandatory, DeliveryRequired, DeliveryOptIn:
		default:
			t.Fatalf("key %q has invalid delivery %q", e.Key, e.Delivery)
		}

		// Account keys must be mandatory; project keys must be required or opt-in.
		if e.Scope == ScopeAccount && e.Delivery != DeliveryMandatory {
			t.Fatalf("account key %q must be mandatory, got %q", e.Key, e.Delivery)
		}
		if e.Scope == ScopeProject && e.Delivery == DeliveryMandatory {
			t.Fatalf("project key %q must not be mandatory", e.Key)
		}

		// Actionable keys must require action.
		if e.Actionable && !e.RequiresAction {
			t.Fatalf("actionable key %q must set RequiresAction", e.Key)
		}
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("task.assigned"); !ok {
		t.Fatal("expected task.assigned to be registered")
	}
	if _, ok := Lookup("does.not.exist"); ok {
		t.Fatal("expected unknown key to be absent")
	}
}

func TestKeys(t *testing.T) {
	proj := Keys(ScopeProject)
	if len(proj) == 0 {
		t.Fatal("expected at least one project key")
	}
	for _, k := range proj {
		e, ok := Lookup(k)
		if !ok || e.Scope != ScopeProject {
			t.Fatalf("Keys(project) returned %q with wrong scope", k)
		}
	}

	acct := Keys(ScopeAccount)
	for _, k := range acct {
		e, _ := Lookup(k)
		if e.Scope != ScopeAccount {
			t.Fatalf("Keys(account) returned %q with wrong scope", k)
		}
	}
}

func TestResolveDelivery(t *testing.T) {
	cases := []struct {
		name        string
		entry       Entry
		prefEnabled bool
		want        bool
	}{
		{"account mandatory delivered even opted out", Entry{Scope: ScopeAccount, Delivery: DeliveryMandatory}, false, true},
		{"account mandatory delivered when opted in", Entry{Scope: ScopeAccount, Delivery: DeliveryMandatory}, true, true},
		{"project required delivered even opted out", Entry{Scope: ScopeProject, Delivery: DeliveryRequired}, false, true},
		{"project required delivered when opted in", Entry{Scope: ScopeProject, Delivery: DeliveryRequired}, true, true},
		{"project opt-in delivered when opted in", Entry{Scope: ScopeProject, Delivery: DeliveryOptIn}, true, true},
		{"project opt-in suppressed when opted out", Entry{Scope: ScopeProject, Delivery: DeliveryOptIn}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveDelivery(tc.entry, tc.prefEnabled); got != tc.want {
				t.Fatalf("ResolveDelivery(%+v, %v) = %v, want %v", tc.entry, tc.prefEnabled, got, tc.want)
			}
		})
	}
}
