// Package e2eframework — redact.go
//
// Secret masking for CLI invocations recorded by the runlog harness.
//
// runlog records the invocation and output of every CLI command it observes.
// Test suites routinely pass credentials on the command line
// (`--api-key`, `--token`, `-p`, `--client-secret`, `Authorization:` headers),
// so the raw argv must never reach the run log, the database, or the test
// output. The helpers here are the single source of truth for that masking:
// RedactArgs/FormatInvocation mask argv, and RedactText masks an already
// rendered invocation or command output. Both paths call the same primitives,
// so the stored record and the printed step output cannot drift apart.
package e2eframework

import (
	"regexp"
	"sort"
	"strings"
)

// RedactedValue is the placeholder written in place of a secret.
const RedactedValue = "***"

// sensitiveFlags are CLI flags whose following value (or `=value`) is a
// secret. Comparison is exact: `--token-id` is not treated as `--token`.
var sensitiveFlags = map[string]bool{
	"--api-key":       true,
	"--api_key":       true,
	"--apikey":        true,
	"--token":         true,
	"--api-token":     true,
	"--access-token":  true,
	"--refresh-token": true,
	"--auth-token":    true,
	"-p":              true,
	"--password":      true,
	"--client-secret": true,
	"--secret":        true,
}

// sensitivePositionalKeys are subcommand words or `config set` keys whose next
// argument is a secret even though it is not passed as a flag. They cover
// `memory set-token <token>` and `memory config set api_key <key>`.
var sensitivePositionalKeys = map[string]bool{
	"set-token":     true,
	"api_key":       true,
	"apikey":        true,
	"client_secret": true,
	"password":      true,
}

// secretFlagTextRe matches secret flags inside an already-rendered string
// (command line echo, error message, captured output). It is built from
// sensitiveFlags so the argv and text paths cannot diverge.
var secretFlagTextRe = regexp.MustCompile(
	`(?i)(` + strings.Join(longSecretFlagPatterns(), "|") + `)(\s*=\s*|\s+)(\S+)`)

// longSecretFlagPatterns returns the quoted long-form flag names, sorted for a
// deterministic pattern.
func longSecretFlagPatterns() []string {
	patterns := make([]string, 0, len(sensitiveFlags))
	for flag := range sensitiveFlags {
		if strings.HasPrefix(flag, "--") {
			patterns = append(patterns, regexp.QuoteMeta(flag))
		}
	}
	sort.Strings(patterns)
	return patterns
}

// shortPasswordTextRe matches `-p value` in free text. The leading boundary
// keeps it from matching inside a longer word or flag.
var shortPasswordTextRe = regexp.MustCompile(`(^|\s)(-p)(\s+)(\S+)`)

// authHeaderTextRe matches the value of an Authorization or X-API-Key header
// in free text. The value is bounded to one credential token (optionally
// preceded by an auth scheme such as Bearer) so masking does not swallow the
// rest of a rendered command line.
var authHeaderTextRe = regexp.MustCompile(`(?i)((?:authorization|x-api-key)"?\s*:\s*)((?:(?:bearer|basic|token)\s+)?[^\s"',;]+)`)

// RedactText masks credentials inside an arbitrary string: Authorization /
// X-API-Key header values and `--secret-flag value` occurrences. Use it for
// text that is already rendered; use RedactArgs when the argv is still a slice.
func RedactText(s string) string {
	if s == "" {
		return s
	}
	s = authHeaderTextRe.ReplaceAllString(s, "${1}"+RedactedValue)
	s = secretFlagTextRe.ReplaceAllString(s, "${1}${2}"+RedactedValue)
	s = shortPasswordTextRe.ReplaceAllString(s, "${1}${2}${3}"+RedactedValue)
	return s
}

// RedactArgs returns a copy of args with every secret value replaced by
// RedactedValue. It handles `--flag value`, `--flag=value`, `-p value`, and
// Authorization/X-API-Key header values (including headers embedded in an
// argument such as `-H "Authorization: Bearer ..."`).
func RedactArgs(args ...string) []string {
	masked, _ := redactScan(args)
	return masked
}

// redactScan returns the masked argv and the literal secret values it found.
// Keeping both in one pass means argv masking and captured-output scrubbing
// can never disagree about what a secret is.
func redactScan(args []string) ([]string, []string) {
	out := make([]string, len(args))
	var secrets []string
	addSecret := func(v string) {
		// Skip empty/very short values: scrubbing them from free-form output
		// would mangle unrelated text for no security benefit.
		if len(v) >= 3 {
			secrets = append(secrets, v)
		}
	}

	maskNext := false
	for i, arg := range args {
		if maskNext {
			out[i] = RedactedValue
			addSecret(arg)
			maskNext = false
			continue
		}
		if sensitiveFlags[arg] || sensitivePositionalKeys[arg] {
			out[i] = arg
			maskNext = true
			continue
		}
		if name, val, ok := strings.Cut(arg, "="); ok && sensitiveFlags[name] {
			out[i] = name + "=" + RedactedValue
			addSecret(val)
			continue
		}
		// Header values can appear anywhere in argv, e.g. as the value of
		// `-H` or `--header=Authorization: ...`.
		out[i] = RedactText(arg)
		for _, m := range authHeaderTextRe.FindAllStringSubmatch(arg, -1) {
			if len(m) > 2 {
				addSecret(m[2])
				if fields := strings.Fields(m[2]); len(fields) > 1 {
					addSecret(fields[len(fields)-1])
				}
			}
		}
	}
	return out, secrets
}

// RedactCapturedOutput masks credential values in captured command output.
// Recording a CLI step must not leak a secret the tool echoed without its
// flag, so the literal values seen in argv are scrubbed too.
func RedactCapturedOutput(output string, args ...string) string {
	output = RedactText(output)
	_, secrets := redactScan(args)
	for _, s := range secrets {
		output = strings.ReplaceAll(output, s, RedactedValue)
	}
	return output
}

// FormatInvocation renders `<binary> <args>` with secret values masked. This
// is the only supported way to build an invocation string for recording.
func FormatInvocation(binary string, args ...string) string {
	if len(args) == 0 {
		return binary
	}
	return binary + " " + strings.Join(RedactArgs(args...), " ")
}
