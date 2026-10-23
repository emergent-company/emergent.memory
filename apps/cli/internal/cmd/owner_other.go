//go:build !unix

package cmd

// adoptOwnership is a no-op where ownership cannot be read or changed from Go's
// standard library; the atomic replace then leaves the temp file's owner, as
// documented on replaceFileAtomically.
func adoptOwnership(original, replacement string) {}
