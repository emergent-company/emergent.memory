package provider

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestNoProjectProviderConfigError pins the classification every caller relies
// on: "this project has no credential for that provider" is a typed gap, while
// a database or decryption failure is not — otherwise the embedding path cannot
// tell a permanent misconfiguration from something worth retrying.
func TestNoProjectProviderConfigError(t *testing.T) {
	wrapped := fmt.Errorf("resolve: %w", noProjectProviderConfigError("google", "p-1", "configure it"))

	if !errors.Is(wrapped, ErrNoProjectProviderConfig) {
		t.Fatal("wrapped error does not match ErrNoProjectProviderConfig")
	}
	msg := wrapped.Error()
	for _, want := range []string{"google", "p-1", "configure it"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to mention %q", msg, want)
		}
	}

	for _, other := range []error{
		errors.New("failed to get project provider config: connection refused"),
		errors.New("failed to decrypt project credential: cipher: message authentication failed"),
	} {
		if errors.Is(other, ErrNoProjectProviderConfig) {
			t.Errorf("errors.Is(%v, ErrNoProjectProviderConfig) = true, want false", other)
		}
	}
}
