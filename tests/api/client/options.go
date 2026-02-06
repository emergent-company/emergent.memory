package client

// requestOptions holds options for a single request.
type requestOptions struct {
	headers map[string]string
	query   map[string]string
}

// Option configures a request.
type Option func(*requestOptions)

// WithAuth sets the Authorization header with a Bearer token.
func WithAuth(token string) Option {
	return func(o *requestOptions) {
		o.headers["Authorization"] = "Bearer " + token
	}
}

// WithProjectID sets the X-Project-ID header.
func WithProjectID(projectID string) Option {
	return func(o *requestOptions) {
		o.headers["X-Project-ID"] = projectID
	}
}

// WithOrgID sets the X-Org-ID header.
func WithOrgID(orgID string) Option {
	return func(o *requestOptions) {
		o.headers["X-Org-ID"] = orgID
	}
}

// WithHeader sets a custom header.
func WithHeader(key, value string) Option {
	return func(o *requestOptions) {
		o.headers[key] = value
	}
}

// WithQuery sets a query parameter.
func WithQuery(key, value string) Option {
	return func(o *requestOptions) {
		o.query[key] = value
	}
}

// WithQueryParams sets multiple query parameters.
func WithQueryParams(params map[string]string) Option {
	return func(o *requestOptions) {
		for k, v := range params {
			o.query[k] = v
		}
	}
}
