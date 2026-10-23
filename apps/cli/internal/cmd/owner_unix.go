//go:build unix

package cmd

import (
	"os"
	"syscall"
)

// adoptOwnership gives replacement the owning user and group of the file it is
// about to replace, so an atomic rewrite does not change who owns a credential
// file. Rename replaces the inode, so without this a `sudo memory config set`
// over a user-owned .env.local would leave it root-owned and the user's service
// unable to read it.
//
// Best effort by design: an unprivileged caller cannot chown, and a file it can
// write but not chown is still written correctly. The caveat is documented on
// replaceFileAtomically.
func adoptOwnership(original, replacement string) {
	info, err := os.Stat(original)
	if err != nil {
		return
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	_ = os.Chown(replacement, int(st.Uid), int(st.Gid))
}
