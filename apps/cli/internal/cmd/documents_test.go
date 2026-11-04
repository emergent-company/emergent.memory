package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocumentsListFlags(t *testing.T) {
	for _, name := range []string{"limit", "cursor", "status", "sort", "order", "search", "source-type"} {
		require.NotNil(t, documentsListCmd.Flags().Lookup(name), "list flag %q should be defined", name)
	}
}

func TestDocumentsListOptionsMapping(t *testing.T) {
	prevLimit := docsLimitFlag
	prevCursor, prevStatus, prevSort := docsCursorFlag, docsStatusFlag, docsSortFlag
	prevOrder, prevSearch, prevSource := docsOrderFlag, docsSearchFlag, docsSourceTypeFlag
	t.Cleanup(func() {
		docsLimitFlag = prevLimit
		docsCursorFlag, docsStatusFlag, docsSortFlag = prevCursor, prevStatus, prevSort
		docsOrderFlag, docsSearchFlag, docsSourceTypeFlag = prevOrder, prevSearch, prevSource
	})

	docsLimitFlag = 25
	docsCursorFlag = "cur"
	docsStatusFlag = "ready"
	docsSortFlag = "updated"
	docsOrderFlag = "asc"
	docsSearchFlag = "report"
	docsSourceTypeFlag = "drive"

	opts := documentsListOptions()
	assert.Equal(t, 25, opts.Limit)
	assert.Equal(t, "cur", opts.Cursor)
	assert.Equal(t, "ready", opts.Status)
	assert.Equal(t, "updated", opts.Sort)
	assert.Equal(t, "asc", opts.Order)
	assert.Equal(t, "report", opts.Query)
	assert.Equal(t, "drive", opts.SourceType)
}

func TestDocumentsListHelpListsVocabularies(t *testing.T) {
	for _, want := range []string{
		"processing | ready | extracted | failed",
		"created (default) | updated",
		"desc (default) | asc",
		"metadata search (filename and source URL), NOT file content",
		"statusCounts",
	} {
		assert.Contains(t, documentsListCmd.Long, want)
	}
}
