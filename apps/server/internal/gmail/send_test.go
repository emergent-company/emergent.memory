package gmail

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildRawMessageRejectsHeaderInjection(t *testing.T) {
	for name, msg := range map[string]OutgoingMessage{
		"to CRLF":      {To: "a@example.com\r\nBcc: evil@example.com", Subject: "x"},
		"subject CRLF": {To: "a@example.com", Subject: "x\r\nBcc: evil@example.com"},
		"invalid to":   {To: "not an address", Subject: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := buildRawMessage(msg); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestBuildRawMessageEncodesValidMessage(t *testing.T) {
	raw, err := buildRawMessage(OutgoingMessage{To: "a@example.com", Subject: "Hi", Text: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "To: a@example.com") || !strings.Contains(string(decoded), "Body") {
		t.Fatalf("decoded message = %s", decoded)
	}
}
