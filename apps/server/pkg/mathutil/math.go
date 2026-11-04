// Package mathutil provides mathematical utility functions for the Go server.
package mathutil

import "math"

// CalcMeanStd calculates the mean and standard deviation of a slice of float32 values.
// Returns (0, 1) for empty slices to avoid division by zero in normalization.
func CalcMeanStd(scores []float32) (mean, std float32) {
	if len(scores) == 0 {
		return 0, 1 // Avoid division by zero
	}

	// Calculate mean
	var sum float32
	for _, s := range scores {
		sum += s
	}
	mean = sum / float32(len(scores))

	// Calculate standard deviation
	var variance float32
	for _, s := range scores {
		diff := s - mean
		variance += diff * diff
	}
	variance /= float32(len(scores))
	std = float32(math.Sqrt(float64(variance)))

	// Avoid division by zero in normalization
	if std == 0 {
		std = 1
	}

	return mean, std
}

// EffectiveFusionWeights adjusts lexical/vector fusion weights so a surviving
// channel is not penalised by an empty channel's share of the weight. When
// exactly one channel produced no scores, the surviving channel gets effective
// weight 1.0 and the empty channel 0.0; otherwise the weights are returned
// unchanged.
func EffectiveFusionWeights(lexicalWeight, vectorWeight float32, hasLexical, hasVector bool) (float32, float32) {
	if hasLexical && !hasVector {
		return 1.0, 0.0
	}
	if !hasLexical && hasVector {
		return 0.0, 1.0
	}
	return lexicalWeight, vectorWeight
}

// Sigmoid applies the sigmoid function to a float32 value.
// sigmoid(z) = 1 / (1 + e^(-z))
func Sigmoid(z float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(float64(-z))))
}

// ClampInt clamps an integer value to a range [min, max].
func ClampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampLimit validates a pagination limit, applying default and max constraints.
// If limit <= 0, returns defaultVal. If limit > maxVal, returns maxVal.
func ClampLimit(limit, defaultVal, maxVal int) int {
	if limit <= 0 {
		return defaultVal
	}
	if limit > maxVal {
		return maxVal
	}
	return limit
}
