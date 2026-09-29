## 1. Implementation

- [x] 1.1 Tag the documents upload form with `data-document-upload`
- [x] 1.2 Add the page-scoped submit handler that opens the file picker when no file is chosen (`apps/web-ui/gateway/documents.templ`)
- [x] 1.3 Pin the contract in `TestRenderDocumentsUploadPicker` (`apps/web-ui/gateway/documents_test.go`)

## 2. Verification

- [x] 2.1 `templ generate` + `go build ./...` in `apps/web-ui/gateway`
- [x] 2.2 `go test ./...` in `apps/web-ui/gateway`
- [x] 2.3 `task lint` in `apps/web-ui/gateway`
