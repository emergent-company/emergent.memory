package main

import "io"

// runDaemon runs the relay client as a long-lived daemon. It shares the relay
// implementation but additionally holds an exclusive single-instance lock
// beside the config file, so two daemons cannot serve the same config.
func runDaemon(args []string, stdout, stderr io.Writer) int {
	return runRelayCommand("daemon", args, stdout, stderr, true)
}
