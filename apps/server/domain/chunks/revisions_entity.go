package chunks

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ChunkRevision is one immutable version of a chunk's text. seq is monotonic
// per chunk and defines revision ordering; the original content is seq 1.
type ChunkRevision struct {
	bun.BaseModel `bun:"table:kb.chunk_revisions,alias:cr"`

	ID        uuid.UUID `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ChunkID   uuid.UUID `bun:"chunk_id,type:uuid,notnull" json:"chunkId"`
	Seq       int       `bun:"seq,notnull" json:"seq"`
	Text      string    `bun:"text,notnull" json:"text"`
	Author    *string   `bun:"author" json:"author,omitempty"`
	CreatedAt time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// ChunkRevisionDTO is the API representation of a chunk revision.
type ChunkRevisionDTO struct {
	ID        string `json:"id"`
	ChunkID   string `json:"chunkId"`
	Seq       int    `json:"seq"`
	Text      string `json:"text"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// ToDTO converts a ChunkRevision to its API representation.
func (r *ChunkRevision) ToDTO() *ChunkRevisionDTO {
	author := ""
	if r.Author != nil {
		author = *r.Author
	}
	return &ChunkRevisionDTO{
		ID:        r.ID.String(),
		ChunkID:   r.ChunkID.String(),
		Seq:       r.Seq,
		Text:      r.Text,
		Author:    author,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

// ListChunkRevisionsResponse is the response for listing a chunk's revisions.
type ListChunkRevisionsResponse struct {
	Data       []*ChunkRevisionDTO `json:"data"`
	TotalCount int                 `json:"totalCount"`
}

// EditChunkRequest is the body for editing a chunk's text.
type EditChunkRequest struct {
	Text string `json:"text" validate:"required"`
}

// RollbackChunkRequest is the body for rolling a chunk back to a revision.
type RollbackChunkRequest struct {
	Seq int `json:"seq" validate:"required,gte=1"`
}

// EditChunkResponse reports the revision created by an edit or rollback.
type EditChunkResponse struct {
	ChunkID  string `json:"chunkId"`
	Revision int    `json:"revision"`
}

// Diff line kinds.
const (
	DiffEqual  = "equal"
	DiffAdd    = "add"
	DiffRemove = "remove"
)

// ChunkDiffLine is one line of a text diff between two chunk revisions.
type ChunkDiffLine struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ChunkDiffResponse is the diff between two revisions of a chunk.
type ChunkDiffResponse struct {
	From  int             `json:"from"`
	To    int             `json:"to"`
	Lines []ChunkDiffLine `json:"lines"`
}
