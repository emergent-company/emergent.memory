# Error Handling

The SDK wraps all non-2xx API responses into a structured `*errors.Error` type and provides predicate functions to classify errors without type-asserting manually.

## Import

```go
import sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
```

## The `errors.Error` Type

```go
type Error struct {
    StatusCode int    // HTTP status code (e.g., 404, 403)
    Message    string // Human-readable error message from the server
}
```

`Error` implements the standard `error` interface.

## Predicate Functions

| Function | Status Code | Description |
|----------|-------------|-------------|
| `IsNotFound(err)` | 404 | Resource does not exist |
| `IsForbidden(err)` | 403 | Caller lacks permission |
| `IsUnauthorized(err)` | 401 | Missing or invalid credentials |
| `IsBadRequest(err)` | 400 | Malformed request |
| `ParseErrorResponse(resp)` | — | Parse an `*http.Response` into `*Error` |

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
    sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

func getObject(client *sdk.Client, id string) {
    obj, err := client.Graph.GetObject(context.Background(), id)
    if err != nil {
        switch {
        case sdkerrors.IsNotFound(err):
            fmt.Printf("Object %s not found\n", id)
        case sdkerrors.IsForbidden(err):
            fmt.Println("Permission denied — check your API key scope")
        case sdkerrors.IsUnauthorized(err):
            fmt.Println("Unauthorized — provide a valid API key")
        case sdkerrors.IsBadRequest(err):
            fmt.Printf("Bad request: %v\n", err)
        default:
            log.Printf("Unexpected error: %v", err)
        }
        return
    }
    fmt.Printf("Object: %s\n", obj.VersionID)
}
```

## Parsing Raw HTTP Responses

If you use `client.Do` to make raw HTTP requests, parse errors with `ParseErrorResponse`:

```go
resp, err := client.Do(ctx, req)
if err != nil {
    return err
}
if resp.StatusCode >= 400 {
    return sdkerrors.ParseErrorResponse(resp)
}
```

## Network vs API Errors

The predicates only return `true` for `*errors.Error` values. A network error (e.g., DNS failure, connection refused) is returned as a plain `error` and will not match any predicate — handle it with a default `else` or `default` branch.

See the [errors reference](reference/errors.md) for the full API.
