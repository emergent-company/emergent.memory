## Why

We need a systematic and automated way to verify the performance and success rate of document extraction within the "huma" test project. Using a real-world dataset hosted on Google Drive ensures our extraction metrics are grounded in actual use cases. Automating this process allows for repeatable, reliable performance monitoring and regression testing.

## What Changes

- Create a new, independent Go application to serve as the extraction test suite.
- Implement a Google Drive synchronization mechanism to fetch and sync documents from a specific folder (`16qesqkUSHJTdKZCMoZtMe9GtesDxGE0A`).
- Utilize the Emergent SDK to programmatically upload the synced documents to the "huma" test project.
- Implement verification logic to track, measure, and report on the extraction success rate and performance metrics (e.g., time taken, accuracy, failure rates).

## Capabilities

### New Capabilities

- `huma-test-suite`: A standalone Go application responsible for Google Drive synchronization, uploading documents via the Emergent SDK, and verifying extraction performance and success rates.

### Modified Capabilities

## Impact

- **New Application:** Introduces a standalone Go testing tool separate from the main backend services.
- **Dependencies:** Requires the Emergent SDK and Google Drive API client libraries (e.g., `google.golang.org/api/drive/v3`).
- **Configuration/Auth:** Needs API credentials for Google Drive access (Service Account or OAuth) and Emergent API keys targeting the "huma" project.
