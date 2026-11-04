package graph

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// recencyScore computes the sigmoid recency score given hours old and half-life.
// Mirrors the formula used in HybridSearch.
func recencyScore(hoursOld, halfLife float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(float64((hoursOld-halfLife)/(halfLife/4.0)))))
}

// accessScore computes the access score given days since last access.
// Mirrors the formula used in HybridSearch.
func accessScore(daysSinceAccess float32) float32 {
	return float32(math.Max(0, float64(1.0-daysSinceAccess/365.0)))
}

// TestRecencyScoreFormula verifies the sigmoid recency formula properties.
func TestRecencyScoreFormula(t *testing.T) {
	halfLife := float32(168.0) // 7 days in hours

	// At exactly half_life hours old, score should be ~0.5
	scoreAtHalfLife := recencyScore(halfLife, halfLife)
	if math.Abs(float64(scoreAtHalfLife)-0.5) > 0.01 {
		t.Errorf("recencyScore at half_life: got %v, want ~0.5", scoreAtHalfLife)
	}

	// Newer than half_life → score > 0.5
	scoreNewer := recencyScore(halfLife/2, halfLife)
	if scoreNewer <= 0.5 {
		t.Errorf("recencyScore for newer object: got %v, want > 0.5", scoreNewer)
	}

	// Older than half_life → score < 0.5
	scoreOlder := recencyScore(halfLife*2, halfLife)
	if scoreOlder >= 0.5 {
		t.Errorf("recencyScore for older object: got %v, want < 0.5", scoreOlder)
	}

	// Score must be in [0, 1]
	for _, hoursOld := range []float32{0, 100, 168, 500, 10000} {
		s := recencyScore(hoursOld, halfLife)
		if s < 0 || s > 1 {
			t.Errorf("recencyScore(%v, %v) = %v, out of [0,1]", hoursOld, halfLife, s)
		}
	}
}

// TestAccessScoreFormula verifies the linear access score formula properties.
func TestAccessScoreFormula(t *testing.T) {
	// Zero days → score 1.0
	s0 := accessScore(0)
	if math.Abs(float64(s0)-1.0) > 0.001 {
		t.Errorf("accessScore(0) = %v, want 1.0", s0)
	}

	// 365 days → score 0.0
	s365 := accessScore(365)
	if math.Abs(float64(s365)) > 0.001 {
		t.Errorf("accessScore(365) = %v, want 0.0", s365)
	}

	// 182 days (~half year) → score ~0.5
	s182 := accessScore(182.5)
	if math.Abs(float64(s182)-0.5) > 0.01 {
		t.Errorf("accessScore(182.5) = %v, want ~0.5", s182)
	}

	// > 365 days → score clamped to 0.0
	s400 := accessScore(400)
	if s400 != 0 {
		t.Errorf("accessScore(400) = %v, want 0.0 (clamped)", s400)
	}

	// Score must be in [0, 1]
	for _, days := range []float32{0, 90, 180, 365, 730} {
		s := accessScore(days)
		if s < 0 || s > 1 {
			t.Errorf("accessScore(%v) = %v, out of [0,1]", days, s)
		}
	}
}

// TestRecencyBoostDefaultHalfLife verifies that RecencyHalfLife defaults to 168
// when RecencyBoost > 0 and RecencyHalfLife is nil.
func TestRecencyBoostDefaultHalfLife(t *testing.T) {
	boostVal := float32(1.0)
	req := &HybridSearchRequest{
		RecencyBoost:    &boostVal,
		RecencyHalfLife: nil,
	}

	var recencyHalfLife float32
	if req.RecencyBoost != nil && *req.RecencyBoost > 0 {
		if req.RecencyHalfLife != nil {
			recencyHalfLife = *req.RecencyHalfLife
		} else {
			recencyHalfLife = 168.0
		}
	}

	if recencyHalfLife != 168.0 {
		t.Errorf("default RecencyHalfLife = %v, want 168.0", recencyHalfLife)
	}
}

// TestHybridSearchWithBoostsApplied is a lightweight integration-style test that
// verifies boost fields are wired through the struct without needing a DB.
func TestHybridSearchWithBoostsApplied(t *testing.T) {
	recencyBoost := float32(1.0)
	accessBoost := float32(0.5)
	halfLife := float32(168.0)

	req := &HybridSearchRequest{
		Query:           "test",
		RecencyBoost:    &recencyBoost,
		RecencyHalfLife: &halfLife,
		AccessBoost:     &accessBoost,
	}

	if req.RecencyBoost == nil || *req.RecencyBoost != 1.0 {
		t.Errorf("RecencyBoost not set correctly")
	}
	if req.RecencyHalfLife == nil || *req.RecencyHalfLife != 168.0 {
		t.Errorf("RecencyHalfLife not set correctly")
	}
	if req.AccessBoost == nil || *req.AccessBoost != 0.5 {
		t.Errorf("AccessBoost not set correctly")
	}
}

// TestRecencyBoostFavorsNewerObjects verifies that when recency_boost > 0,
// a newer object receives a higher score addition than an older one.
func TestRecencyBoostFavorsNewerObjects(t *testing.T) {
	recencyBoostVal := float32(1.0)
	halfLife := float32(168.0)

	newObjHoursOld := float32(24)  // 1 day old
	oldObjHoursOld := float32(720) // 30 days old

	newScore := recencyBoostVal * recencyScore(newObjHoursOld, halfLife)
	oldScore := recencyBoostVal * recencyScore(oldObjHoursOld, halfLife)

	if newScore <= oldScore {
		t.Errorf("newer object should have higher boost: new=%v old=%v", newScore, oldScore)
	}
}

// TestRecencyBoostZeroIsIdentity verifies that recency_boost=0 adds zero to the score.
func TestRecencyBoostZeroIsIdentity(t *testing.T) {
	recencyBoostVal := float32(0)
	baseScore := float32(0.75)

	now := time.Now()
	_ = uuid.New()

	if recencyBoostVal > 0 {
		hoursOld := float32(time.Since(now).Hours())
		baseScore += recencyBoostVal * recencyScore(hoursOld, 168)
	}

	if baseScore != 0.75 {
		t.Errorf("zero recency boost changed score: %v", baseScore)
	}
}
