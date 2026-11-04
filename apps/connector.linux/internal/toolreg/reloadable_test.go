package toolreg

import "testing"

// payloadNames extracts the tool names from a ToolsListPayload.
func payloadNames(t *testing.T, payload map[string]any) []string {
	t.Helper()
	raw, ok := payload["tools"]
	if !ok {
		t.Fatalf("payload missing tools key: %v", payload)
	}
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("payload tools type = %T, want []map[string]any", raw)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		names = append(names, name)
	}
	return names
}

func wantNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
}

func TestReloadableSwapReplacesRegistry(t *testing.T) {
	r1 := New()
	mustRegister(t, r1, sampleTool("a"))
	r2 := New()
	mustRegister(t, r2, sampleTool("b"), sampleTool("c"))

	rr := NewReloadable(r1)
	if rr.Current() != r1 {
		t.Fatal("Current() != initial registry")
	}
	if _, ok := rr.Lookup("a"); !ok {
		t.Error("Lookup(a) before swap = missing, want present")
	}
	if _, ok := rr.Lookup("b"); ok {
		t.Error("Lookup(b) before swap = present, want missing")
	}
	wantNames(t, payloadNames(t, rr.ToolsListPayload()), []string{"a"})

	prev := rr.Swap(r2)
	if prev != r1 {
		t.Errorf("Swap returned %p, want previous registry %p", prev, r1)
	}
	if rr.Current() != r2 {
		t.Fatal("Current() != swapped registry")
	}
	if _, ok := rr.Lookup("a"); ok {
		t.Error("Lookup(a) after swap = present, want missing")
	}
	if _, ok := rr.Lookup("b"); !ok {
		t.Error("Lookup(b) after swap = missing, want present")
	}
	wantNames(t, payloadNames(t, rr.ToolsListPayload()), []string{"b", "c"})
}

func TestReloadableNilBacking(t *testing.T) {
	rr := NewReloadable(nil)
	if _, ok := rr.Lookup("anything"); ok {
		t.Error("Lookup with nil backing = present, want missing")
	}
	if names := payloadNames(t, rr.ToolsListPayload()); len(names) != 0 {
		t.Errorf("payload names = %v, want empty", names)
	}

	// Zero value behaves the same (no panic before the first Store).
	var zero Reloadable
	if _, ok := zero.Lookup("x"); ok {
		t.Error("zero Reloadable Lookup = present, want missing")
	}
	if names := payloadNames(t, zero.ToolsListPayload()); len(names) != 0 {
		t.Errorf("zero payload names = %v, want empty", names)
	}
}
