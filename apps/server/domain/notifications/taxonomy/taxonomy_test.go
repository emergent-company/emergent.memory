package taxonomy

import (
	"fmt"
	"testing"
)

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
		case DeliveryMandatory, DeliveryRequired, DeliveryOptIn, DeliveryOptOut:
		default:
			t.Fatalf("key %q has invalid delivery %q", e.Key, e.Delivery)
		}

		// Account keys must be mandatory; project keys must be required, opt-in
		// or opt-out.
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
		name  string
		entry Entry
		pref  *bool
		want  bool
	}{
		{"account mandatory delivered with no preference row", Entry{Scope: ScopeAccount, Delivery: DeliveryMandatory}, nil, true},
		{"account mandatory delivered even explicitly disabled", Entry{Scope: ScopeAccount, Delivery: DeliveryMandatory}, boolPtr(false), true},
		{"project required delivered with no preference row", Entry{Scope: ScopeProject, Delivery: DeliveryRequired}, nil, true},
		{"project required delivered even explicitly disabled", Entry{Scope: ScopeProject, Delivery: DeliveryRequired}, boolPtr(false), true},
		{"project opt-in suppressed with no preference row", Entry{Scope: ScopeProject, Delivery: DeliveryOptIn}, nil, false},
		{"project opt-in delivered when opted in", Entry{Scope: ScopeProject, Delivery: DeliveryOptIn}, boolPtr(true), true},
		{"project opt-in suppressed when opted out", Entry{Scope: ScopeProject, Delivery: DeliveryOptIn}, boolPtr(false), false},
		{"project opt-out delivered with no preference row", Entry{Scope: ScopeProject, Delivery: DeliveryOptOut}, nil, true},
		{"project opt-out delivered when explicitly enabled", Entry{Scope: ScopeProject, Delivery: DeliveryOptOut}, boolPtr(true), true},
		{"project opt-out suppressed when explicitly disabled", Entry{Scope: ScopeProject, Delivery: DeliveryOptOut}, boolPtr(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pref := "no row"
			if tc.pref != nil {
				pref = fmt.Sprintf("%v", *tc.pref)
			}
			if got := ResolveDelivery(tc.entry, tc.pref); got != tc.want {
				t.Fatalf("ResolveDelivery(%+v, pref=%s) = %v, want %v", tc.entry, pref, got, tc.want)
			}
		})
	}
}

func TestDefaultEnabled(t *testing.T) {
	if !DefaultEnabled(Entry{Scope: ScopeProject, Delivery: DeliveryOptOut}) {
		t.Fatal("an opt-out key must default to enabled")
	}
	for _, d := range []Delivery{DeliveryOptIn, DeliveryRequired} {
		if DefaultEnabled(Entry{Scope: ScopeProject, Delivery: d}) {
			t.Fatalf("%s must not default to enabled", d)
		}
	}
}

func boolPtr(b bool) *bool { return &b }
