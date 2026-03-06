package jobs

import "go.uber.org/fx"

// Module provides job queue infrastructure.
// Domain modules (email, extraction, etc.) should use these components
// to build their specific job services and workers.
//
// The pattern is:
//  1. Domain creates a service that embeds/uses Queue for job operations
//  2. Domain creates a worker using Worker with a custom process function
//  3. Domain registers the worker with fx lifecycle for start/stop
var Module = fx.Module("jobs",
	// No direct providers - this is a library module
	// Domain modules create their own Queue and Worker instances
)
