package agents

import "testing"

func TestPickEffectiveModel(t *testing.T) {
	cases := []struct {
		name           string
		override       string
		projectDefault string
		want           string
	}{
		{"override wins", "openai/gpt-4o", "deepseek/deepseek-v4-flash", "openai/gpt-4o"},
		{"no override uses project default", "", "deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-flash"},
		{"neither set is empty", "", "", ""},
	}
	for _, tc := range cases {
		if got := pickEffectiveModel(tc.override, tc.projectDefault); got != tc.want {
			t.Errorf("%s: pickEffectiveModel(%q, %q) = %q, want %q", tc.name, tc.override, tc.projectDefault, got, tc.want)
		}
	}
}
