package mcpregistry

import "testing"

// TestServerTypeContractCoversEveryType is the exhaustive classification table.
// Every MCPServerType must be listed in allServerTypes and given exactly one
// contract; the zero contract (unknown type) denies every registry-managed
// operation, so a type forgotten by the switch cannot fall through to the
// external proxy/CRUD path.
func TestServerTypeContractCoversEveryType(t *testing.T) {
	want := map[MCPServerType]struct {
		registryManaged  bool
		inProcess        bool
		syntheticInspect bool
		remote           bool
	}{
		ServerTypeBuiltin: {inProcess: true, syntheticInspect: true},
		ServerTypeStdio:   {registryManaged: true},
		ServerTypeSSE:     {registryManaged: true, remote: true},
		ServerTypeHTTP:    {registryManaged: true, remote: true},
		ServerTypeSource:  {inProcess: true},
	}

	types := allServerTypes()
	if len(types) != len(want) {
		t.Fatalf("allServerTypes has %d types, want %d — add the new constant to both allServerTypes and contract()", len(types), len(want))
	}
	seen := make(map[MCPServerType]bool, len(types))
	for _, typ := range types {
		if seen[typ] {
			t.Fatalf("allServerTypes lists %q twice", typ)
		}
		seen[typ] = true

		expected, ok := want[typ]
		if !ok {
			t.Fatalf("allServerTypes contains unknown type %q with no expected contract", typ)
		}
		got := typ.contract()
		if !got.known {
			t.Errorf("%q contract.known = false, want true", typ)
		}
		if got.registryManaged != expected.registryManaged {
			t.Errorf("%q registryManaged = %v, want %v", typ, got.registryManaged, expected.registryManaged)
		}
		if got.inProcess != expected.inProcess {
			t.Errorf("%q inProcess = %v, want %v", typ, got.inProcess, expected.inProcess)
		}
		if got.syntheticInspect != expected.syntheticInspect {
			t.Errorf("%q syntheticInspect = %v, want %v", typ, got.syntheticInspect, expected.syntheticInspect)
		}
		if got.remote != expected.remote {
			t.Errorf("%q remote = %v, want %v", typ, got.remote, expected.remote)
		}
	}
	for typ := range want {
		if !seen[typ] {
			t.Errorf("type %q has an expected contract but is missing from allServerTypes", typ)
		}
	}

	// stdio/sse/http are exactly the registry-managed set; builtin/source the
	// in-process set. The SQL filters derive from these helpers.
	if got, wantManaged := registryManagedServerTypes(), []MCPServerType{ServerTypeStdio, ServerTypeSSE, ServerTypeHTTP}; !equalTypes(got, wantManaged) {
		t.Errorf("registryManagedServerTypes = %v, want %v", got, wantManaged)
	}
	if got, wantInProcess := inProcessServerTypes(), []MCPServerType{ServerTypeBuiltin, ServerTypeSource}; !equalTypes(got, wantInProcess) {
		t.Errorf("inProcessServerTypes = %v, want %v", got, wantInProcess)
	}
}

// TestUnknownServerTypeDeniesEveryPath pins the fail-safe default: a type the
// build does not define is never creatable, deletable, inspectable, callable or
// dialed outbound.
func TestUnknownServerTypeDeniesEveryPath(t *testing.T) {
	unknown := MCPServerType("future-type")
	if got := unknown.contract(); got != (serverTypeContract{}) {
		t.Fatalf("unknown type contract = %+v, want the zero value", got)
	}
	c := unknown.contract()
	if c.known || c.registryManaged || c.inProcess || c.syntheticInspect || c.remote {
		t.Fatalf("unknown type grants a capability: %+v", c)
	}
}

func equalTypes(a, b []MCPServerType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
