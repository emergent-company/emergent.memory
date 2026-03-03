package provider

import (
	"testing"
)

// TestComputeCost exercises the pure computeCost function with various
// token combinations and price inputs.
func TestComputeCost(t *testing.T) {
	tests := []struct {
		name            string
		event           *LLMUsageEvent
		textInputPrice  float64
		imageInputPrice float64
		videoInputPrice float64
		audioInputPrice float64
		outputPrice     float64
		wantCost        float64
	}{
		{
			name: "text-only tokens",
			event: &LLMUsageEvent{
				TextInputTokens: 1_000_000,
				OutputTokens:    500_000,
			},
			textInputPrice: 0.075,        // $0.075 per 1M tokens
			outputPrice:    0.30,         // $0.30 per 1M tokens
			wantCost:       0.075 + 0.15, // 0.075 + 0.30*(500k/1M)
		},
		{
			name: "multi-modal tokens",
			event: &LLMUsageEvent{
				TextInputTokens:  1_000_000,
				ImageInputTokens: 500_000,
				VideoInputTokens: 250_000,
				AudioInputTokens: 100_000,
				OutputTokens:     200_000,
			},
			textInputPrice:  0.075,
			imageInputPrice: 0.04,
			videoInputPrice: 0.02,
			audioInputPrice: 0.01,
			outputPrice:     0.30,
			// 0.075 + 0.04*0.5 + 0.02*0.25 + 0.01*0.1 + 0.30*0.2
			// = 0.075 + 0.02 + 0.005 + 0.001 + 0.06 = 0.161
			wantCost: 0.161,
		},
		{
			name: "zero tokens returns zero cost",
			event: &LLMUsageEvent{
				TextInputTokens: 0,
				OutputTokens:    0,
			},
			textInputPrice: 1.0,
			outputPrice:    1.0,
			wantCost:       0.0,
		},
		{
			name: "zero prices returns zero cost",
			event: &LLMUsageEvent{
				TextInputTokens: 1_000_000,
				OutputTokens:    1_000_000,
			},
			textInputPrice: 0.0,
			outputPrice:    0.0,
			wantCost:       0.0,
		},
		{
			name: "only image tokens",
			event: &LLMUsageEvent{
				ImageInputTokens: 2_000_000,
			},
			imageInputPrice: 0.04,
			wantCost:        0.08, // 0.04 * 2
		},
		{
			name: "fractional token counts below 1M",
			event: &LLMUsageEvent{
				TextInputTokens: 1_000, // 0.001M
				OutputTokens:    500,   // 0.0005M
			},
			textInputPrice: 1.0,
			outputPrice:    2.0,
			// 1.0 * (1000/1M) + 2.0 * (500/1M) = 0.001 + 0.001 = 0.002
			wantCost: 0.002,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCost(tt.event,
				tt.textInputPrice, tt.imageInputPrice,
				tt.videoInputPrice, tt.audioInputPrice,
				tt.outputPrice)

			const epsilon = 1e-9
			diff := got - tt.wantCost
			if diff < -epsilon || diff > epsilon {
				t.Errorf("computeCost() = %v, want %v (diff %v)", got, tt.wantCost, diff)
			}
		})
	}
}
