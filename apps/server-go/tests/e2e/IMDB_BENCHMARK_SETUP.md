# Setting up the IMDb Benchmark Project on `mcj-emergent`

To run the IMDb graph benchmark and stress-test the AI agent's multi-hop reasoning against a massive dataset, you must first create and seed a dedicated project using the official Emergent CLI and Go SDK.

## Prerequisites

1. SSH access to the `mcj-emergent` server (`ssh root@mcj-emergent`).
2. The Emergent CLI installed (`/root/.emergent/bin/emergent`).

## Step-by-Step Instructions

### 1. Create a New Project via the CLI

SSH into `mcj-emergent` and use the CLI to create the project:

```bash
/root/.emergent/bin/emergent projects create \
  --name 'IMDb Benchmark Project' \
  --description 'A massive dataset for stress-testing graph agent multi-hop reasoning.' \
  --output json
```

*Note the `ID` returned in the output. You will need to plug this into the seeder script.*

### 2. Generate an API Token with Write Permissions

Create a token specifically for the seeding script with `data:write` permissions:

```bash
/root/.emergent/bin/emergent tokens create \
  --name IMDb-Seeder-Write \
  --project-id <YOUR_PROJECT_ID> \
  --scopes data:read,data:write,schema:read
```

*Copy the `Token:` string starting with `emt_...`.*

### 3. Update the Seeder Script

Open `apps/server-go/cmd/seed-imdb/main.go` and update the constants with your newly created project and token:

```go
const ProjectID = "<YOUR_PROJECT_ID>"

func main() {
	// ...
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "<YOUR_TOKEN>"
	}
    // ...
}
```

### 4. Run the Seeder

The seeder uses the official `graph.BulkCreateObjects` and `graph.BulkCreateRelationships` SDK methods to stream massive datasets without hitting `out of shared memory` PostgreSQL limits. It tracks Canonical IDs automatically from the SDK responses.

Start the ingestion process (this will take several minutes to run as it deliberately throttles to avoid overwhelming the database):

```bash
cd apps/server-go
go run ./cmd/seed-imdb/main.go
```

Once complete, background embedding workers will kick in to generate semantic vectors for all objects and relationships.

### 5. Run the Benchmark!

Once the data is seeded, update your benchmark script (`tests/e2e/benchmark_script.go`) to point to the new project and let the agents loose.
