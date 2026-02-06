package mathutil

import (
	"math"
	"testing"
)

func TestCalcMeanStd(t *testing.T) {
	tests := []struct {
		name        string
		scores      []float32
		wantMean    float32
		wantStd     float32
		meanEpsilon float32 // Tolerance for floating point comparison
		stdEpsilon  float32
	}{
		{
			name:        "empty slice returns 0, 1",
			scores:      []float32{},
			wantMean:    0,
			wantStd:     1,
			meanEpsilon: 0,
			stdEpsilon:  0,
		},
		{
			name:        "nil slice returns 0, 1",
			scores:      nil,
			wantMean:    0,
			wantStd:     1,
			meanEpsilon: 0,
			stdEpsilon:  0,
		},
		{
			name:        "single element returns element, 1",
			scores:      []float32{5.0},
			wantMean:    5.0,
			wantStd:     1, // std=0 is clamped to 1
			meanEpsilon: 0,
			stdEpsilon:  0,
		},
		{
			name:        "two equal elements returns element, 1",
			scores:      []float32{3.0, 3.0},
			wantMean:    3.0,
			wantStd:     1, // std=0 is clamped to 1
			meanEpsilon: 0,
			stdEpsilon:  0,
		},
		{
			name:        "simple sequence 1,2,3,4,5",
			scores:      []float32{1, 2, 3, 4, 5},
			wantMean:    3.0,
			wantStd:     float32(math.Sqrt(2)), // sqrt((4+1+0+1+4)/5) = sqrt(2)
			meanEpsilon: 0.0001,
			stdEpsilon:  0.0001,
		},
		{
			name:        "negative values",
			scores:      []float32{-2, -1, 0, 1, 2},
			wantMean:    0.0,
			wantStd:     float32(math.Sqrt(2)), // sqrt((4+1+0+1+4)/5) = sqrt(2)
			meanEpsilon: 0.0001,
			stdEpsilon:  0.0001,
		},
		{
			name:        "large values",
			scores:      []float32{1000, 2000, 3000},
			wantMean:    2000,
			wantStd:     float32(math.Sqrt(2000000.0 / 3.0)), // sqrt(((1000000+0+1000000)/3))
			meanEpsilon: 0.1,
			stdEpsilon:  0.1,
		},
		{
			name:        "small decimal values",
			scores:      []float32{0.1, 0.2, 0.3},
			wantMean:    0.2,
			wantStd:     float32(math.Sqrt(0.02 / 3.0)), // sqrt(((0.01+0+0.01)/3))
			meanEpsilon: 0.0001,
			stdEpsilon:  0.0001,
		},
		{
			name:        "mixed positive and negative",
			scores:      []float32{-10, 0, 10},
			wantMean:    0.0,
			wantStd:     float32(math.Sqrt(200.0 / 3.0)), // sqrt((100+0+100)/3)
			meanEpsilon: 0.0001,
			stdEpsilon:  0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMean, gotStd := CalcMeanStd(tt.scores)

			if tt.meanEpsilon == 0 {
				if gotMean != tt.wantMean {
					t.Errorf("CalcMeanStd() mean = %v, want %v", gotMean, tt.wantMean)
				}
			} else {
				if diff := float32(math.Abs(float64(gotMean - tt.wantMean))); diff > tt.meanEpsilon {
					t.Errorf("CalcMeanStd() mean = %v, want %v (diff %v > epsilon %v)", gotMean, tt.wantMean, diff, tt.meanEpsilon)
				}
			}

			if tt.stdEpsilon == 0 {
				if gotStd != tt.wantStd {
					t.Errorf("CalcMeanStd() std = %v, want %v", gotStd, tt.wantStd)
				}
			} else {
				if diff := float32(math.Abs(float64(gotStd - tt.wantStd))); diff > tt.stdEpsilon {
					t.Errorf("CalcMeanStd() std = %v, want %v (diff %v > epsilon %v)", gotStd, tt.wantStd, diff, tt.stdEpsilon)
				}
			}
		})
	}
}

func TestSigmoid(t *testing.T) {
	tests := []struct {
		name    string
		input   float32
		want    float32
		epsilon float32
	}{
		{
			name:    "zero returns 0.5",
			input:   0,
			want:    0.5,
			epsilon: 0.0001,
		},
		{
			name:    "large positive returns ~1",
			input:   10,
			want:    1.0,
			epsilon: 0.001,
		},
		{
			name:    "large negative returns ~0",
			input:   -10,
			want:    0.0,
			epsilon: 0.001,
		},
		{
			name:    "positive 1",
			input:   1,
			want:    float32(1.0 / (1.0 + math.Exp(-1))), // ~0.731
			epsilon: 0.0001,
		},
		{
			name:    "negative 1",
			input:   -1,
			want:    float32(1.0 / (1.0 + math.Exp(1))), // ~0.269
			epsilon: 0.0001,
		},
		{
			name:    "positive 2",
			input:   2,
			want:    float32(1.0 / (1.0 + math.Exp(-2))), // ~0.881
			epsilon: 0.0001,
		},
		{
			name:    "very large positive saturates to 1",
			input:   100,
			want:    1.0,
			epsilon: 0.0001,
		},
		{
			name:    "very large negative saturates to 0",
			input:   -100,
			want:    0.0,
			epsilon: 0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sigmoid(tt.input)
			if diff := float32(math.Abs(float64(got - tt.want))); diff > tt.epsilon {
				t.Errorf("Sigmoid(%v) = %v, want %v (diff %v > epsilon %v)", tt.input, got, tt.want, diff, tt.epsilon)
			}
		})
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		name  string
		value int
		min   int
		max   int
		want  int
	}{
		{
			name:  "value within range",
			value: 5,
			min:   0,
			max:   10,
			want:  5,
		},
		{
			name:  "value at min boundary",
			value: 0,
			min:   0,
			max:   10,
			want:  0,
		},
		{
			name:  "value at max boundary",
			value: 10,
			min:   0,
			max:   10,
			want:  10,
		},
		{
			name:  "value below min",
			value: -5,
			min:   0,
			max:   10,
			want:  0,
		},
		{
			name:  "value above max",
			value: 15,
			min:   0,
			max:   10,
			want:  10,
		},
		{
			name:  "negative range value within",
			value: -5,
			min:   -10,
			max:   -1,
			want:  -5,
		},
		{
			name:  "negative range value below",
			value: -15,
			min:   -10,
			max:   -1,
			want:  -10,
		},
		{
			name:  "negative range value above",
			value: 5,
			min:   -10,
			max:   -1,
			want:  -1,
		},
		{
			name:  "min equals max value equals both",
			value: 5,
			min:   5,
			max:   5,
			want:  5,
		},
		{
			name:  "min equals max value below",
			value: 3,
			min:   5,
			max:   5,
			want:  5,
		},
		{
			name:  "min equals max value above",
			value: 7,
			min:   5,
			max:   5,
			want:  5,
		},
		{
			name:  "large positive value",
			value: 1000000,
			min:   0,
			max:   100,
			want:  100,
		},
		{
			name:  "large negative value",
			value: -1000000,
			min:   0,
			max:   100,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampInt(tt.value, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("ClampInt(%d, %d, %d) = %d, want %d", tt.value, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		defaultVal int
		maxVal     int
		want       int
	}{
		{
			name:       "limit within range",
			limit:      50,
			defaultVal: 20,
			maxVal:     100,
			want:       50,
		},
		{
			name:       "limit zero returns default",
			limit:      0,
			defaultVal: 20,
			maxVal:     100,
			want:       20,
		},
		{
			name:       "limit negative returns default",
			limit:      -10,
			defaultVal: 20,
			maxVal:     100,
			want:       20,
		},
		{
			name:       "limit exceeds max returns max",
			limit:      150,
			defaultVal: 20,
			maxVal:     100,
			want:       100,
		},
		{
			name:       "limit equals max",
			limit:      100,
			defaultVal: 20,
			maxVal:     100,
			want:       100,
		},
		{
			name:       "limit equals default",
			limit:      20,
			defaultVal: 20,
			maxVal:     100,
			want:       20,
		},
		{
			name:       "limit of 1",
			limit:      1,
			defaultVal: 20,
			maxVal:     100,
			want:       1,
		},
		{
			name:       "very large limit clamped to max",
			limit:      1000000,
			defaultVal: 20,
			maxVal:     100,
			want:       100,
		},
		{
			name:       "typical pagination scenario",
			limit:      25,
			defaultVal: 10,
			maxVal:     50,
			want:       25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampLimit(tt.limit, tt.defaultVal, tt.maxVal)
			if got != tt.want {
				t.Errorf("ClampLimit(%d, %d, %d) = %d, want %d", tt.limit, tt.defaultVal, tt.maxVal, got, tt.want)
			}
		})
	}
}
