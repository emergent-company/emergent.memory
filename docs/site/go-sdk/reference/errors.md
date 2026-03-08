# errors

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors`

The `errors` package provides structured error handling for SDK API responses.

## The Error Type

```go
type Error struct {
    StatusCode int                    // HTTP status code
    Code       string                 // Machine-readable error code (e.g. "not_found")
    Message    string                 // Human-readable error message from the server
    Details    map[string]interface{} // Optional structured details from the server
}

func (e *Error) Error() string
```

All non-2xx API responses are returned as `*Error`. Network errors and other infrastructure failures are returned as plain `error` values.

## Predicate Functions

Use these instead of type-asserting `*Error` directly:

```go
func IsNotFound(err error) bool     // true if StatusCode == 404
func IsForbidden(err error) bool    // true if StatusCode == 403
func IsUnauthorized(err error) bool // true if StatusCode == 401
func IsBadRequest(err error) bool   // true if StatusCode == 400
```

Each function returns `false` for `nil` or for non-`*Error` values, making them safe to call on any `error`.

## ParseErrorResponse

```go
func ParseErrorResponse(resp *http.Response) error
```

Reads the response body and returns a `*Error` populated with the status code and any message from the JSON body. Useful when using `client.Do` for raw HTTP requests.

## Usage

```go
import sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"

obj, err := client.Graph.GetObject(ctx, id)
if err != nil {
    switch {
    case sdkerrors.IsNotFound(err):
        // handle 404
    case sdkerrors.IsForbidden(err):
        // handle 403
    case sdkerrors.IsUnauthorized(err):
        // handle 401
    case sdkerrors.IsBadRequest(err):
        // handle 400
    default:
        // network error or unexpected
    }
    return
}
```

## See Also

- [Error Handling guide](../error-handling.md)
