## 1. Response contract truncation

- [ ] 1.1 Add `maxPropertyValueChars` const (4000) to `response_contract.go`
- [ ] 1.2 Add `truncateProperties`, `truncatePropertyValue`, `truncateString` helpers
- [ ] 1.3 Apply in `propertiesForStrategy` (full and projection branches)
- [ ] 1.4 Apply in `slimRelationship`

## 2. Raw entity/edge emission truncation

- [ ] 2.1 Apply in `executeQueryEntities` (`Properties: e.Properties`)
- [ ] 2.2 Apply in `executeQueryEntitiesByIDs`
- [ ] 2.3 Apply in `executeSearchEntities`
- [ ] 2.4 Apply in `executeGetEntityEdges` incoming + outgoing
- [ ] 2.5 Apply in `getEntityBasicInfo`

## 3. Tests

- [ ] 3.1 Add truncation unit tests
- [ ] 3.2 `go test ./domain/mcp/...`

## 4. Verify

- [ ] 4.1 `go build ./...`
- [ ] 4.2 `go test ./domain/mcp/...`
- [ ] 4.3 `gofmt`
