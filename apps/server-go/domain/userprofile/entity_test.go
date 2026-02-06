package userprofile

import (
	"testing"
	"time"
)

func TestProfile_ToDTO(t *testing.T) {
	now := time.Now()
	firstName := "John"
	lastName := "Doe"
	displayName := "John D."
	phone := "+14155551234"
	avatarKey := "avatars/user-123.png"

	tests := []struct {
		name    string
		profile *Profile
		email   string
	}{
		{
			name: "complete profile",
			profile: &Profile{
				ID:              "profile-123",
				ZitadelUserID:   "zitadel-456",
				FirstName:       &firstName,
				LastName:        &lastName,
				DisplayName:     &displayName,
				PhoneE164:       &phone,
				AvatarObjectKey: &avatarKey,
				CreatedAt:       now,
				UpdatedAt:       now,
			},
			email: "john@example.com",
		},
		{
			name: "minimal profile (nil optional fields)",
			profile: &Profile{
				ID:            "profile-456",
				ZitadelUserID: "zitadel-789",
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			email: "user@example.com",
		},
		{
			name: "profile with empty email",
			profile: &Profile{
				ID:            "profile-789",
				ZitadelUserID: "zitadel-012",
				FirstName:     &firstName,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			email: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dto := tt.profile.ToDTO(tt.email)

			if dto.ID != tt.profile.ID {
				t.Errorf("ID = %q, want %q", dto.ID, tt.profile.ID)
			}
			if dto.SubjectID != tt.profile.ZitadelUserID {
				t.Errorf("SubjectID = %q, want %q", dto.SubjectID, tt.profile.ZitadelUserID)
			}
			if dto.ZitadelUserID == nil || *dto.ZitadelUserID != tt.profile.ZitadelUserID {
				t.Errorf("ZitadelUserID = %v, want %q", dto.ZitadelUserID, tt.profile.ZitadelUserID)
			}
			if dto.Email != tt.email {
				t.Errorf("Email = %q, want %q", dto.Email, tt.email)
			}

			// Check optional fields
			if (tt.profile.FirstName == nil) != (dto.FirstName == nil) {
				t.Errorf("FirstName nil mismatch: profile=%v, dto=%v", tt.profile.FirstName, dto.FirstName)
			}
			if tt.profile.FirstName != nil && dto.FirstName != nil && *dto.FirstName != *tt.profile.FirstName {
				t.Errorf("FirstName = %q, want %q", *dto.FirstName, *tt.profile.FirstName)
			}

			if (tt.profile.LastName == nil) != (dto.LastName == nil) {
				t.Errorf("LastName nil mismatch")
			}
			if tt.profile.LastName != nil && dto.LastName != nil && *dto.LastName != *tt.profile.LastName {
				t.Errorf("LastName = %q, want %q", *dto.LastName, *tt.profile.LastName)
			}

			if (tt.profile.DisplayName == nil) != (dto.DisplayName == nil) {
				t.Errorf("DisplayName nil mismatch")
			}
			if tt.profile.DisplayName != nil && dto.DisplayName != nil && *dto.DisplayName != *tt.profile.DisplayName {
				t.Errorf("DisplayName = %q, want %q", *dto.DisplayName, *tt.profile.DisplayName)
			}

			if (tt.profile.PhoneE164 == nil) != (dto.PhoneE164 == nil) {
				t.Errorf("PhoneE164 nil mismatch")
			}
			if tt.profile.PhoneE164 != nil && dto.PhoneE164 != nil && *dto.PhoneE164 != *tt.profile.PhoneE164 {
				t.Errorf("PhoneE164 = %q, want %q", *dto.PhoneE164, *tt.profile.PhoneE164)
			}

			if (tt.profile.AvatarObjectKey == nil) != (dto.AvatarObjectKey == nil) {
				t.Errorf("AvatarObjectKey nil mismatch")
			}
			if tt.profile.AvatarObjectKey != nil && dto.AvatarObjectKey != nil && *dto.AvatarObjectKey != *tt.profile.AvatarObjectKey {
				t.Errorf("AvatarObjectKey = %q, want %q", *dto.AvatarObjectKey, *tt.profile.AvatarObjectKey)
			}
		})
	}
}

func TestProfileDTO_Fields(t *testing.T) {
	zitadelID := "zitadel-123"
	firstName := "Jane"
	lastName := "Smith"
	displayName := "Jane S."
	phone := "+14155559999"
	avatarKey := "avatars/jane.png"

	dto := ProfileDTO{
		ID:              "profile-123",
		SubjectID:       "subject-456",
		ZitadelUserID:   &zitadelID,
		FirstName:       &firstName,
		LastName:        &lastName,
		DisplayName:     &displayName,
		PhoneE164:       &phone,
		AvatarObjectKey: &avatarKey,
		Email:           "jane@example.com",
	}

	if dto.ID != "profile-123" {
		t.Errorf("ID = %q, want %q", dto.ID, "profile-123")
	}
	if dto.SubjectID != "subject-456" {
		t.Errorf("SubjectID = %q, want %q", dto.SubjectID, "subject-456")
	}
	if dto.Email != "jane@example.com" {
		t.Errorf("Email = %q, want %q", dto.Email, "jane@example.com")
	}
}

func TestUpdateProfileRequest_Fields(t *testing.T) {
	firstName := "Updated"
	lastName := "Name"
	displayName := "Updated N."
	phone := "+14155550000"

	req := UpdateProfileRequest{
		FirstName:   &firstName,
		LastName:    &lastName,
		DisplayName: &displayName,
		PhoneE164:   &phone,
	}

	if req.FirstName == nil || *req.FirstName != firstName {
		t.Errorf("FirstName = %v, want %q", req.FirstName, firstName)
	}
	if req.LastName == nil || *req.LastName != lastName {
		t.Errorf("LastName = %v, want %q", req.LastName, lastName)
	}
	if req.DisplayName == nil || *req.DisplayName != displayName {
		t.Errorf("DisplayName = %v, want %q", req.DisplayName, displayName)
	}
	if req.PhoneE164 == nil || *req.PhoneE164 != phone {
		t.Errorf("PhoneE164 = %v, want %q", req.PhoneE164, phone)
	}
}

func TestUpdateProfileRequest_NilFields(t *testing.T) {
	req := UpdateProfileRequest{}

	if req.FirstName != nil {
		t.Errorf("FirstName = %v, want nil", req.FirstName)
	}
	if req.LastName != nil {
		t.Errorf("LastName = %v, want nil", req.LastName)
	}
	if req.DisplayName != nil {
		t.Errorf("DisplayName = %v, want nil", req.DisplayName)
	}
	if req.PhoneE164 != nil {
		t.Errorf("PhoneE164 = %v, want nil", req.PhoneE164)
	}
}
