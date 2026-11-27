## 1. Core catalog types

- [x] 1.1 Add `Role` (schema.org/Role, parent `Thing`) to `core.yaml` with properties name, description, start_date, end_date and labels role, position, title
- [x] 1.2 Add `Device` (schema.org/Device, parent `Product`) to `core.yaml` with properties name, description, manufacturer, model, url and labels device, machine

## 2. Business domain pack

- [x] 2.1 Create `business.yaml` with `DecisionRecord` (parent `CreativeWork`, labels decision/adr/decision log, properties name, description, decided_on, status, rationale)
- [x] 2.2 Add `Equipment` (parent `Device`, labels equipment/machine/hardware, properties name, description, manufacturer, model, serial_number)
- [x] 2.3 Add `StorageSystem` (parent `Device`, labels storage system/battery system/energy storage technology, properties name, description, capacity, technology)

## 3. Version and counts

- [x] 3.1 Bump `00-manifest.yaml` version 1.2.0 → 1.3.0
- [x] 3.2 Update `catalog_test.go` object-type count 66 → 71 and version assertion

## 4. Alias-aware discovery matching

- [x] 4.1 Normalize discovered and golden type names (lowercase, strip non-alphanumerics) in `scoreTypes`
- [x] 4.2 Add the alias map (Decision → DecisionRecord, EnergyStorageTechnology → StorageSystem, ActionItem → Task) and compare normalized names
- [x] 4.3 Add tests for the alias cases and the unchanged exact cases

## 5. Verification

- [x] 5.1 `gofmt -w` changed directories
- [x] 5.2 `go build ./...`
- [x] 5.3 `go test ./domain/catalog/... ./cmd/pipeline-eval/...`
- [x] 5.4 `openspec validate expand-catalog-business-types`
