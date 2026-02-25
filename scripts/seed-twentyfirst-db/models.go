package main

type CreateGraphObjectRequest struct {
	Type       string         `json:"type"`
	Key        *string        `json:"key,omitempty"`
	Status     *string        `json:"status,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type PatchGraphObjectRequest struct {
	Properties    map[string]any `json:"properties,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
	ReplaceLabels bool           `json:"replaceLabels,omitempty"`
	Status        *string        `json:"status,omitempty"`
}

type BulkCreateObjectsRequest struct {
	Items []CreateGraphObjectRequest `json:"items"`
}

type BulkCreateObjectsResponse struct {
	Success int                      `json:"success"`
	Failed  int                      `json:"failed"`
	Results []BulkCreateObjectResult `json:"results"`
}

type BulkCreateObjectResult struct {
	Index   int                  `json:"index"`
	Success bool                 `json:"success"`
	Object  *GraphObjectResponse `json:"object,omitempty"`
	Error   *string              `json:"error,omitempty"`
}

type GraphObjectResponse struct {
	ID          string `json:"id"`
	CanonicalID string `json:"canonical_id"`
}

type CreateGraphRelationshipRequest struct {
	Type       string         `json:"type"`
	SrcID      string         `json:"src_id"`
	DstID      string         `json:"dst_id"`
	Properties map[string]any `json:"properties,omitempty"`
}

type BulkCreateRelationshipsRequest struct {
	Items []CreateGraphRelationshipRequest `json:"items"`
}

type BulkCreateRelationshipsResponse struct {
	Success int                            `json:"success"`
	Failed  int                            `json:"failed"`
	Results []BulkCreateRelationshipResult `json:"results"`
}

type BulkCreateRelationshipResult struct {
	Index   int     `json:"index"`
	Success bool    `json:"success"`
	Error   *string `json:"error,omitempty"`
}
