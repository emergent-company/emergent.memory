module github.com/emergent-company/emergent.memory/tools/e2e-suite

go 1.24.0

replace github.com/emergent-company/emergent.memory/apps/server/pkg/sdk => ../../apps/server/pkg/sdk

require (
	github.com/emergent-company/emergent.memory/apps/server/pkg/sdk v0.0.0
	github.com/joho/godotenv v1.5.1
)
