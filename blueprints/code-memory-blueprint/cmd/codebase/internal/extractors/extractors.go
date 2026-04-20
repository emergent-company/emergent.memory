// Package extractors provides bundled route extractor scripts for common frameworks.
package extractors

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed bundled/*
var bundledFS embed.FS

// KnownFrameworks lists the supported bundled extractor names.
var KnownFrameworks = []string{"nestjs"}

// ExtractToTemp writes the bundled extractor for the given framework to a temp file
// and returns its path. The caller is responsible for removing the file when done.
func ExtractToTemp(framework string) (string, error) {
	name := framework + ".js"
	data, err := bundledFS.ReadFile(filepath.Join("bundled", name))
	if err != nil {
		return "", fmt.Errorf("no bundled extractor for framework %q (available: nestjs)", framework)
	}

	tmp, err := os.CreateTemp("", "codebase-extractor-"+framework+"-*.js")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer tmp.Close()

	if _, err := tmp.Write(data); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("writing extractor: %w", err)
	}

	return tmp.Name(), nil
}
