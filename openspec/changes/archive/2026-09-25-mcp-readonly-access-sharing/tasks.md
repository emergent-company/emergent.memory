## 1. Email Template

- [x] 1.1 Create `apps/server/templates/email/mcp-invite.hbs` with sections for project name, MCP URL, API key (in `<code>`), sender name, and step-by-step Claude Desktop and Cursor config instructions
- [x] 1.2 Ensure the template does not reference CLI install steps or account sign-up links

## 2. Backend — Share Service Method

- [x] 2.1 Add `ShareMCPAccessRequest` and `ShareMCPAccessResponse` structs to `apps/server/domain/mcp/` (request: `name string`, `emails []string`; response: `token`, `mcpUrl`, `projectId`, `snippets.claudeDesktop`, `snippets.cursor`)
- [x] 2.2 Add `ShareMCPAccess(ctx, projectID, userID string, req ShareMCPAccessRequest) (*ShareMCPAccessResponse, error)` method to `mcp.Service` that calls `apitokenSvc.Create` with `viewerReadOnlyScopes` and builds the snippet strings
- [x] 2.3 Add email dispatch loop in `ShareMCPAccess`: for each address in `req.Emails`, call `emailSvc.Enqueue` with template `mcp-invite` and the required `TemplateData` fields (`projectName`, `mcpUrl`, `apiKey`, `projectId`, `senderName`, `snippets`)
- [x] 2.4 Wire `emailSvc *email.JobsService` into `mcp.Service` via `ServiceParams` and `NewService`

## 3. Backend — HTTP Handler & Route

- [x] 3.1 Add `HandleShareMCPAccess(c echo.Context) error` to `mcp.Handler`: parse and validate request body (validate email format, return 422 on invalid), require project admin role, call `svc.ShareMCPAccess`, return JSON response
- [x] 3.2 Register route `POST /api/projects/:projectId/mcp/share` in `mcp/routes.go` pointing to `h.HandleShareMCPAccess`

## 4. Verification

- [x] 4.1 Confirm `POST /api/projects/:projectId/mcp/share` returns `token`, `mcpUrl`, `projectId`, and `snippets` for a project admin
- [x] 4.2 Confirm the generated token appears in `GET /api/projects/:projectId/tokens` with read-only scopes
- [x] 4.3 Confirm a non-admin receives HTTP 403
- [x] 4.4 Confirm an invalid email in `emails[]` returns HTTP 422 and no token is created
- [x] 4.5 Confirm the `mcp-invite` email template renders without errors when all required variables are provided

