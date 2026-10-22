package sources

import (
	"testing"
)

func TestTraleProvider_Catalog(t *testing.T) {
	p := newTraleProvider().Catalog()
	if p.Type != ProviderTypeTrale {
		t.Errorf("Type = %q, want %q", p.Type, ProviderTypeTrale)
	}
	if p.Name != "Trale" {
		t.Errorf("Name = %q, want Trale", p.Name)
	}
	if p.Vendor != "trale" || p.VendorName != "Trale" {
		t.Errorf("Vendor/VendorName = %q/%q, want trale/Trale", p.Vendor, p.VendorName)
	}
	if p.SourceType != SourceTypeMeeting {
		t.Errorf("SourceType = %q, want %q", p.SourceType, SourceTypeMeeting)
	}
	if !p.Available {
		t.Error("Available = false, want true")
	}
	if !p.SelfRegistering {
		t.Error("SelfRegistering = false, want true (Trale registers its own client)")
	}
}

func TestTraleProvider_ConfigSchema_RequiresTags(t *testing.T) {
	schema := newTraleProvider().ConfigSchema()
	if schema.Type != ProviderTypeTrale {
		t.Errorf("schema.Type = %q, want %q", schema.Type, ProviderTypeTrale)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "tags" {
		t.Errorf("schema.Required = %v, want [tags]", schema.Required)
	}
	for _, key := range []string{"tags", "name_pattern", "include_transcript", "backfill_days"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("schema.Properties missing %q", key)
		}
	}
}

func TestTraleProvider_AuthMethodsAndKeys(t *testing.T) {
	p := newTraleProvider()
	methods := p.AuthMethods()
	if len(methods) != 1 || methods[0] != AuthMethodOAuth {
		t.Errorf("AuthMethods() = %v, want [oauth]", methods)
	}
	if len(p.CredentialFields()) != 0 {
		t.Errorf("CredentialFields() = %v, want empty (OAuth-only)", p.CredentialFields())
	}
	keys := p.CredentialKeys()
	want := []string{"access_token", "refresh_token", "expires_at", "client_id", "token_endpoint", "resource", "scope"}
	if len(keys) != len(want) {
		t.Fatalf("CredentialKeys() = %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("CredentialKeys()[%d] = %q, want %q", i, keys[i], k)
		}
	}
}

func TestTraleProvider_ValidateConfig_TagsRequired(t *testing.T) {
	p := newTraleProvider()

	// Absent tags → rejected.
	if err := p.ValidateConfig(map[string]any{}); err == nil {
		t.Error("ValidateConfig({}) = nil, want tags-required error")
	}
	// Empty tags → rejected.
	if err := p.ValidateConfig(map[string]any{"tags": []string{}}); err == nil {
		t.Error("ValidateConfig(tags=[]) = nil, want tags-required error")
	}
	// Blank tag → rejected.
	if err := p.ValidateConfig(map[string]any{"tags": []string{"  "}}); err == nil {
		t.Error("ValidateConfig(tags=['  ']) = nil, want non-empty-tag error")
	}
	// Valid tags → accepted.
	if err := p.ValidateConfig(map[string]any{"tags": []string{"standup", "roadmap"}}); err != nil {
		t.Errorf("ValidateConfig(valid tags) error = %v, want nil", err)
	}
}

func TestTraleProvider_ValidateConfig_OtherFields(t *testing.T) {
	p := newTraleProvider()
	base := map[string]any{"tags": []string{"standup"}}

	// Invalid regex → rejected.
	bad := cloneMap(base)
	bad["name_pattern"] = "["
	if err := p.ValidateConfig(bad); err == nil {
		t.Error("ValidateConfig(bad regex) = nil, want error")
	}
	// Valid regex → accepted.
	ok := cloneMap(base)
	ok["name_pattern"] = "(?i)^standup"
	ok["include_transcript"] = true
	ok["backfill_days"] = float64(30)
	if err := p.ValidateConfig(ok); err != nil {
		t.Errorf("ValidateConfig(valid extras) error = %v, want nil", err)
	}
	// Out-of-range backfill → rejected.
	neg := cloneMap(base)
	neg["backfill_days"] = float64(-1)
	if err := p.ValidateConfig(neg); err == nil {
		t.Error("ValidateConfig(backfill=-1) = nil, want error")
	}
	huge := cloneMap(base)
	huge["backfill_days"] = float64(9999)
	if err := p.ValidateConfig(huge); err == nil {
		t.Error("ValidateConfig(backfill=9999) = nil, want error")
	}
	// A form submits integers as strings; that must validate and be honoured.
	asString := cloneMap(base)
	asString["backfill_days"] = "30"
	if err := p.ValidateConfig(asString); err != nil {
		t.Errorf("ValidateConfig(backfill=\"30\") error = %v, want nil", err)
	}
	cfg, err := parseTraleConfig(asString)
	if err != nil {
		t.Fatalf("parseTraleConfig(backfill=\"30\") error = %v", err)
	}
	if cfg.backfillDays != 30 {
		t.Errorf("backfillDays = %d, want 30", cfg.backfillDays)
	}
	// A malformed value is reported, never silently replaced by the default.
	badValue := cloneMap(base)
	badValue["backfill_days"] = "ninety"
	if err := p.ValidateConfig(badValue); err == nil {
		t.Error("ValidateConfig(backfill=\"ninety\") = nil, want error")
	}
	// An explicitly blank value falls back to the default.
	blank := cloneMap(base)
	blank["backfill_days"] = ""
	if err := p.ValidateConfig(blank); err != nil {
		t.Errorf("ValidateConfig(backfill=\"\") error = %v, want nil", err)
	}
}

func TestTraleProvider_ExtractCredentials(t *testing.T) {
	p := newTraleProvider()
	creds, err := p.ExtractCredentials(map[string]any{
		"access_token": "at", "refresh_token": "rt", "client_id": "cid",
		"token_endpoint": "https://tok", "resource": "https://res", "scope": "openid",
	})
	if err != nil {
		t.Fatalf("ExtractCredentials() error = %v", err)
	}
	if creds["access_token"] != "at" || creds["refresh_token"] != "rt" {
		t.Errorf("ExtractCredentials() = %v, want at/rt", creds)
	}
	// Missing access_token → rejected.
	if _, err := p.ExtractCredentials(map[string]any{"refresh_token": "rt"}); err == nil {
		t.Error("ExtractCredentials(no access_token) = nil, want error")
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
