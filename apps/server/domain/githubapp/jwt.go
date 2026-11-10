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
	"fmt"
	"time"
)

// jwtLifetime keeps exp inside GitHub's 600 s cap with a margin. iat is
// backdated 60 s to tolerate clock skew.
const (
	jwtIssuedSkew = 60 * time.Second
	jwtLifetime   = 480 * time.Second
)

// Private-key parse failures. SignAppJWT returns them as-is; callers that
// validate a credential before sealing it classify them with errors.Is so they
// can name the offending field without duplicating the parser (see
// domain/sources.RegisterSourceApp).
var (
	// ErrPrivateKeyNotPEM means the value holds no PEM block at all.
	ErrPrivateKeyNotPEM = errors.New("githubapp: private key is not PEM encoded")
	// ErrPrivateKeyUnsupportedPEMType means the PEM block is not one of the two
	// RSA encodings GitHub issues. The message carries the block type.
	ErrPrivateKeyUnsupportedPEMType = errors.New("githubapp: unsupported private key PEM type")
	// ErrPrivateKeyNotRSA means the PEM block parsed but holds a non-RSA key.
	ErrPrivateKeyNotRSA = errors.New("githubapp: private key is not RSA")
	// ErrPrivateKeyTooWeak means the key parsed but its modulus is smaller than
	// MinRSAPrivateKeyBits. Returned wrapped in *WeakPrivateKeyError, which
	// carries the observed size.
	ErrPrivateKeyTooWeak = errors.New("githubapp: private key is too weak")
)

// MinRSAPrivateKeyBits is the smallest RSA modulus accepted for an App private
// key. The floor lives with the parser, not with one caller, so a weak key can
// neither sign an App JWT nor be stored at registration.
const MinRSAPrivateKeyBits = 2048

// WeakPrivateKeyError reports a parsed RSA key below MinRSAPrivateKeyBits. It
// unwraps to ErrPrivateKeyTooWeak, so errors.Is(err, ErrPrivateKeyTooWeak)
// works while the caller can still name the observed size.
type WeakPrivateKeyError struct {
	Bits int
}

func (e *WeakPrivateKeyError) Error() string {
	return fmt.Sprintf("%s: %d bits, minimum is %d", ErrPrivateKeyTooWeak, e.Bits, MinRSAPrivateKeyBits)
}

// Unwrap makes errors.Is(err, ErrPrivateKeyTooWeak) true.
func (e *WeakPrivateKeyError) Unwrap() error { return ErrPrivateKeyTooWeak }

// SignAppJWT signs the RS256 App JWT GitHub expects for App-level API calls.
// The private key may be PKCS#1 ("RSA PRIVATE KEY") or PKCS#8 ("PRIVATE KEY").
func SignAppJWT(appID int64, privateKeyPEM []byte, now time.Time) (string, error) {
	key, err := ParseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("githubapp: marshal jwt header: %w", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-jwtIssuedSkew).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", fmt.Errorf("githubapp: marshal jwt claims: %w", err)
	}

	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("githubapp: sign jwt: %w", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ParseRSAPrivateKey parses a PKCS#1 ("RSA PRIVATE KEY") or PKCS#8
// ("PRIVATE KEY") PEM-encoded RSA private key and enforces MinRSAPrivateKeyBits.
// It is the single parser behind both App-JWT minting and source-App
// registration validation, so a weak key is rejected on both paths. Errors never
// carry key bytes; they carry the reason, wrapped in the ErrPrivateKey*
// sentinels where the caller may need to classify them.
func ParseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, ErrPrivateKeyNotPEM
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("githubapp: parse PKCS#1 private key: %w", err)
		}
		key = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("githubapp: parse PKCS#8 private key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, ErrPrivateKeyNotRSA
		}
		key = rsaKey
	default:
		return nil, fmt.Errorf("%w %q", ErrPrivateKeyUnsupportedPEMType, block.Type)
	}
	// Both x509 parsers already ran Validate on the key, so only the modulus
	// size is left to check. The floor is shared with the JWT minter on
	// purpose: a weak key must not sign either.
	if bits := key.N.BitLen(); bits < MinRSAPrivateKeyBits {
		return nil, &WeakPrivateKeyError{Bits: bits}
	}
	return key, nil
}
