package sources

import "strings"

// reconnectPhrase is the part every connector's dead-credential remediation
// shares. It is matched as text rather than through a sentinel type because each
// provider pins its own wording and the sync path shows it verbatim in the job
// history and the source's error row: wrapping the errors would change what the
// user reads. processingproblems.Classify keys on the same phrase to classify the
// failure as a non-retryable permission problem.
const reconnectPhrase = "reconnect the source"

// IsReconnectRequired reports whether err is a sync failure only a fresh consent
// can clear: the provider no longer accepts the source's stored credential
// (Google's revoked or expired grant, Trale's expired token). Such a failure is
// terminal — retrying repeats a provider call that must fail — so the worker
// parks the source instead of leaving it active for the next sweep.
//
// Transient credential failures (a token endpoint 5xx, a timeout, a network
// error) are deliberately not matched: the connectors do not map them to the
// remediation, so they stay retryable.
func IsReconnectRequired(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), reconnectPhrase)
}

// reconnectRemediation is the actionable copy for a source that must be
// reconnected, used where the failure is refused before a provider is asked
// (a manual sync on a parked source). Providers with published copy keep it;
// everything else gets the provider-neutral wording, which carries the same
// phrase so it classifies and reads the same way.
func reconnectRemediation(provider string) string {
	switch provider {
	case ProviderTypeGoogleDrive:
		return driveReconnectRequired
	case ProviderTypeGmail:
		return gmailReconnectRequired
	default:
		return "the connection for this source has expired or was revoked; reconnect the source to continue"
	}
}
