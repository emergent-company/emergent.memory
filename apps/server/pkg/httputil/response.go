package httputil

// APIResponse wraps API responses with a success flag, data payload, and optional error/message fields.
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

// NewSuccessResponse creates a successful API response wrapping data.
func NewSuccessResponse[T any](data T) APIResponse[T] {
	return APIResponse[T]{
		Success: true,
		Data:    data,
	}
}

// NewErrorResponse creates an error API response with the given error message.
func NewErrorResponse[T any](err string) APIResponse[T] {
	return APIResponse[T]{
		Success: false,
		Error:   &err,
	}
}

// PaginatedResponse wraps paginated API responses with items and pagination metadata.
type PaginatedResponse[T any] struct {
	Items      []T    `json:"items"`
	TotalCount int    `json:"totalCount"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	NextCursor string `json:"nextCursor,omitempty"`
}
