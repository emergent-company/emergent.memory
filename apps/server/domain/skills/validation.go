package skills

import (
	"regexp"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// nameRegex matches the NamePattern slug rule.
var nameRegex = regexp.MustCompile(NamePattern)

// DefaultMaxContentSize is the default maximum skill content size in bytes (1 MiB).
// Used when no explicit cap is configured.
const DefaultMaxContentSize = 1 << 20

// maxNameLength is the maximum allowed skill name length in characters.
const maxNameLength = 64

// normalizeMaxContentSize returns max when positive, falling back to DefaultMaxContentSize.
func normalizeMaxContentSize(max int) int {
	if max <= 0 {
		return DefaultMaxContentSize
	}
	return max
}

// ValidateSkillName checks that the name matches the slug pattern and length constraints.
func ValidateSkillName(name string) error {
	if name == "" {
		return apperror.ErrValidation.WithMessage("name is required")
	}
	if len(name) > maxNameLength {
		return apperror.ErrValidation.WithMessage("name must be 64 characters or fewer")
	}
	if !nameRegex.MatchString(name) {
		return apperror.ErrValidation.WithMessage("name must be a lowercase alphanumeric slug (e.g. my-skill)")
	}
	return nil
}

// ValidateContentSize rejects content larger than the configured maximum
// (default DefaultMaxContentSize when max <= 0).
func ValidateContentSize(content string, maxContentSize int) error {
	if len(content) > normalizeMaxContentSize(maxContentSize) {
		return apperror.ErrValidation.WithMessage("content exceeds the maximum allowed size")
	}
	return nil
}

// ValidateCreateSkill validates a skill create request (name pattern/length,
// non-empty description and content, content size cap).
func ValidateCreateSkill(dto CreateSkillDTO, maxContentSize int) error {
	if err := ValidateSkillName(dto.Name); err != nil {
		return err
	}
	if dto.Description == "" {
		return apperror.ErrValidation.WithMessage("description is required")
	}
	if dto.Content == "" {
		return apperror.ErrValidation.WithMessage("content is required")
	}
	return ValidateContentSize(dto.Content, maxContentSize)
}

// ValidateUpdateSkill validates a partial skill update request. Only the fields
// that are present (non-nil) are validated; the name cannot be changed via PATCH.
func ValidateUpdateSkill(dto UpdateSkillDTO, maxContentSize int) error {
	if dto.Description != nil && *dto.Description == "" {
		return apperror.ErrValidation.WithMessage("description is required")
	}
	if dto.Content != nil {
		if *dto.Content == "" {
			return apperror.ErrValidation.WithMessage("content is required")
		}
		if err := ValidateContentSize(*dto.Content, maxContentSize); err != nil {
			return err
		}
	}
	return nil
}
