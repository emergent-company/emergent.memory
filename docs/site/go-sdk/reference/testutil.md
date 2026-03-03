# testutil

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil`

The `testutil` package provides test infrastructure for writing unit tests against SDK clients. It is intended for use in `_test.go` files only.

## MockServer

```go
type MockServer struct { /* ... */ }

func NewMockServer(t *testing.T) *MockServer
func (ms *MockServer) On(method, path string, handler http.HandlerFunc)
func (ms *MockServer) OnJSON(method, path string, statusCode int, response interface{})
func (ms *MockServer) Close()
```

`MockServer` starts a real `httptest.Server` and routes requests by `method + path`.

```go
func TestListProjects(t *testing.T) {
    ms := testutil.NewMockServer(t)
    defer ms.Close()

    ms.OnJSON("GET", "/api/projects", http.StatusOK, []projects.Project{
        testutil.FixtureProject(),
    })

    client, _ := sdk.New(sdk.Config{
        ServerURL: ms.URL,
        Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test-key"},
    })

    result, err := client.Projects.List(context.Background(), nil)
    // assert result...
}
```

## Assertion Helpers

```go
func AssertHeader(t *testing.T, r *http.Request, key, expected string)
func AssertMethod(t *testing.T, r *http.Request, expected string)
func AssertJSONBody(t *testing.T, r *http.Request, expected interface{})
```

These helpers call `t.Fatal` on mismatch, making test failures clear.

```go
ms.On("POST", "/api/graph/objects", func(w http.ResponseWriter, r *http.Request) {
    testutil.AssertMethod(t, r, "POST")
    testutil.AssertHeader(t, r, "X-Project-ID", "proj_123")
    testutil.AssertJSONBody(t, r, &graph.CreateObjectRequest{Type: "Note"})
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(testutil.FixtureGraphObject())
})
```

## Fixtures

Pre-built test fixtures for common types:

```go
testutil.FixtureProject()          // *projects.Project
testutil.FixtureProjects()         // []projects.Project
testutil.FixtureOrganization()     // *orgs.Organization
testutil.FixtureUserProfile()      // *users.UserProfile
testutil.FixtureAPIToken()         // *apitokens.APIToken
testutil.FixtureHealthResponse()   // *health.HealthResponse
testutil.FixtureDocument()         // *documents.Document
testutil.FixtureAgent()            // *agents.Agent
testutil.FixtureAgentRun()         // *agents.AgentRun
testutil.FixtureAgentQuestion()    // *agents.AgentQuestion
testutil.FixtureAgentRunMessage()  // *agents.AgentRunMessage
testutil.FixtureAgentRunToolCall() // *agents.AgentRunToolCall
```
