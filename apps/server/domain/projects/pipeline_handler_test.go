package projects

import (
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/pipeline"
)

func TestResolvePipelineEmptyFallsBackToPreset(t *testing.T) {
	p, customized, err := resolvePipeline(pipeline.ModeSuggest, nil)
	if err != nil {
		t.Fatalf("resolvePipeline: %v", err)
	}
	if !p.Equal(pipeline.DefaultPolicy(pipeline.ModeSuggest)) {
		t.Fatal("empty policy should resolve to the suggest preset")
	}
	if customized {
		t.Fatal("empty policy must not be customized")
	}
}

func TestResolvePipelinePresetNotCustomized(t *testing.T) {
	auto := pipeline.DefaultPolicy(pipeline.ModeAuto)
	p, customized, err := resolvePipeline(pipeline.ModeAuto, auto.Marshal())
	if err != nil {
		t.Fatalf("resolvePipeline: %v", err)
	}
	if !p.Equal(auto) {
		t.Fatal("stored auto preset should resolve verbatim")
	}
	if customized {
		t.Fatal("preset-equal policy must not be customized")
	}
}

func TestResolvePipelineCustomized(t *testing.T) {
	auto := pipeline.DefaultPolicy(pipeline.ModeAuto)
	auto.Discovery.DebounceHours = 1
	p, customized, err := resolvePipeline(pipeline.ModeAuto, auto.Marshal())
	if err != nil {
		t.Fatalf("resolvePipeline: %v", err)
	}
	if !p.Equal(auto) {
		t.Fatal("customized policy should resolve verbatim")
	}
	if !customized {
		t.Fatal("modified policy must be flagged customized")
	}
}

func TestResolvePipelineRejectsInvalidStoredPolicy(t *testing.T) {
	_, _, err := resolvePipeline(pipeline.ModeSuggest, json.RawMessage(`{"merge":{"policy":"nuke"}}`))
	if err == nil {
		t.Fatal("expected invalid stored policy to error")
	}
}
