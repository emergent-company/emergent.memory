package github

// Credentials is the credential payload for a GitHub source. GitHub OAuth App
// and personal access tokens are long-lived, so there is no refresh token and
// no client secret at sync time: the stored access token is the whole secret.
// It is never persisted in plaintext (the sources service encrypts the whole
// credential map) and never appears in an error message.
type Credentials struct {
	Token string `json:"access_token"`
}
