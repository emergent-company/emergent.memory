//go:build !linux

package linux

import "errors"

// errTrashUnsupported is returned when trashing is attempted off Linux.
var errTrashUnsupported = errors.New("trashing is not supported on this platform")

// trashPathPlatform is the portable fallback: trashing is unavailable off
// Linux, so delete reports a clear unsupported error (permanent deletion is
// still possible when AllowPermanentDelete is set).
func trashPathPlatform(string) error {
	return errTrashUnsupported
}
