package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/upgrade"
)

// upgradeTimeout bounds the whole resolve + download + replace operation.
const upgradeTimeout = 5 * time.Minute

// newUpgradeClient builds the self-update client. It is a variable so
// command-level tests can inject a client aimed at an httptest server.
var newUpgradeClient = upgrade.NewClient

// newUpgradeCmd wires "memory-connector upgrade" into the cobra tree. The
// stdlib runUpgrade keeps the same output/exit-code contract as the other
// commands (0 success, 1 runtime error, 2 usage).
func newUpgradeCmd() *cobra.Command {
	return leaf("upgrade", "upgrade the memory-connector binary", `Upgrade the memory-connector binary in place from its GitHub releases.

Resolves the newest connector-v* release, downloads the archive for this OS
and architecture, verifies its SHA-256 checksum when published, and atomically
replaces the running executable.

Examples:
  memory-connector upgrade                    # upgrade to the newest release
  memory-connector upgrade --check            # report only; no download
  memory-connector upgrade --force            # reinstall the current release
  memory-connector upgrade --version 0.2.0    # install a pinned release`, func(cmd *cobra.Command) {
		cmd.Flags().Bool("check", false, "report current and latest versions without installing")
		cmd.Flags().Bool("force", false, "download and reinstall even when already up to date")
		cmd.Flags().String("version", "", "install a specific version instead of the newest (X.Y.Z)")
	}, runUpgrade)
}

// runUpgrade implements the upgrade command. It resolves the target release,
// short-circuits when already up to date (unless --force), and otherwise
// downloads and atomically replaces the running binary.
func runUpgrade(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "report current and latest versions without installing")
	force := fs.Bool("force", false, "download and reinstall even when already up to date")
	pin := fs.String("version", "", "install a specific version instead of the newest (X.Y.Z)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector upgrade: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	client := newUpgradeClient()
	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()

	current := upgrade.Normalize(version)

	var (
		rel *upgrade.Release
		err error
	)
	if *pin != "" {
		rel, err = client.ReleaseFor(*pin)
	} else {
		rel, err = client.Latest(ctx)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector upgrade: %v\n", err)
		return 1
	}

	upToDate := upgrade.Compare(current, rel.Version) >= 0

	if *check {
		_, _ = fmt.Fprintf(stdout, "current: %s\nlatest:  %s\n", current, rel.Version)
		if upToDate {
			_, _ = fmt.Fprintln(stdout, "memory-connector is up to date")
		} else {
			_, _ = fmt.Fprintln(stdout, "a newer version is available")
		}
		return 0
	}

	if upToDate && !*force {
		_, _ = fmt.Fprintf(stdout, "memory-connector is already up to date (%s)\n", current)
		return 0
	}

	_, _ = fmt.Fprintf(stdout, "downloading memory-connector %s...\n", rel.Version)
	if _, err := client.Install(ctx, rel); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector upgrade: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "memory-connector upgraded: %s -> %s\n", current, rel.Version)
	return 0
}
