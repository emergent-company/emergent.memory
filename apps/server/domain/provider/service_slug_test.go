package provider

import (
	"strings"
	"testing"
)

func TestValidateProviderSlug(t *testing.T) {
	cases := []struct {
		name    string
		slug    ProviderSlug
		dialect ProviderDialect
		wantErr bool
	}{
		{name: "simple", slug: "openai", dialect: ProviderOpenAI},
		{name: "with dash", slug: "openai-main", dialect: ProviderOpenAI},
		{name: "starts digit", slug: "1openai", dialect: ProviderOpenAI},
		{name: "own dialect allowed", slug: "openai", dialect: ProviderOpenAI},
		{name: "shadow other dialect", slug: "deepseek", dialect: ProviderOpenAI, wantErr: true},
		{name: "uppercase", slug: "OpenAI", dialect: ProviderOpenAI, wantErr: true},
		{name: "underscore", slug: "openai_main", dialect: ProviderOpenAI, wantErr: true},
		{name: "leading dash", slug: "-openai", dialect: ProviderOpenAI, wantErr: true},
		{name: "trailing space", slug: "openai ", dialect: ProviderOpenAI, wantErr: true},
		{name: "empty", slug: "", dialect: ProviderOpenAI, wantErr: true},
		{name: "too long", slug: ProviderSlug(strings.Repeat("a", 64)), dialect: ProviderOpenAI, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProviderSlug(tc.slug, tc.dialect)
			if tc.wantErr && err == nil {
				t.Fatalf("validateProviderSlug(%q, %q) = nil, want error", tc.slug, tc.dialect)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateProviderSlug(%q, %q) unexpected error: %v", tc.slug, tc.dialect, err)
			}
		})
	}
}

func TestResolveSlug(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		dialect   ProviderDialect
		want      ProviderSlug
		wantErr   bool
	}{
		{name: "empty defaults to dialect", requested: "", dialect: ProviderOpenAI, want: "openai"},
		{name: "whitespace defaults to dialect", requested: "   ", dialect: ProviderDeepSeek, want: "deepseek"},
		{name: "explicit slug", requested: "azure-openai", dialect: ProviderOpenAI, want: "azure-openai"},
		{name: "explicit trimmed", requested: "  azure  ", dialect: ProviderOpenAI, want: "azure"},
		{name: "invalid explicit", requested: "Bad Slug", dialect: ProviderOpenAI, wantErr: true},
		{name: "shadowing explicit", requested: "google", dialect: ProviderOpenAI, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSlug(tc.requested, tc.dialect)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveSlug(%q) = %q, want error", tc.requested, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSlug(%q) unexpected error: %v", tc.requested, err)
			}
			if got != tc.want {
				t.Fatalf("resolveSlug(%q) = %q, want %q", tc.requested, got, tc.want)
			}
		})
	}
}
