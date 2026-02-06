package embeddingpolicies

// CreateRequest is the request body for creating an embedding policy
type CreateRequest struct {
	ProjectID        string   `json:"projectId" validate:"required,uuid"`
	ObjectType       string   `json:"objectType" validate:"required"`
	Enabled          *bool    `json:"enabled"`
	MaxPropertySize  *int     `json:"maxPropertySize" validate:"omitempty,min=1"`
	RequiredLabels   []string `json:"requiredLabels"`
	ExcludedLabels   []string `json:"excludedLabels"`
	RelevantPaths    []string `json:"relevantPaths"`
	ExcludedStatuses []string `json:"excludedStatuses"`
}

// UpdateRequest is the request body for updating an embedding policy
type UpdateRequest struct {
	Enabled          *bool    `json:"enabled"`
	MaxPropertySize  *int     `json:"maxPropertySize" validate:"omitempty,min=1"`
	RequiredLabels   []string `json:"requiredLabels"`
	ExcludedLabels   []string `json:"excludedLabels"`
	RelevantPaths    []string `json:"relevantPaths"`
	ExcludedStatuses []string `json:"excludedStatuses"`
}

// Response is the response body for embedding policy operations
type Response struct {
	ID               string   `json:"id"`
	ProjectID        string   `json:"projectId"`
	ObjectType       string   `json:"objectType"`
	Enabled          bool     `json:"enabled"`
	MaxPropertySize  *int     `json:"maxPropertySize"`
	RequiredLabels   []string `json:"requiredLabels"`
	ExcludedLabels   []string `json:"excludedLabels"`
	RelevantPaths    []string `json:"relevantPaths"`
	ExcludedStatuses []string `json:"excludedStatuses"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
}

// ToResponse converts an EmbeddingPolicy entity to a Response DTO
func ToResponse(p *EmbeddingPolicy) *Response {
	return &Response{
		ID:               p.ID,
		ProjectID:        p.ProjectID,
		ObjectType:       p.ObjectType,
		Enabled:          p.Enabled,
		MaxPropertySize:  p.MaxPropertySize,
		RequiredLabels:   ensureArray(p.RequiredLabels),
		ExcludedLabels:   ensureArray(p.ExcludedLabels),
		RelevantPaths:    ensureArray(p.RelevantPaths),
		ExcludedStatuses: ensureArray(p.ExcludedStatuses),
		CreatedAt:        p.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt:        p.UpdatedAt.Format("2006-01-02T15:04:05.000Z"),
	}
}

// ToResponseList converts a slice of EmbeddingPolicy entities to Response DTOs
func ToResponseList(policies []EmbeddingPolicy) []Response {
	result := make([]Response, len(policies))
	for i, p := range policies {
		result[i] = *ToResponse(&p)
	}
	return result
}

// ensureArray returns an empty slice if input is nil
func ensureArray(arr []string) []string {
	if arr == nil {
		return []string{}
	}
	return arr
}
