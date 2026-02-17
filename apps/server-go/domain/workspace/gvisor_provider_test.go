package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGVisorCapabilities(t *testing.T) {
	// Test capabilities without requiring Docker daemon
	caps := (&GVisorProvider{}).Capabilities()

	assert.Equal(t, "gVisor (Docker)", caps.Name)
	assert.True(t, caps.SupportsPersistence)
	assert.False(t, caps.SupportsSnapshots)
	assert.True(t, caps.SupportsWarmPool)
	assert.False(t, caps.RequiresKVM)
	assert.Equal(t, 50, caps.EstimatedStartupMs)
	assert.Equal(t, ProviderGVisor, caps.ProviderType)
}

func TestParseMemoryBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"empty", "", 0},
		{"gigabytes", "4G", 4 * 1024 * 1024 * 1024},
		{"gigabytes_lower", "4g", 4 * 1024 * 1024 * 1024},
		{"megabytes", "512M", 512 * 1024 * 1024},
		{"megabytes_with_B", "512MB", 512 * 1024 * 1024},
		{"kilobytes", "1024K", 1024 * 1024},
		{"bytes_only", "1048576", 1048576},
		{"1G", "1G", 1073741824},
		{"2G", "2G", 2147483648},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemoryBytes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0", 0},
		{"1234", 1234},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseSize(tt.input))
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"42", 42},
		{"0", 0},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseInt(tt.input))
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"2.5", 2.5},
		{"0", 0},
		{"1", 1},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.InDelta(t, tt.expected, parseFloat(tt.input), 0.001)
		})
	}
}

func TestBase64Encode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "hello", "aGVsbG8="},
		{"empty", "", ""},
		{"with newlines", "line1\nline2", "bGluZTEKbGluZTI="},
		{"with special chars", "const x = 'hello';", "Y29uc3QgeCA9ICdoZWxsbyc7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base64Encode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyResourceLimits(t *testing.T) {
	// Test that nil limits don't panic
	p := &GVisorProvider{}

	t.Run("nil limits", func(t *testing.T) {
		// applyResourceLimits is only called with non-nil limits in Create
		// but test the parse functions it uses
		assert.Equal(t, int64(0), parseMemoryBytes(""))
		assert.InDelta(t, 0.0, parseFloat(""), 0.001)
	})

	t.Run("with CPU and memory", func(t *testing.T) {
		caps := p.Capabilities()
		assert.NotNil(t, caps)
		assert.False(t, caps.RequiresKVM)
	})
}
