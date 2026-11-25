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

func strPtr(s string) *string { return &s }

func rawPtr(b []byte) *json.RawMessage { r := json.RawMessage(b); return &r }

func TestApplyPipelineUpdateModeOnlyResetsPolicy(t *testing.T) {
	p := &Project{PipelineMode: pipeline.ModeSuggest, PipelinePolicy: pipeline.DefaultPolicy(pipeline.ModeSuggest).Marshal()}
	err := applyPipelineUpdate(p, UpdateProjectRequest{PipelineMode: strPtr(pipeline.ModeAuto)})
	if err != nil {
		t.Fatalf("applyPipelineUpdate: %v", err)
	}
	if p.PipelineMode != pipeline.ModeAuto {
		t.Fatalf("mode = %q, want auto", p.PipelineMode)
	}
	got, _, err := resolvePipeline(p.PipelineMode, p.PipelinePolicy)
	if err != nil {
		t.Fatalf("resolvePipeline: %v", err)
	}
	if !got.Equal(pipeline.DefaultPolicy(pipeline.ModeAuto)) {
		t.Fatal("mode-only update must reset policy to the auto preset")
	}
}

func TestApplyPipelineUpdatePolicyWinsOverMode(t *testing.T) {
	p := &Project{PipelineMode: pipeline.ModeSuggest, PipelinePolicy: pipeline.DefaultPolicy(pipeline.ModeSuggest).Marshal()}
	auto := pipeline.DefaultPolicy(pipeline.ModeAuto)
	req := UpdateProjectRequest{
		PipelineMode:   strPtr(pipeline.ModeOff),
		PipelinePolicy: rawPtr(auto.Marshal()),
	}
	if err := applyPipelineUpdate(p, req); err != nil {
		t.Fatalf("applyPipelineUpdate: %v", err)
	}
	if p.PipelineMode != pipeline.ModeAuto {
		t.Fatalf("mode = %q, want auto (policy wins)", p.PipelineMode)
	}
	got, _, _ := resolvePipeline(p.PipelineMode, p.PipelinePolicy)
	if !got.Equal(auto) {
		t.Fatal("policy must be stored verbatim when provided")
	}
}

func TestApplyPipelineUpdatePolicyWithoutModeDefaultsSuggest(t *testing.T) {
	p := &Project{PipelineMode: pipeline.ModeSuggest, PipelinePolicy: pipeline.DefaultPolicy(pipeline.ModeSuggest).Marshal()}
	raw := json.RawMessage(`{"discovery":{"debounce_hours":1},"apply":{"max_new_types_per_run":2}}`)
	req := UpdateProjectRequest{PipelinePolicy: rawPtr(raw)}
	if err := applyPipelineUpdate(p, req); err != nil {
		t.Fatalf("applyPipelineUpdate: %v", err)
	}
	if p.PipelineMode != pipeline.ModeSuggest {
		t.Fatalf("mode = %q, want suggest default", p.PipelineMode)
	}
	got, customized, err := resolvePipeline(p.PipelineMode, p.PipelinePolicy)
	if err != nil {
		t.Fatalf("resolvePipeline: %v", err)
	}
	if got.Discovery.DebounceHours != 1 || got.Apply.MaxNewTypesPerRun != 2 {
		t.Fatalf("overrides not applied: %+v", got)
	}
	if !customized {
		t.Fatal("overridden policy must be customized")
	}
}

func TestApplyPipelineUpdateRejectsEmptyMode(t *testing.T) {
	p := &Project{PipelineMode: pipeline.ModeSuggest, PipelinePolicy: pipeline.DefaultPolicy(pipeline.ModeSuggest).Marshal()}
	if err := applyPipelineUpdate(p, UpdateProjectRequest{PipelineMode: strPtr("")}); err == nil {
		t.Fatal("expected empty mode to be rejected")
	}
}
