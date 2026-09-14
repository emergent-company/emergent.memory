package appletools

import (
	"errors"
	"strings"
	"testing"
)

func TestAppleExpr(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "hello", want: `"hello"`},
		{name: "empty", in: "", want: `""`},
		{name: "double quote", in: `say "hi"`, want: `"say \"hi\""`},
		{name: "backslash", in: `a\b`, want: `"a\\b"`},
		{name: "quote and backslash", in: `x"y\z`, want: `"x\"y\\z"`},
		{name: "single newline", in: "a\nb", want: `("a" & linefeed & "b")`},
		{name: "crlf", in: "a\r\nb", want: `("a" & linefeed & "b")`},
		{name: "cr only", in: "a\rb", want: `("a" & linefeed & "b")`},
		{name: "newline with quote", in: "a\nb\"c", want: `("a" & linefeed & "b\"c")`},
		{name: "trailing newline", in: "a\n", want: `("a" & linefeed & "")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := appleExpr(tc.in); got != tc.want {
				t.Errorf("appleExpr(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestAppleExprNeverContainsRawNewline(t *testing.T) {
	inputs := []string{"line1\nline2", "a\r\nb\r\nc", "ends with newline\n", "no newline here"}
	for _, in := range inputs {
		got := appleExpr(in)
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("appleExpr(%q) contains a raw newline: %q", in, got)
		}
	}
}

func TestPreview(t *testing.T) {
	short := "abc"
	if got := preview(short); got != short {
		t.Errorf("preview(short) = %q", got)
	}
	long := strings.Repeat("x", 500)
	got := preview(long)
	if len([]rune(got)) != 203 { // 200 + "..."
		t.Errorf("preview(long) length = %d, want 203", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("preview(long) = %q should end with ...", got)
	}
}

func TestOffsetExpr(t *testing.T) {
	cases := []struct {
		secs int64
		want string
	}{
		{secs: 0, want: "(current date) + 0"},
		{secs: 3600, want: "(current date) + 3600"},
		{secs: -300, want: "(current date) - 300"},
	}
	for _, tc := range cases {
		if got := offsetExpr(tc.secs); got != tc.want {
			t.Errorf("offsetExpr(%d) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}

func TestDueDateExpr(t *testing.T) {
	// Pure duration-in → expression-out; no clock dependency.
	if got, want := dueDateExpr(90*1e9), "(current date) + 90"; got != want {
		t.Errorf("dueDateExpr(+90s) = %q, want %q", got, want)
	}
	if got, want := dueDateExpr(-1500*1e9), "(current date) - 1500"; got != want {
		t.Errorf("dueDateExpr(-1500s) = %q, want %q", got, want)
	}
}

func TestOptionalHelpers(t *testing.T) {
	args := map[string]any{
		"name":  "x",
		"count": float64(3),
		"n":     float64(3.5),
		"on":    true,
	}
	if got := optionalString(args, "name"); got != "x" {
		t.Errorf("optionalString = %q", got)
	}
	if got := optionalString(args, "missing"); got != "" {
		t.Errorf("optionalString(missing) = %q", got)
	}
	if got := optionalBool(args, "on", false); got != true {
		t.Errorf("optionalBool = %v", got)
	}
	if got := optionalBool(args, "missing", true); got != true {
		t.Errorf("optionalBool default = %v", got)
	}

	if got, err := optionalInt(args, "count", 0); err != nil || got != 3 {
		t.Errorf("optionalInt(float 3) = %d, %v", got, err)
	}
	if _, err := optionalInt(args, "n", 0); err == nil {
		t.Error("optionalInt(3.5) expected error")
	}
	if got, err := optionalInt(args, "missing", 7); err != nil || got != 7 {
		t.Errorf("optionalInt(default) = %d, %v", got, err)
	}
}

func TestArgValidation(t *testing.T) {
	_, err := requireString(nil, "query")
	if err == nil || !strings.Contains(err.Error(), `"query"`) {
		t.Errorf("requireString(nil) = %v, want missing-arg error", err)
	}
	_, err = requireString(map[string]any{"query": nil}, "query")
	if err == nil {
		t.Error("requireString(null) expected error")
	}
	_, err = requireString(map[string]any{"query": 42}, "query")
	if err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("requireString(int) = %v", err)
	}
	_, err = requireString(map[string]any{"query": "  "}, "query")
	if err == nil || !strings.Contains(err.Error(), "not be empty") {
		t.Errorf("requireString(blank) = %v", err)
	}
	if errors.Is(err, ErrNotAuthorized) {
		t.Error("validation error must not be ErrNotAuthorized")
	}
}
