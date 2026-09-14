## 1. Data layer

- [x] 1.1 Add `CreateObjectRequest` struct and a `CreateObject` method to `MemoryClient` (POST `/api/graph/objects`, parse 201 `GraphObject`) and verify `go build ./...` passes
- [x] 1.2 Add `CreateObject` to the `MemoryBackend` interface and to the `fakeMemory` test double in `handlers_test.go` and verify `go build ./...` passes
- [x] 1.3 Add unit tests for the `CreateObject` client method (request marshalling, 201 response parsing, non-2xx error propagation) and verify `go test ./... -run CreateObject` passes

## 2. Handlers and routes

- [x] 2.1 Add a `uiObjectNew` GET handler that renders the create form with the project's compiled object types and verify a handler unit test passes
- [x] 2.2 Add a `uiObjectCreate` POST handler that validates the required `type`, builds the create payload, calls `CreateObject`, and redirects to `/objects/:id`; on validation/create failure re-render the form with an error and preserved values, and verify a handler unit test passes
- [x] 2.3 Register `GET /objects/new` and `POST /objects` routes in `main.go` and verify `go build ./...` passes
- [x] 2.4 Add unit tests for `uiObjectCreate` (missing type → rejected with values preserved; valid submit → `CreateObject` called and redirect to new object; create failure → error with values preserved) and verify `go test ./... -run Object` passes

## 3. UI

- [x] 3.1 Add a "New object" action to the Objects browser page linking to `/objects/new` and verify the browser smoke test shows the button
- [x] 3.2 Build the create form template (type selector, key/status/labels inputs, per-type property inputs with JSON text for complex values) and verify `templ generate` succeeds and the form renders
- [x] 3.3 Render an empty-state with schema-install guidance when the project has no compiled object types and verify a template render test passes

## 4. Verification

- [x] 4.1 Run `templ generate` and `go build ./...` and verify both succeed
- [x] 4.2 Run `task lint` and verify it passes
- [x] 4.3 Restart the dev server and browser-test the full flow (open Objects → New object → submit → redirected to the new object's detail view with the entered fields)

## 5. Type-aware property inputs

- [x] 5.1 Replace `compiledTypePropertyNames` with `compiledTypePropertyDefs` returning `[]objectPropertyDef{Name,Type}` (parse `type` from each property's raw JSON) and verify a unit test passes
- [x] 5.2 Update `uiObjectNew`/`uiObjectCreate` to pass property defs (not just names) to the template and verify handler tests pass
- [x] 5.3 Render type-aware inputs in `ObjectCreatePage` — `date` → `type=date`, `number` → `type=number step=any`, `boolean` → checkbox (hidden `false` + checkbox `true`), `array`/`object` → textarea, `string`/unknown → text — and verify `templ generate` succeeds and render tests pass
- [x] 5.4 Add render/handler tests for the type-aware inputs (date/number/boolean/array/string widgets) and verify `go test ./...` passes
- [x] 5.5 Run `templ generate`, `go build ./...`, and `task lint` and verify all pass

## 6. Tag chip input + toggle switch

- [x] 6.1 Adopt go-daisy `form.TagList` for the labels field and `array`/`object` properties (chip add/remove, submits `name[idx]`) and verify render tests pass
- [x] 6.2 Switch `boolean` properties to a daisyUI `toggle` switch (hidden `false` + `class="toggle"` checkbox `true`) and verify render tests pass
- [x] 6.3 Add label autocomplete: fetch existing labels in `uiObjectNew`/`uiObjectCreate`, pass as `TagListProps.Suggestions` (native `<datalist>`), and verify render tests pass
- [x] 6.4 Update `uiObjectCreate` to parse indexed `labels[i]`/`prop_x[i]` values into slices and verify handler tests pass
- [x] 6.5 Fix go-daisy `TagList` Alpine bugs (inline `x-data` methods + template-literal `:name`), push to go-daisy `master`, pin via pseudo-version, and verify `go build ./...` + browser chip add/remove pass

## 7. Combobox autocomplete dropdown

- [x] 7.1 Replace the native `<datalist>` with a Chakra-style combobox dropdown in go-daisy `TagList` — `filteredSuggestions` computed (case-insensitive, excludes added tags), `open`/`activeIndex` state, ArrowUp/Down highlight, Enter select-or-add, Escape close, `@click.outside` dismiss — and verify `templ generate` + `go build ./...` pass
- [x] 7.2 Update gateway render tests (listbox instead of datalist) and verify `go test ./...` passes
- [x] 7.3 Push go-daisy combobox change upstream, pin via pseudo-version, and verify `go build ./...` + `task lint` pass
