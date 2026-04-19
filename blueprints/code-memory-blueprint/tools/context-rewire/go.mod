module github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/tools/context-rewire

go 1.24

require github.com/emergent-company/emergent.memory/apps/server/pkg/sdk v0.0.0

replace github.com/emergent-company/emergent.memory/apps/server/pkg/sdk => ../../../../apps/server/pkg/sdk
