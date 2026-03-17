package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Show what's new since your current version",
	Long: `Display the aggregated changelog between your installed version and the latest release.

For dev builds, shows the latest release changelog only.
If you are already on the latest version, a message is shown instead.`,
	RunE: runChangelog,
}

func runChangelog(cmd *cobra.Command, args []string) error {
	latest, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVersion := normalizeVersion(latest.TagName)
	currentVersion := normalizeVersion(Version)

	// Dev build: show the latest release only (no meaningful "since" version)
	if currentVersion == "dev" || currentVersion == "unknown" {
		fmt.Printf("You are running a development build. Showing latest release (%s):\n", latestVersion)
		releases, err := fetchReleasesBetween("0.0.0", latestVersion)
		if err != nil || len(releases) == 0 {
			// Fall back to just the single latest release
			releases = []Release{*latest}
		} else {
			// Limit to just the single latest for dev builds
			releases = releases[:1]
		}
		if out := formatChangelog(releases); out != "" {
			fmt.Print(out)
		} else {
			fmt.Println("No changelog available for the latest release.")
		}
		return nil
	}

	// Already up to date
	if compareVersions(latestVersion, currentVersion) <= 0 {
		displayCurrent := strings.TrimPrefix(Version, "v")
		fmt.Printf("You are up to date: %s\n", displayCurrent)
		return nil
	}

	// Show aggregated changelog from current to latest
	releases, err := fetchReleasesBetween(currentVersion, latestVersion)
	if err != nil {
		return fmt.Errorf("failed to fetch changelog: %w", err)
	}

	if out := formatChangelog(releases); out != "" {
		fmt.Print(out)
	} else {
		fmt.Printf("No changelog entries found between %s and %s.\n", currentVersion, latestVersion)
		fmt.Printf("See full release notes: %s\n", releasesPageURL)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(changelogCmd)
}
