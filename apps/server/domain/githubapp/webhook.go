package githubapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SignatureHeader is the HMAC-SHA256 header GitHub signs webhook bodies with.
const SignatureHeader = "X-Hub-Signature-256"

// DeliveryHeader is the per-delivery id used for ingress dedupe.
const DeliveryHeader = "X-GitHub-Delivery"

// VerifyWebhook reports whether header ("sha256=<hex>") is a valid HMAC-SHA256
// of body under secret, compared in constant time. An empty secret fails closed.
func VerifyWebhook(secret string, body []byte, header string) bool {
	if secret == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
