package agents

import (
	"os"
	"path/filepath"
	"testing"
)

// docSuiteDir resolves the doc-processing-suite checkout used by the protocol
// extraction E2E tests, skipping the test when it is unavailable.
//
// The dataset behind those comparisons is a private evaluation corpus: it is not
// part of this repository, and no contributor can fetch it. So the path is never
// hardcoded — point DOC_PROCESSING_SUITE_DIR at a checkout that contains an
// `output/` directory:
//
//	DOC_PROCESSING_SUITE_DIR=~/doc-processing-suite \
//	  go test ./domain/extraction/agents -run Protocol
func docSuiteDir(t *testing.T) string {
	t.Helper()

	dir := os.Getenv("DOC_PROCESSING_SUITE_DIR")
	if dir == "" {
		t.Skip("DOC_PROCESSING_SUITE_DIR not set, skipping: point it at a doc-processing-suite checkout containing output/")
	}
	if _, err := os.Stat(filepath.Join(dir, "output")); err != nil {
		t.Skipf("DOC_PROCESSING_SUITE_DIR=%s has no readable output/ directory, skipping", dir)
	}

	return dir
}
