package skills

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireValidationErr asserts err is an apperror with the validation_error code.
func requireValidationErr(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var apErr *apperror.Error
	require.ErrorAs(t, err, &apErr)
	assert.Equal(t, apperror.ErrValidation.Code, apErr.Code, "expected validation_error, got %v", err)
}

func TestValidateSkillName(t *testing.T) {
	valid := []string{
		"a",
		"code-review",
		"my-skill-name",
		"abc123",
		"x-y-z",
		strings.Repeat("a", 64),
	}
	for _, name := range valid {
		assert.NoError(t, ValidateSkillName(name), "name %q should be valid", name)
	}

	invalid := []string{
		"",
		"Code Review!",
		"CodeReview",
		"UPPER",
		"-lead",
		"trailing-",
		"a--b",
		"a_b",
		"a.b",
		"a b",
		strings.Repeat("a", 65),
	}
	for _, name := range invalid {
		requireValidationErr(t, ValidateSkillName(name))
	}
}

func TestValidateContentSize(t *testing.T) {
	assert.NoError(t, ValidateContentSize(strings.Repeat("x", 16), 16), "content exactly at cap must be accepted")
	assert.NoError(t, ValidateContentSize(strings.Repeat("x", DefaultMaxContentSize), 0), "default cap must accept exactly 1 MiB")
	requireValidationErr(t, ValidateContentSize(strings.Repeat("x", 17), 16))
	requireValidationErr(t, ValidateContentSize(strings.Repeat("x", DefaultMaxContentSize+1), 0))
}

func TestValidateCreateSkill(t *testing.T) {
	valid := CreateSkillDTO{Name: "code-review", Description: "desc", Content: "content"}
	assert.NoError(t, ValidateCreateSkill(valid, 0))

	t.Run("invalid name", func(t *testing.T) {
		dto := valid
		dto.Name = "Code Review!"
		requireValidationErr(t, ValidateCreateSkill(dto, 0))
	})
	t.Run("empty name", func(t *testing.T) {
		dto := valid
		dto.Name = ""
		requireValidationErr(t, ValidateCreateSkill(dto, 0))
	})
	t.Run("empty description", func(t *testing.T) {
		dto := valid
		dto.Description = ""
		requireValidationErr(t, ValidateCreateSkill(dto, 0))
	})
	t.Run("empty content", func(t *testing.T) {
		dto := valid
		dto.Content = ""
		requireValidationErr(t, ValidateCreateSkill(dto, 0))
	})
	t.Run("oversized content", func(t *testing.T) {
		dto := valid
		dto.Content = strings.Repeat("x", DefaultMaxContentSize+1)
		requireValidationErr(t, ValidateCreateSkill(dto, 0))
	})
	t.Run("custom cap honored", func(t *testing.T) {
		dto := valid
		dto.Content = strings.Repeat("x", 129)
		requireValidationErr(t, ValidateCreateSkill(dto, 128))
	})
}

func TestValidateUpdateSkill(t *testing.T) {
	assert.NoError(t, ValidateUpdateSkill(UpdateSkillDTO{}, 0), "empty partial update must be accepted")

	t.Run("empty description rejected", func(t *testing.T) {
		empty := ""
		requireValidationErr(t, ValidateUpdateSkill(UpdateSkillDTO{Description: &empty}, 0))
	})
	t.Run("empty content rejected", func(t *testing.T) {
		empty := ""
		requireValidationErr(t, ValidateUpdateSkill(UpdateSkillDTO{Content: &empty}, 0))
	})
	t.Run("oversized content rejected", func(t *testing.T) {
		big := strings.Repeat("x", DefaultMaxContentSize+1)
		requireValidationErr(t, ValidateUpdateSkill(UpdateSkillDTO{Content: &big}, 0))
	})
	t.Run("valid partial accepted", func(t *testing.T) {
		desc := "new description"
		content := "new content"
		assert.NoError(t, ValidateUpdateSkill(UpdateSkillDTO{Description: &desc, Content: &content}, 0))
	})
}
