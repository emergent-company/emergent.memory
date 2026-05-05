## ADDED Requirements

### Requirement: Worker interface in domain/extraction
`domain/extraction` SHALL define a `Worker` interface with `Start(ctx context.Context) error` and `Stop(ctx context.Context) error` methods. All extraction worker types SHALL satisfy this interface.

#### Scenario: All extraction workers implement Worker interface
- **WHEN** the codebase is compiled
- **THEN** `GraphEmbeddingWorker`, `GraphRelationshipEmbeddingWorker`, `ChunkEmbeddingWorker`, `DocumentParsingWorker`, `ObjectExtractionWorker`, and `EmbeddingSweepWorker` all implement the `Worker` interface

### Requirement: RegisterWorkerLifecycle generic helper
`domain/extraction` SHALL provide a `RegisterWorkerLifecycle(lc fx.Lifecycle, w Worker)` function that registers a single `fx.Hook` with `OnStart` calling `w.Start(context.Background())` and `OnStop` calling `w.Stop(ctx)`. This function SHALL be used for all worker lifecycle registrations in `extraction/module.go`.

#### Scenario: Module registers worker lifecycle via helper
- **WHEN** a developer adds a new extraction worker to the module
- **THEN** they call `RegisterWorkerLifecycle(lc, worker)` rather than writing a new `lc.Append(fx.Hook{...})` block

#### Scenario: extraction/module.go contains no inline lifecycle hook blocks
- **WHEN** the codebase is compiled after migration
- **THEN** `extraction/module.go` contains no direct `lc.Append(fx.Hook{OnStart: ..., OnStop: ...})` blocks; all such registrations go through `RegisterWorkerLifecycle`
