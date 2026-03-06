package auth

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name   string
		scope  string
		want   []string
	}{
		{
			name:  "empty string",
			scope: "",
			want:  []string{},
		},
		{
			name:  "single scope",
			scope: "openid",
			want:  []string{"openid"},
		},
		{
			name:  "multiple scopes",
			scope: "openid profile email",
			want:  []string{"openid", "profile", "email"},
		},
		{
			name:  "scopes with custom values",
			scope: "openid urn:zitadel:iam:org:project:id:123:aud read:documents",
			want:  []string{"openid", "urn:zitadel:iam:org:project:id:123:aud", "read:documents"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseScopes(tt.scope)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseScopes(%q) returned %d scopes, want %d", tt.scope, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseScopes(%q)[%d] = %q, want %q", tt.scope, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTimeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		jsonData  string
		wantUnix  int64
		wantErr   bool
	}{
		{
			name:     "valid timestamp",
			jsonData: "1700000000",
			wantUnix: 1700000000,
		},
		{
			name:     "zero timestamp",
			jsonData: "0",
			wantUnix: 0,
		},
		{
			name:     "negative timestamp",
			jsonData: "-1000",
			wantUnix: -1000,
		},
		{
			name:     "invalid json",
			jsonData: "invalid",
			wantErr:  true,
		},
		{
			name:     "string timestamp",
			jsonData: `"1700000000"`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tm Time
			err := tm.UnmarshalJSON([]byte(tt.jsonData))
			
			if tt.wantErr {
				if err == nil {
					t.Error("UnmarshalJSON() expected error, got nil")
				}
				return
			}
			
			if err != nil {
				t.Errorf("UnmarshalJSON() unexpected error: %v", err)
				return
			}
			
			if tm.Unix() != tt.wantUnix {
				t.Errorf("UnmarshalJSON() Unix() = %d, want %d", tm.Unix(), tt.wantUnix)
			}
		})
	}
}

func TestTimeAsTime(t *testing.T) {
	expected := time.Date(2023, 11, 15, 10, 30, 0, 0, time.UTC)
	tm := Time{Time: expected}
	
	got := tm.AsTime()
	
	if !got.Equal(expected) {
		t.Errorf("AsTime() = %v, want %v", got, expected)
	}
}

func TestIntrospectionResponseIsActive(t *testing.T) {
	tests := []struct {
		name   string
		active bool
		want   bool
	}{
		{"active", true, true},
		{"inactive", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &introspectionResponse{Active: tt.active}
			if got := r.IsActive(); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntrospectionResponseSetActive(t *testing.T) {
	r := &introspectionResponse{Active: false}
	
	r.SetActive(true)
	if !r.Active {
		t.Error("SetActive(true) didn't set Active to true")
	}
	
	r.SetActive(false)
	if r.Active {
		t.Error("SetActive(false) didn't set Active to false")
	}
}

func TestIntrospectionResponseGetEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{"with email", "user@example.com", "user@example.com"},
		{"empty email", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &introspectionResponse{Email: tt.email}
			if got := r.GetEmail(); got != tt.want {
				t.Errorf("GetEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntrospectionResponseGetPreferredUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{"with username", "johndoe", "johndoe"},
		{"empty username", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &introspectionResponse{PreferredUsername: tt.username}
			if got := r.GetPreferredUsername(); got != tt.want {
				t.Errorf("GetPreferredUsername() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntrospectionResponseGetName(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		want     string
	}{
		{"with name", "John Doe", "John Doe"},
		{"empty name", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &introspectionResponse{Name: tt.fullName}
			if got := r.GetName(); got != tt.want {
				t.Errorf("GetName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntrospectionResponseJSONRoundTrip(t *testing.T) {
	// Test that the struct can be properly marshaled/unmarshaled
	input := `{
		"active": true,
		"scope": "openid profile email",
		"client_id": "123456",
		"token_type": "Bearer",
		"exp": 1700000000,
		"iat": 1699999000,
		"nbf": 1699999000,
		"sub": "user-123",
		"aud": "api-audience",
		"iss": "https://issuer.example.com",
		"jti": "jwt-id-456",
		"email": "user@example.com",
		"email_verified": true,
		"name": "John Doe",
		"preferred_username": "johndoe",
		"given_name": "John",
		"family_name": "Doe"
	}`

	var resp introspectionResponse
	if err := json.Unmarshal([]byte(input), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	// Verify parsed values
	if !resp.Active {
		t.Error("Active should be true")
	}
	if resp.Scope != "openid profile email" {
		t.Errorf("Scope = %q, want %q", resp.Scope, "openid profile email")
	}
	if resp.ClientID != "123456" {
		t.Errorf("ClientID = %q, want %q", resp.ClientID, "123456")
	}
	if resp.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", resp.Subject, "user-123")
	}
	if resp.Expiration.Unix() != 1700000000 {
		t.Errorf("Expiration.Unix() = %d, want %d", resp.Expiration.Unix(), 1700000000)
	}
	if resp.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", resp.Email, "user@example.com")
	}
	if !resp.EmailVerified {
		t.Error("EmailVerified should be true")
	}
	if resp.Name != "John Doe" {
		t.Errorf("Name = %q, want %q", resp.Name, "John Doe")
	}
	if resp.PreferredUsername != "johndoe" {
		t.Errorf("PreferredUsername = %q, want %q", resp.PreferredUsername, "johndoe")
	}
	if resp.GivenName != "John" {
		t.Errorf("GivenName = %q, want %q", resp.GivenName, "John")
	}
	if resp.FamilyName != "Doe" {
		t.Errorf("FamilyName = %q, want %q", resp.FamilyName, "Doe")
	}
}

func TestIntrospectionResultFields(t *testing.T) {
	result := &IntrospectionResult{
		Active:   true,
		Sub:      "user-123",
		Email:    "test@example.com",
		Scope:    "openid profile",
		Exp:      1700000000,
		ClientID: "client-456",
		Username: "testuser",
		Name:     "Test User",
		Claims:   map[string]any{"custom": "value"},
	}

	// Verify all fields are accessible
	if !result.Active {
		t.Error("Active should be true")
	}
	if result.Sub != "user-123" {
		t.Errorf("Sub = %q, want %q", result.Sub, "user-123")
	}
	if result.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", result.Email, "test@example.com")
	}
	if result.Scope != "openid profile" {
		t.Errorf("Scope = %q, want %q", result.Scope, "openid profile")
	}
	if result.Exp != 1700000000 {
		t.Errorf("Exp = %d, want %d", result.Exp, 1700000000)
	}
	if result.ClientID != "client-456" {
		t.Errorf("ClientID = %q, want %q", result.ClientID, "client-456")
	}
	if result.Username != "testuser" {
		t.Errorf("Username = %q, want %q", result.Username, "testuser")
	}
	if result.Name != "Test User" {
		t.Errorf("Name = %q, want %q", result.Name, "Test User")
	}
	if result.Claims["custom"] != "value" {
		t.Errorf("Claims[custom] = %v, want %q", result.Claims["custom"], "value")
	}
}

func TestIntrospectionResultJSONMarshal(t *testing.T) {
	result := &IntrospectionResult{
		Active:   true,
		Sub:      "user-123",
		Email:    "test@example.com",
		Scope:    "openid",
		Exp:      1700000000,
		ClientID: "client-456",
		Username: "testuser",
		Name:     "Test User",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var unmarshaled IntrospectionResult
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if unmarshaled.Active != result.Active {
		t.Errorf("Active = %v, want %v", unmarshaled.Active, result.Active)
	}
	if unmarshaled.Sub != result.Sub {
		t.Errorf("Sub = %q, want %q", unmarshaled.Sub, result.Sub)
	}
	if unmarshaled.Email != result.Email {
		t.Errorf("Email = %q, want %q", unmarshaled.Email, result.Email)
	}
}

func TestZitadelService_hashToken(t *testing.T) {
	// hashToken is a pure function that doesn't use any ZitadelService fields
	// so we can test it with a nil-ish service (just need the method receiver)
	z := &ZitadelService{}

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "simple token",
			token: "abc123",
		},
		{
			name:  "bearer token format",
			token: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature",
		},
		{
			name:  "api token format",
			token: "emt_abcdefghijklmnopqrstuvwxyz123456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := z.hashToken(tt.token)

			// SHA-512 produces 128 hex characters
			if len(hash) != 128 {
				t.Errorf("hashToken(%q) produced hash of length %d, want 128", tt.token, len(hash))
			}

			// Hash should be deterministic
			hash2 := z.hashToken(tt.token)
			if hash != hash2 {
				t.Errorf("hashToken(%q) not deterministic: %q != %q", tt.token, hash, hash2)
			}

			// Hash should only contain hex characters
			for _, c := range hash {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("hashToken(%q) contains non-hex character: %c", tt.token, c)
					break
				}
			}
		})
	}
}

func TestZitadelService_hashToken_Uniqueness(t *testing.T) {
	z := &ZitadelService{}

	tokens := []string{
		"token1",
		"token2",
		"Token1", // different case
		"token1 ", // trailing space
		" token1", // leading space
	}

	hashes := make(map[string]string)
	for _, token := range tokens {
		hash := z.hashToken(token)
		if existingToken, exists := hashes[hash]; exists {
			t.Errorf("hashToken collision: %q and %q both hash to %q", token, existingToken, hash)
		}
		hashes[hash] = token
	}
}

func TestGetAllScopes(t *testing.T) {
	scopes := GetAllScopes()

	// Should return a non-empty list
	if len(scopes) == 0 {
		t.Fatal("GetAllScopes() returned empty list")
	}

	// Should contain some expected scopes
	expectedScopes := []string{
		"org:read",
		"project:read",
		"documents:read",
		"documents:write",
		"search:read",
		"chat:use",
		"graph:read",
		"graph:write",
	}

	for _, expected := range expectedScopes {
		found := false
		for _, s := range scopes {
			if s == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetAllScopes() missing expected scope %q", expected)
		}
	}

	// Should not contain duplicates
	seen := make(map[string]bool)
	for _, s := range scopes {
		if seen[s] {
			t.Errorf("GetAllScopes() contains duplicate scope %q", s)
		}
		seen[s] = true
	}

	// All scopes should be non-empty strings
	for i, s := range scopes {
		if s == "" {
			t.Errorf("GetAllScopes()[%d] is empty string", i)
		}
	}
}
