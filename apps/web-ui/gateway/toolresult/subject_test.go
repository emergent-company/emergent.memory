package toolresult

import (
	"strings"
	"testing"
)

func TestSubject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "query", input: `{"query": "swiftui", "limit": 5}`, want: "swiftui"},
		{name: "preferred key wins", input: `{"id": "e1", "name": "Alice"}`, want: "Alice"},
		{name: "id alone", input: `{"id": "e1"}`, want: "e1"},
		{name: "path", input: `{"path": "notes/today.md"}`, want: "notes/today.md"},
		{name: "one readable string", input: `{"a": "x", "n": 1}`, want: "x"},
		{name: "two strings name nothing", input: `{"a": "x", "b": "y"}`, want: ""},
		{name: "no strings", input: `{"limit": 10, "offset": 0}`, want: ""},
		{name: "bare string argument", input: `"hello"`, want: "hello"},
		{name: "list is not a subject", input: `["a", "b"]`, want: ""},
		{name: "empty object", input: `{}`, want: ""},
		{name: "null", input: `null`, want: ""},
		{name: "nothing", input: ``, want: ""},
		{name: "padding", input: `{"question": "  which one?  "}`, want: "which one?"},
		{name: "first line of a prompt", input: `{"prompt": "Summarise this\nand that"}`, want: "Summarise this"},
		{name: "collapses whitespace", input: `{"query": "two   words"}`, want: "two words"},
		{name: "blank string is not a subject", input: `{"name": "   "}`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Subject([]byte(tt.input)); got != tt.want {
				t.Errorf("Subject(%s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSubjectIsBounded(t *testing.T) {
	got := Subject([]byte(`{"query": "` + strings.Repeat("q", 200) + `"}`))
	if runes := []rune(got); len(runes) != subjectWidth+1 {
		t.Errorf("subject runes = %d, want %d", len(runes), subjectWidth+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("subject does not mark its cut: %q", got)
	}
}
