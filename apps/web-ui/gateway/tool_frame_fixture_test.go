package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The shared presentation contract for one live tool call. The fixture holds a
// worker frame (`upstream`) and the enrichment the gateway adds in place
// (`expected`); the frame a client receives is the two merged. The same file is
// read by the iOS test (VoiceAgentTests/ToolFrameFixtureTests.swift), so a
// change to the wording, the casing or the shape of `toolCategory`,
// `toolSubject` or `toolView` breaks one of the two surfaces instead of
// silently making them describe the same call differently.
//
// The fixture lives under apps/ios/ because the Mac build syncs only that
// subtree: the app's tests can read it there, and this test reaches across.
const toolFrameFixturePath = "../../ios/VoiceAgentTests/Fixtures/tool-call-frames.json"

type toolFrameFixture struct {
	Frames []struct {
		Upstream json.RawMessage        `json:"upstream"`
		Expected map[string]interface{} `json:"expected"`
	} `json:"frames"`
}

func TestChatToolFramesMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(toolFrameFixturePath))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture toolFrameFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(fixture.Frames) == 0 {
		t.Fatal("fixture has no frames")
	}

	for i, frame := range fixture.Frames {
		var upstream map[string]json.RawMessage
		if err := json.Unmarshal(frame.Upstream, &upstream); err != nil {
			t.Fatalf("frame %d: decode upstream: %v", i, err)
		}
		var tool, status string
		if err := json.Unmarshal(upstream["tool"], &tool); err != nil {
			t.Fatalf("frame %d: tool: %v", i, err)
		}
		if err := json.Unmarshal(upstream["status"], &status); err != nil {
			t.Fatalf("frame %d: status: %v", i, err)
		}

		data := string(frame.Upstream)
		var out bytes.Buffer
		if err := emitToolResultHTML(
			&out,
			[]byte("data: "+data+"\n\n"),
			data,
			tool,
			status,
			upstream["result"],
		); err != nil {
			t.Fatalf("frame %d: enrich: %v", i, err)
		}

		payload := bytes.TrimPrefix(bytes.TrimSpace(out.Bytes()), []byte("data: "))
		var emitted map[string]json.RawMessage
		if err := json.Unmarshal(payload, &emitted); err != nil {
			t.Fatalf("frame %d: decode emitted frame: %v", i, err)
		}

		// The original fields survive the re-emit verbatim, so the client sees
		// exactly the upstream frame plus the enrichment.
		for key, want := range upstream {
			got, ok := emitted[key]
			if !ok {
				t.Errorf("frame %d: upstream field %s missing from the emitted frame", i, key)
				continue
			}
			if !sameJSON(got, want) {
				t.Errorf("frame %d: upstream %s = %s, want %s", i, key, got, want)
			}
		}

		// The enrichment is exactly what the fixture says it is: same keys, same
		// values, so the iOS half of the fixture cannot drift into testing a
		// frame the gateway never emits.
		for key, want := range frame.Expected {
			gotRaw, ok := emitted[key]
			if !ok {
				t.Errorf("frame %d: %s missing from the emitted frame", i, key)
				continue
			}
			var got interface{}
			if err := json.Unmarshal(gotRaw, &got); err != nil {
				t.Errorf("frame %d: decode %s: %v", i, key, err)
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("frame %d: %s = %#v, want %#v (update the fixture and the iOS test together)", i, key, got, want)
			}
		}

		// `resultHtml` is the one addition the fixture does not describe (the
		// clients parse the payload, not the highlighted fragment). Anything
		// else is an enrichment the fixture failed to record.
		for key := range emitted {
			if _, ok := upstream[key]; ok {
				continue
			}
			if _, ok := frame.Expected[key]; ok {
				continue
			}
			if key == "resultHtml" {
				continue
			}
			t.Errorf("frame %d: emitted field %s is not described by the fixture", i, key)
		}
	}
}

// sameJSON compares two raw JSON values by value, so key order and whitespace
// do not matter but a changed number, string or shape does.
func sameJSON(a, b json.RawMessage) bool {
	var left, right interface{}
	if err := json.Unmarshal(a, &left); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &right); err != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
