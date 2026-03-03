# Errors

All `EmergentAPIClient` methods throw `EmergentAPIError`. Handle errors with a `do-catch` block or use Swift's structured concurrency error propagation.

**Source:** `Emergent/Core/EmergentAPIClient.swift` in `emergent-company/emergent.memory.mac`

---

## EmergentAPIError

```swift
enum EmergentAPIError: Error, LocalizedError {
    case notConfigured
    case invalidURL
    case unauthorized
    case notFound
    case serverError(statusCode: Int, message: String)
    case httpError(statusCode: Int)
    case network(Error)
    case decodingFailed(Error)
}
```

### Cases

| Case | When thrown |
|------|-------------|
| `notConfigured` | `configure(serverURL:apiKey:)` has not been called before making a request |
| `invalidURL` | The constructed request URL is malformed |
| `unauthorized` | Server returned HTTP 401 — API key is missing, invalid, or expired |
| `notFound` | Server returned HTTP 404 — the requested resource does not exist |
| `serverError(statusCode:message:)` | Server returned HTTP 5xx with a decodable error message body |
| `httpError(statusCode:)` | Server returned an HTTP error not covered by the cases above |
| `network(Error)` | A `URLSession` network-layer error (no connectivity, TLS failure, timeout, etc.) |
| `decodingFailed(Error)` | The response body could not be decoded into the expected type |

### Recommended handling pattern

```swift
do {
    let projects = try await EmergentAPIClient.shared.fetchProjects()
    // use projects
} catch EmergentAPIError.notConfigured {
    // Prompt user to configure the server connection
    showConnectionSetup()
} catch EmergentAPIError.unauthorized {
    // API key is invalid or expired — prompt re-authentication
    showReauthPrompt()
} catch EmergentAPIError.notFound {
    // Resource was deleted or the ID is wrong
    showNotFoundAlert()
} catch EmergentAPIError.serverError(let code, let message) {
    // 5xx from server
    logger.error("Server error \(code): \(message)")
} catch EmergentAPIError.network(let underlying) {
    // URLSession error — check connectivity
    logger.error("Network error: \(underlying.localizedDescription)")
} catch EmergentAPIError.decodingFailed(let underlying) {
    // Unexpected response shape — may indicate API version mismatch
    logger.error("Decoding failed: \(underlying)")
} catch {
    logger.error("Unexpected error: \(error)")
}
```

### `localizedDescription`

`EmergentAPIError` conforms to `LocalizedError` and provides human-readable descriptions for all cases. You can display `error.localizedDescription` directly in alert messages for non-technical users.

---

## ConnectionState

`ConnectionState` tracks the live connection health of a server in the Emergent Mac app. It is used by the connection manager, not directly by `EmergentAPIClient`.

```swift
enum ConnectionState: Equatable {
    case unknown
    case connected
    case disconnected
    case error(String)
}
```

### Values

| Value | Meaning |
|-------|---------|
| `.unknown` | Connection state has not yet been determined (initial state) |
| `.connected` | Server is reachable and responding to health checks |
| `.disconnected` | Server is unreachable (network down, server stopped) |
| `.error(String)` | An error occurred; the associated string contains a human-readable message |

### Conformances

`ConnectionState` conforms to `Equatable`. The `.error(String)` case compares equal only when both the case and the associated string match.

```swift
switch connectionState {
case .unknown:
    ProgressView()
case .connected:
    Image(systemName: "checkmark.circle.fill").foregroundColor(.green)
case .disconnected:
    Image(systemName: "xmark.circle.fill").foregroundColor(.red)
case .error(let message):
    Text("Error: \(message)").foregroundColor(.orange)
}
```
