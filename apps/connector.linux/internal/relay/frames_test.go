package relay

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
)

func TestParseRegisterFrame(t *testing.T) {
	data := []byte(`{"type":"register","instance_id":"mbp-connector","version":"0.1.0","tools":{"tools":[{"name":"notes_search"}]}}`)
	frame, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reg, ok := frame.(*RegisterFrame)
	if !ok {
		t.Fatalf("frame type = %T, want *RegisterFrame", frame)
	}
	if reg.Type != FrameRegister || reg.InstanceID != "mbp-connector" || reg.Version != "0.1.0" {
		t.Errorf("reg = %+v", reg)
	}
	tools, ok := reg.Tools["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Errorf("nested tools shape lost: %v", reg.Tools)
	}
}

func TestParseRequestFrame(t *testing.T) {
	data := []byte(`{"type":"request","id":"relay-1","payload":{"jsonrpc":"2.0","id":"relay-1","method":"tools/call","params":{"name":"notes_search","arguments":{"query":"x"}}}}`)
	frame, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	req, ok := frame.(*RequestFrame)
	if !ok {
		t.Fatalf("frame type = %T, want *RequestFrame", frame)
	}
	if req.ID != "relay-1" {
		t.Errorf("ID = %q", req.ID)
	}
	params := req.Payload["params"].(map[string]any)
	if params["name"] != "notes_search" {
		t.Errorf("params.name = %v", params["name"])
	}
}

func TestParseResponseFrameEchoesAnyIDShape(t *testing.T) {
	// Hub matches responses by id; id may be string or number.
	for _, raw := range []string{
		`{"type":"response","id":"relay-1","payload":{"ok":true}}`,
		`{"type":"response","id":7,"payload":{"ok":true}}`,
		`{"type":"response","id":null,"payload":{"ok":true}}`,
		`{"type":"response","id":"relay-2","payload":null,"error":"boom"}`,
	} {
		frame, err := Parse([]byte(raw))
		if err != nil {
			t.Fatalf("Parse(%s): %v", raw, err)
		}
		resp, ok := frame.(*ResponseFrame)
		if !ok {
			t.Fatalf("frame type = %T for %s", frame, raw)
		}
		if len(resp.ID) == 0 {
			t.Errorf("id not preserved for %s", raw)
		}
	}
}

func TestParsePingPongErrorRegistered(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{raw: `{"type":"ping"}`, want: &PingFrame{Type: FramePing}},
		{raw: `{"type":"pong"}`, want: &PongFrame{Type: FramePong}},
		{raw: `{"type":"error","message":"first frame must be register"}`, want: &ErrorFrame{Type: FrameError, Message: "first frame must be register"}},
		{raw: `{"type":"registered","instance_id":"mbp-connector"}`, want: &RegisteredFrame{Type: FrameRegistered, InstanceID: "mbp-connector"}},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			frame, err := Parse([]byte(tc.raw))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(frame, tc.want) {
				t.Errorf("Parse(%s) = %#v, want %#v", tc.raw, frame, tc.want)
			}
		})
	}
}

func TestParseMalformedJSON(t *testing.T) {
	for _, raw := range []string{
		``,
		`not json`,
		`{"type":}`,
		`{"type":"register"`, // truncated
	} {
		_, err := Parse([]byte(raw))
		if err == nil {
			t.Errorf("Parse(%q): expected error", raw)
			continue
		}
		if !errors.Is(err, ErrMalformedJSON) {
			t.Errorf("Parse(%q) error = %v, want ErrMalformedJSON", raw, err)
		}
	}
}

func TestParseUnknownFrameType(t *testing.T) {
	_, err := Parse([]byte(`{"type":"teleport","data":1}`))
	if err == nil {
		t.Fatal("Parse: expected error for unknown type")
	}
	if !errors.Is(err, ErrUnknownFrame) {
		t.Errorf("error = %v, want ErrUnknownFrame", err)
	}
}

func TestParseEmptyType(t *testing.T) {
	_, err := Parse([]byte(`{"instance_id":"x"}`))
	if !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("error = %v, want ErrMalformedJSON for missing type", err)
	}
}

func TestMarshalRegisterRoundTrip(t *testing.T) {
	reg := &RegisterFrame{
		Type:       FrameRegister,
		InstanceID: "mbp-connector",
		Version:    "0.1.0",
		Tools:      map[string]any{"tools": []any{map[string]any{"name": "notes_search"}}},
	}
	data, err := Marshal(reg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	frame, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(marshaled): %v", err)
	}
	got := frame.(*RegisterFrame)
	if got.InstanceID != reg.InstanceID || got.Version != reg.Version {
		t.Errorf("round trip mismatch: %+v vs %+v", got, reg)
	}
}

func TestMarshalResponseRoundTrip(t *testing.T) {
	resp := &ResponseFrame{
		Type:    FrameResponse,
		ID:      json.RawMessage(`"relay-9"`),
		Payload: map[string]any{"notes": []any{}},
	}
	data, err := Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Type != "response" || decoded.ID != "relay-9" {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestMarshalRejectsNonFrame(t *testing.T) {
	if _, err := Marshal(map[string]any{"tools": []any{}}); err == nil {
		t.Error("Marshal(non-frame) expected error")
	}
}

func TestRawID(t *testing.T) {
	if got := string(rawID("abc")); got != `"abc"` {
		t.Errorf("rawID = %s", got)
	}
}

func TestParseConfigFrame(t *testing.T) {
	data := []byte(`{"type":"config","id":"c1","version":7,"servers":[{"name":"github","transport":"stdio","enabled":true,"command":"github-mcp","args":["--stdio"],"env":{"GITHUB_TOKEN":"s3cret"},"url":"","headers":{}}]}`)
	frame, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cfg, ok := frame.(*ConfigFrame)
	if !ok {
		t.Fatalf("frame type = %T, want *ConfigFrame", frame)
	}
	if cfg.Type != FrameConfig || cfg.ID != "c1" || cfg.Version != 7 {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers length = %d, want 1", len(cfg.Servers))
	}
	s := cfg.Servers[0]
	if s.Name != "github" || s.Transport != "stdio" || s.Command != "github-mcp" {
		t.Errorf("server[0] = %+v", s)
	}
	if !s.IsEnabled() {
		t.Error("enabled=true must be honored")
	}
	if s.Env["GITHUB_TOKEN"] != "s3cret" {
		t.Errorf("env = %v", s.Env)
	}
	if len(s.Args) != 1 || s.Args[0] != "--stdio" {
		t.Errorf("args = %v", s.Args)
	}
}

func TestParseConfigFrameUnknownTransportAndEmpty(t *testing.T) {
	// Transport is a plain string at the wire level; parsing must not reject an
	// unknown value (validation happens later in mcphost.ValidateServers).
	frame, err := Parse([]byte(`{"type":"config","id":"c2","version":1,"servers":[]}`))
	if err != nil {
		t.Fatalf("Parse empty servers: %v", err)
	}
	cfg := frame.(*ConfigFrame)
	if cfg.ID != "c2" || cfg.Version != 1 || len(cfg.Servers) != 0 {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestParseConfigFrameMalformed(t *testing.T) {
	for _, raw := range []string{
		`{"type":"config","id":"c3","version":"not-a-number"}`,
		`{"type":"config","id":"c3","version":1,"servers":123}`,
		`{"type":"config","id":"c3",` + `}`,
	} {
		_, err := Parse([]byte(raw))
		if err == nil {
			t.Errorf("Parse(%q): expected error", raw)
			continue
		}
		if !errors.Is(err, ErrMalformedJSON) {
			t.Errorf("Parse(%q) error = %v, want ErrMalformedJSON", raw, err)
		}
	}
}

func TestMarshalConfigRoundTrip(t *testing.T) {
	enabled := true
	cfg := &ConfigFrame{
		Type:    FrameConfig,
		ID:      "c4",
		Version: 42,
		Servers: []mcphost.ServerConfig{
			{Name: "notes", Transport: mcphost.TransportStdio, Enabled: &enabled, Command: "notes-mcp", Args: []string{"--stdio"}},
		},
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	frame, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(marshaled): %v", err)
	}
	got := frame.(*ConfigFrame)
	if got.ID != "c4" || got.Version != 42 || len(got.Servers) != 1 {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if got.Servers[0].Command != "notes-mcp" {
		t.Errorf("server command = %q", got.Servers[0].Command)
	}
}
