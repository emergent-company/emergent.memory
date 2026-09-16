## ADDED Requirements

### Requirement: Fixtures package exists as importable Go sub-package
The `fixtures/` directory SHALL be a Go package named `e2efixtures` under module `github.com/emergent-company/emergent.memory.e2e/fixtures`. It SHALL contain reusable test fixture data structs and builder functions used by multiple tests.

#### Scenario: Package compiles independently
- **WHEN** `go build ./fixtures/...` is run
- **THEN** the build succeeds with no errors

### Requirement: Bookstore fixture moved to fixtures package
The `bookstoreWorkspace` fixture (currently in root package `bookstore_fixture.go`) SHALL be moved to `fixtures/bookstore.go` as exported type `BookstoreWorkspace` in `package e2efixtures`.

#### Scenario: Fixture is importable by test files
- **WHEN** a test file imports `github.com/emergent-company/emergent.memory.e2e/fixtures`
- **THEN** it can reference `fixtures.BookstoreWorkspace` and all associated builder functions

#### Scenario: Original bookstore_fixture.go removed
- **WHEN** the migration is complete
- **THEN** no file named `bookstore_fixture.go` exists in the root package

### Requirement: Root package no longer contains non-test Go files
After migration, the root package SHALL contain only `*_test.go` files plus `go.mod` / `go.sum`. All non-test Go source files SHALL live in sub-packages.

#### Scenario: No non-test .go files in root
- **WHEN** `ls *.go` is run (excluding `_test.go` files)
- **THEN** no `.go` source files are listed (only test files remain)
