package email

import (
	"fmt"
	"mime"
	"net/mail"
	"strings"
)

// validateHeaderValue rejects a header value containing CR or LF. A raw line
// break inside a header value is the classic SMTP/MIME header-injection vector:
// it lets an attacker terminate the current header and write new ones. Any
// caller-influenced value that contains CR/LF must fail closed, so this returns
// an error naming the field rather than silently altering the value.
func validateHeaderValue(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("email header %q contains CR/LF (header injection)", field)
	}
	return nil
}

// formatAddress validates and canonicalises a "Name <address>" header value.
// The address is parsed with net/mail, which rejects CR/LF and malformed
// addresses, and the display name is checked for CR/LF before re-serialisation
// (net/mail quotes the name when it contains special characters). An empty
// display name yields the bare addr-spec, matching the historical To header
// format.
func formatAddress(name, addr string) (string, error) {
	if err := validateHeaderValue("display name", name); err != nil {
		return "", err
	}
	if err := validateHeaderValue("address", addr); err != nil {
		return "", err
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		return "", fmt.Errorf("invalid email address %q: %w", addr, err)
	}
	if name == "" {
		return parsed.Address, nil
	}
	return (&mail.Address{Name: name, Address: parsed.Address}).String(), nil
}

// encodeSubject validates a subject and encodes it as an RFC 2047 encoded-word
// when it contains non-ASCII characters. The validation rejects CR/LF outright;
// Q-encoding additionally renders any remaining line-break byte as =0D/=0A
// rather than a raw CR/LF, so a subject can never inject a header.
func encodeSubject(subject string) (string, error) {
	if err := validateHeaderValue("Subject", subject); err != nil {
		return "", err
	}
	return mime.QEncoding.Encode("utf-8", subject), nil
}
