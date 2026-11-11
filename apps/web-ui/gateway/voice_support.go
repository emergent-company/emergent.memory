package main

import "sync/atomic"

// voiceSupported mirrors Config.VoiceDisabled for package-level templ helpers
// (settingsSubNav) that have no *Server receiver. It is set once at startup by
// main; tests may override it and restore it.
var voiceSupported atomic.Bool

func init() { voiceSupported.Store(true) }

// setVoiceSupported publishes the deployment's voice capability to the templ
// helpers and returns the value for startup logging.
func setVoiceSupported(supported bool) bool {
	voiceSupported.Store(supported)
	return supported
}
