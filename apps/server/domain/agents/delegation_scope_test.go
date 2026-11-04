package agents

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

func scopedCtx(objectTypes, relationshipTypes []string) context.Context {
	return mcp.ContextWithTypeScope(context.Background(), mcp.NewTypeScope(objectTypes, relationshipTypes))
}

// TestPermitsDelegation pins the delegation guard semantics: a scoped parent may
// only delegate to a target whose declared allowlists do not widen its own
// (#1695).
func TestPermitsDelegation(t *testing.T) {
	t.Run("nil parent permits anything", func(t *testing.T) {
		var scope *mcp.TypeScope
		if !scope.PermitsDelegation(nil, nil) {
			t.Fatal("nil scope must permit any delegation")
		}
		if !scope.PermitsDelegation([]string{"Law"}, []string{"CITES"}) {
			t.Fatal("nil scope must permit a restricted target")
		}
	})

	t.Run("unrestricted parent permits anything", func(t *testing.T) {
		scope := mcp.NewTypeScope(nil, nil)
		if scope != nil {
			t.Fatalf("NewTypeScope(nil,nil) = %#v, want nil", scope)
		}
	})

	t.Run("object axis requires a subset", func(t *testing.T) {
		scope := mcp.NewTypeScope([]string{"Law", "Regulation"}, nil)
		if !scope.PermitsDelegation([]string{"Law"}, []string{"CITES"}) {
			t.Fatal("subset object list should be permitted (relationship axis unrestricted)")
		}
		if !scope.PermitsDelegation([]string{"Law", "Regulation"}, nil) {
			t.Fatal("equal object list should be permitted")
		}
		if scope.PermitsDelegation([]string{"Person"}, nil) {
			t.Fatal("excluded object type must be refused")
		}
		if scope.PermitsDelegation([]string{"Law", "Person"}, nil) {
			t.Fatal("object list with an excluded extra type must be refused")
		}
		if scope.PermitsDelegation(nil, nil) {
			t.Fatal("unrestricted object list widens a restricted parent and must be refused")
		}
	})

	t.Run("relationship axis requires a subset", func(t *testing.T) {
		scope := mcp.NewTypeScope(nil, []string{"CITES"})
		if !scope.PermitsDelegation(nil, []string{"CITES"}) {
			t.Fatal("equal relationship list should be permitted (object axis unrestricted)")
		}
		if scope.PermitsDelegation(nil, []string{"AMENDS"}) {
			t.Fatal("excluded relationship type must be refused")
		}
		if scope.PermitsDelegation(nil, nil) {
			t.Fatal("unrestricted relationship list widens a restricted parent and must be refused")
		}
	})

	t.Run("axes are independent", func(t *testing.T) {
		scope := mcp.NewTypeScope([]string{"Law"}, []string{"CITES"})
		if !scope.PermitsDelegation([]string{"Law"}, []string{"CITES"}) {
			t.Fatal("both axes within subset should be permitted")
		}
		if scope.PermitsDelegation([]string{"Law"}, nil) {
			t.Fatal("widening the relationship axis must be refused even if objects are a subset")
		}
		if scope.PermitsDelegation(nil, []string{"CITES"}) {
			t.Fatal("widening the object axis must be refused even if relationships are a subset")
		}
	})
}

// TestDelegationDeniedByTypeScope pins the guard used by trigger_agent,
// spawn_agents, and call_agent (#1695).
func TestDelegationDeniedByTypeScope(t *testing.T) {
	child := &AgentDefinition{Config: map[string]any{
		objectTypesConfigKey: []string{"Law"},
	}}
	wider := &AgentDefinition{Config: map[string]any{
		objectTypesConfigKey: []string{"Law", "Person"},
	}}
	unrestricted := &AgentDefinition{}

	t.Run("no caller scope permits any target", func(t *testing.T) {
		if delegationDeniedByTypeScope(context.Background(), wider) {
			t.Fatal("unscoped caller must not be denied")
		}
		if delegationDeniedByTypeScope(context.Background(), nil) {
			t.Fatal("unscoped caller must not be denied a nil target")
		}
	})

	t.Run("scoped caller permits a subset target", func(t *testing.T) {
		ctx := scopedCtx([]string{"Law", "Regulation"}, nil)
		if delegationDeniedByTypeScope(ctx, child) {
			t.Fatal("subset target must be permitted")
		}
	})

	t.Run("scoped caller denies a wider or unrestricted target", func(t *testing.T) {
		ctx := scopedCtx([]string{"Law"}, nil)
		if !delegationDeniedByTypeScope(ctx, wider) {
			t.Fatal("wider target must be denied")
		}
		if !delegationDeniedByTypeScope(ctx, unrestricted) {
			t.Fatal("unrestricted target must be denied")
		}
		if !delegationDeniedByTypeScope(ctx, nil) {
			t.Fatal("nil target must be denied")
		}
	})
}
