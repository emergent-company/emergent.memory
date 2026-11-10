package mcpregistry

import (
	"os"
	"regexp"
	"testing"
)

// TestServerTypeContractCoversEveryType is the exhaustive classification table.
// Every MCPServerType must be listed in allServerTypes and given exactly one
// contract; the zero contract (unknown type) denies every registry-managed
// operation, so a type forgotten by the switch cannot fall through to the
// external proxy/CRUD path.
func TestServerTypeContractCoversEveryType(t *testing.T) {
	want := map[MCPServerType]struct {
		registryManaged  bool
		inProcess        bool
		listed           bool
		syntheticInspect bool
		remote           bool
	}{
		ServerTypeBuiltin: {inProcess: true, listed: true, syntheticInspect: true},
		ServerTypeStdio:   {registryManaged: true, listed: true},
		ServerTypeSSE:     {registryManaged: true, listed: true, remote: true},
		ServerTypeHTTP:    {registryManaged: true, listed: true, remote: true},
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
		if got.listed != expected.listed {
			t.Errorf("%q listed = %v, want %v", typ, got.listed, expected.listed)
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
	if got, wantListed := listedServerTypes(), []MCPServerType{ServerTypeBuiltin, ServerTypeStdio, ServerTypeSSE, ServerTypeHTTP}; !equalTypes(got, wantListed) {
		t.Errorf("listedServerTypes = %v, want %v", got, wantListed)
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
	if c.known || c.registryManaged || c.inProcess || c.listed || c.syntheticInspect || c.remote {
		t.Fatalf("unknown type grants a capability: %+v", c)
	}
}

// TestServerTypeConstantsMatchEntity guards the gap the length check in
// TestServerTypeContractCoversEveryType cannot see: a constant added to
// entity.go but forgotten in both allServerTypes() and the test's want map
// would keep the lengths equal. Grepping the source closes that hole: every
// `X MCPServerType = "..."` literal must be in allServerTypes() and vice versa.
func TestServerTypeConstantsMatchEntity(t *testing.T) {
	src, err := os.ReadFile("entity.go")
	if err != nil {
		t.Fatalf("read entity.go: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s*(\w+)\s+MCPServerType\s*=\s*"([^"]+)"`)
	declared := make(map[MCPServerType]string)
	for _, match := range re.FindAllStringSubmatch(string(src), -1) {
		typ := MCPServerType(match[2])
		if prev, ok := declared[typ]; ok {
			t.Fatalf("entity.go declares %q twice (%s and %s)", typ, prev, match[1])
		}
		declared[typ] = match[1]
	}
	if len(declared) == 0 {
		t.Fatal("no MCPServerType literals found in entity.go — the scan pattern is stale")
	}

	known := make(map[MCPServerType]bool, len(allServerTypes()))
	for _, typ := range allServerTypes() {
		known[typ] = true
	}
	for typ, name := range declared {
		if !known[typ] {
			t.Errorf("entity.go declares %s = %q but allServerTypes() omits it", name, typ)
		}
	}
	for _, typ := range allServerTypes() {
		if _, ok := declared[typ]; !ok {
			t.Errorf("allServerTypes() lists %q but entity.go declares no MCPServerType constant for it", typ)
		}
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
