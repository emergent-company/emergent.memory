package search

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandToParent_PopulatesParentText pins the small-to-big expansion (#251):
// a child hit with a parent_chunk_id must surface its parent's text in the
// separate ParentText field, while the child Text (the snippet) is preserved.
// Without the parent join (fetchParentTexts) the expansion cannot populate
// ParentText, so this fails if the join is removed.
func TestExpandToParent_PopulatesParentText(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	parentID := uuid.New()
	childID := uuid.New()

	mock.ExpectQuery(`kb\.chunks`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "text"}).
			AddRow(parentID, "full section text"))

	results := []*TextSearchResult{
		{ID: childID, Text: "child fragment", ParentChunkID: &parentID},
	}
	require.NoError(t, repo.ExpandToParent(context.Background(), results))
	require.Len(t, results, 1)
	assert.Equal(t, "child fragment", results[0].Text, "child snippet must be preserved")
	require.NotNil(t, results[0].ParentText, "parent text must be populated")
	assert.Equal(t, "full section text", *results[0].ParentText)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestExpandToParent_CapsParentText pins the expansion's own bound (#251 point 4):
// parents are stored up to 32 000 chars, but the expansion returns at most
// maxParentContextChars so an oversized section is not handed to a model raw.
func TestExpandToParent_CapsParentText(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	parentID := uuid.New()

	longText := strings.Repeat("x", maxParentContextChars*2)
	mock.ExpectQuery(`kb\.chunks`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "text"}).
			AddRow(parentID, longText))

	results := []*TextSearchResult{
		{ID: uuid.New(), Text: "child", ParentChunkID: &parentID},
	}
	require.NoError(t, repo.ExpandToParent(context.Background(), results))
	require.NotNil(t, results[0].ParentText)
	assert.Len(t, *results[0].ParentText, maxParentContextChars)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestExpandToParent_WithoutParentIsNoop ensures chunks without a parent (or
// whose parent is missing) are untouched and no parent query is issued.
func TestExpandToParent_WithoutParentIsNoop(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)

	results := []*TextSearchResult{
		{ID: uuid.New(), Text: "child", ParentChunkID: nil},
	}
	require.NoError(t, repo.ExpandToParent(context.Background(), results))
	assert.Nil(t, results[0].ParentText)
	assert.Equal(t, "child", results[0].Text)
	require.NoError(t, mock.ExpectationsWereMet(), "no parent query should be issued")
}

// TestTextResultToItem_PreservesSnippetAndExposesContextText pins the DTO
// mapping (#251 point 3): Snippet stays the matched child text for callers that
// want precision, while the parent text is exposed separately as ContextText.
func TestTextResultToItem_PreservesSnippetAndExposesContextText(t *testing.T) {
	svc := &Service{}
	parent := "parent section"
	res := &TextSearchResult{
		ID:         uuid.New(),
		DocumentID: uuid.New(),
		Text:       "child fragment",
		ParentText: &parent,
	}

	item := svc.textResultToItem(res)
	assert.Equal(t, "child fragment", item.Snippet)
	require.NotNil(t, item.ContextText)
	assert.Equal(t, "parent section", *item.ContextText)
}
