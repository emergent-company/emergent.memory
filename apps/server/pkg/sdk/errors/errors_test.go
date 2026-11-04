package errors

import "testing"

func TestErrorMissingScope(t *testing.T) {
	e := &Error{
		StatusCode: 403,
		Code:       "forbidden",
		Message:    "Insufficient permissions",
		Details:    map[string]interface{}{"missing": []interface{}{"chat:use"}},
	}
	got := e.Error()
	want := "[403] forbidden: Insufficient permissions (missing scope: chat:use)"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestErrorMissingScopesPlural(t *testing.T) {
	e := &Error{
		StatusCode: 403,
		Code:       "forbidden",
		Message:    "Insufficient permissions",
		Details:    map[string]interface{}{"missing": []interface{}{"chat:use", "graph:read"}},
	}
	got := e.Error()
	want := "[403] forbidden: Insufficient permissions (missing scope: chat:use, graph:read)"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestErrorNoDetails(t *testing.T) {
	e := &Error{StatusCode: 403, Code: "forbidden", Message: "Insufficient permissions"}
	got := e.Error()
	want := "[403] forbidden: Insufficient permissions"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestErrorNoCode(t *testing.T) {
	e := &Error{StatusCode: 500, Message: "boom"}
	if got, want := e.Error(), "[500] boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
