## Why

On the documents page, pressing **Upload** without first choosing a file posts an empty multipart form, which the API rejects with a 400 and leaves the user staring at an error with no hint of what to do. The upload control has two steps (choose, then upload) but only the second is obvious — the primary action fails silently-ish instead of guiding the user to the file input.

## What Changes

- Clicking **Upload** when no file has been chosen opens the native file picker instead of submitting an empty form. Once a file is chosen, Upload proceeds as before.

## Impact

- **Frontend (web UI)**: `apps/web-ui/gateway/documents.templ` — the upload form is tagged `data-document-upload` and the page ships a small delegated `submit` handler that opens the file dialog when the input is empty. No server, API, or schema changes.
