package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func pkcs1PEM(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func pkcs8PEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestSignAppJWT(t *testing.T) {
	key := testRSAKey(t)
	now := time.Unix(1_800_000_000, 0)

	for name, pemBytes := range map[string][]byte{
		"pkcs1": pkcs1PEM(key),
		"pkcs8": pkcs8PEM(t, key),
	} {
		t.Run(name, func(t *testing.T) {
			token, err := SignAppJWT(4884315, pemBytes, now)
			if err != nil {
				t.Fatalf("SignAppJWT: %v", err)
			}
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				t.Fatalf("token has %d parts, want 3", len(parts))
			}

			var header map[string]string
			rawHeader, _ := base64.RawURLEncoding.DecodeString(parts[0])
			if err := json.Unmarshal(rawHeader, &header); err != nil {
				t.Fatalf("decode header: %v", err)
			}
			if header["alg"] != "RS256" || header["typ"] != "JWT" {
				t.Fatalf("header = %#v", header)
			}

			var claims struct {
				Iss int64 `json:"iss"`
				Iat int64 `json:"iat"`
				Exp int64 `json:"exp"`
			}
			rawClaims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			if err := json.Unmarshal(rawClaims, &claims); err != nil {
				t.Fatalf("decode claims: %v", err)
			}
			if claims.Iss != 4884315 {
				t.Fatalf("iss = %d, want 4884315", claims.Iss)
			}
			if claims.Iat != now.Unix()-60 {
				t.Fatalf("iat = %d, want %d", claims.Iat, now.Unix()-60)
			}
			if got := claims.Exp - claims.Iat; got != 540 {
				t.Fatalf("lifetime = %d, want 540", got)
			}

			sig, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Fatalf("decode signature: %v", err)
			}
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
				t.Fatalf("signature does not verify: %v", err)
			}
		})
	}
}

func TestSignAppJWTRejectsBadPEM(t *testing.T) {
	if _, err := SignAppJWT(1, []byte("not a pem"), time.Now()); err == nil {
		t.Fatal("expected error for non-PEM key")
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("x")})
	if _, err := SignAppJWT(1, block, time.Now()); err == nil {
		t.Fatal("expected error for unsupported PEM type")
	}
}

// TestParseRSAPrivateKeyRejectsWeakKey pins the shared floor: a 1024-bit key is
// parsed and structurally valid, but neither the minter nor registration may
// use it.
func TestParseRSAPrivateKeyRejectsWeakKey(t *testing.T) {
	key := testRSAKeyWithBits(t, 1024)
	for name, pemBytes := range map[string][]byte{
		"pkcs1": pkcs1PEM(key),
		"pkcs8": pkcs8PEM(t, key),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseRSAPrivateKey(pemBytes)
			if !errors.Is(err, ErrPrivateKeyTooWeak) {
				t.Fatalf("err = %v, want ErrPrivateKeyTooWeak", err)
			}
			var weak *WeakPrivateKeyError
			if !errors.As(err, &weak) || weak.Bits != 1024 {
				t.Fatalf("err = %v, want WeakPrivateKeyError{Bits: 1024}", err)
			}
			if !strings.Contains(err.Error(), "1024") || !strings.Contains(err.Error(), "2048") {
				t.Fatalf("err = %q, want observed and minimum sizes", err)
			}
		})
	}
}

// testRSAKeyWithBits generates a key of an explicit size. 1024 is the smallest
// rsa.GenerateKey still accepts, which is exactly what the weak-key cases need.
func testRSAKeyWithBits(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generate %d-bit key: %v", bits, err)
	}
	return key
}
