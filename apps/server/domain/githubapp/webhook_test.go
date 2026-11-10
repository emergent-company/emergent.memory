package githubapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func signed(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	header := signed("s3cret", body)

	if !VerifyWebhook("s3cret", body, header) {
		t.Fatal("valid signature rejected")
	}
	if VerifyWebhook("other", body, header) {
		t.Fatal("wrong secret accepted")
	}
	if VerifyWebhook("", body, header) {
		t.Fatal("empty secret must fail closed")
	}
	if VerifyWebhook("s3cret", body, "sha1=abc") {
		t.Fatal("wrong algorithm accepted")
	}
	if VerifyWebhook("s3cret", body, "sha256=zz") {
		t.Fatal("non-hex signature accepted")
	}
	if VerifyWebhook("s3cret", append(body, ' '), header) {
		t.Fatal("modified body accepted")
	}
}

func TestParseWebhook(t *testing.T) {
	body := []byte(`{
		"action": "synchronize",
		"installation": {"id": 99},
		"repository": {"full_name": "owner/repo", "html_url": "https://github.com/owner/repo"},
		"pull_request": {
			"number": 531,
			"title": "feat: x",
			"draft": false,
			"merged": false,
			"state": "open",
			"user": {"login": "mkucharz", "type": "User"},
			"base": {"ref": "dev", "sha": "base-sha"},
			"head": {"ref": "feat/x", "sha": "head-sha"}
		}
	}`)

	p, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	if p.Installation.ID != 99 {
		t.Fatalf("installation = %d", p.Installation.ID)
	}
	if p.Repository.FullName != "owner/repo" {
		t.Fatalf("repo = %q", p.Repository.FullName)
	}
	if p.PullNumber() != 531 || p.PullRequest.Head.SHA != "head-sha" || p.PullRequest.Base.Ref != "dev" {
		t.Fatalf("payload = %#v", p.PullRequest)
	}
	if !p.IsRelevantPullRequestAction() {
		t.Fatal("synchronize should be relevant")
	}
	if p.ShouldSkipPullRequest() {
		t.Fatal("open non-draft user PR should not be skipped")
	}
}

func TestShouldSkipPullRequest(t *testing.T) {
	cases := map[string]WebhookPullRequest{
		"draft":  {Draft: true},
		"merged": {Merged: true},
		"closed": {State: "closed"},
		// Other bots must still be reviewed; only the integration's own bot is
		// skipped by the producer via AuthoredBy.
		"other-bot": {State: "open", User: WebhookUser{Login: "dependabot[bot]", Type: "Bot"}},
	}
	for name, pr := range cases {
		p := &WebhookPayload{Action: "opened", PullRequest: pr}
		if name == "other-bot" {
			if p.ShouldSkipPullRequest() {
				t.Error("other-bot: must not be skipped")
			}
			continue
		}
		if !p.ShouldSkipPullRequest() {
			t.Errorf("%s: expected skip", name)
		}
	}
}

func TestAuthoredBy(t *testing.T) {
	p := &WebhookPayload{PullRequest: WebhookPullRequest{User: WebhookUser{Login: "memory-reviewer[bot]"}}}
	if !p.AuthoredBy("memory-reviewer") {
		t.Fatal("App bot form must match")
	}
	if p.AuthoredBy("dependabot") {
		t.Fatal("a different bot must not match")
	}
	if p.AuthoredBy("") {
		t.Fatal("empty login must not match")
	}
}
