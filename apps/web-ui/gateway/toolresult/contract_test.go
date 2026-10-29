package toolresult

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestStepContractMatchesParser pins the shared step contract both web surfaces
// render from: the chat chip (JS) and the recorded-session page (Go templ) read
// `testdata/envelope-search.step.json`, and this test is the only place the
// parser's output is compared against it. Change a wording rule and this fails
// first, then both surfaces pick the new line up from one file instead of two
// renderers drifting apart.
func TestStepContractMatchesParser(t *testing.T) {
	contract := stepContract(t)

	if got := Classify(contract.Tool); got != contract.Category {
		t.Errorf("Classify(%q) = %+v, want %+v", contract.Tool, got, contract.Category)
	}
	if got := Subject(contract.Input); got != contract.Subject {
		t.Errorf("Subject(%s) = %q, want %q", contract.Input, got, contract.Subject)
	}
	got := Parse(fixture(t, "envelope-search.json"))
	if !reflect.DeepEqual(got, contract.View) {
		t.Errorf("Parse(envelope-search) = %+v,\nwant %+v", got, contract.View)
	}
}

// stepContract is the decoded shared contract, read the same way by every test
// that asserts what a step reads like.
type stepContractFile struct {
	Tool     string          `json:"tool"`
	Input    json.RawMessage `json:"input"`
	Category Category        `json:"category"`
	Subject  string          `json:"subject"`
	View     *Presentation   `json:"view"`
}

func stepContract(t *testing.T) stepContractFile {
	t.Helper()
	var c stepContractFile
	if err := json.Unmarshal([]byte(fixture(t, "envelope-search.step.json")), &c); err != nil {
		t.Fatalf("decode step contract: %v", err)
	}
	if c.Tool == "" || c.View == nil || c.Subject == "" {
		t.Fatalf("incomplete step contract: %+v", c)
	}
	return c
}
