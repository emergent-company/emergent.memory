package auth

import "context"

// AutoProvisionService handles automatic org and project creation for new users.
// It is called from the auth middleware when a brand-new user profile is created.
type AutoProvisionService interface {
	// ProvisionNewUser creates a default org and project for a newly registered user.
	// It should be idempotent and tolerate partial failures (e.g., org created but project fails).
	// Errors are logged but should not prevent the user from authenticating.
	ProvisionNewUser(ctx context.Context, userID string, profile *UserProfileInfo) error
}
