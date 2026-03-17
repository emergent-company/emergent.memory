## Why

Documents currently have opaque statuses that don't reflect the actual processing pipeline: a document that needs no conversion sits in an undifferentiated state, and once extraction runs there is no way for users to trigger it on demand, see its progress, or review a summary of what was found. This leaves users guessing whether a document is "ready," "processing," or "stalled."

## What Changes

- **Richer document status model**: Replace the implicit "no status = unknown" pattern with explicit statuses that distinguish the full lifecycle — `pending_conversion`, `ready_for_extraction`, `extracting`, `completed`, `failed` — so the UI can present a clear, accurate state at all times.
- **Trigger extraction from the document UI**: Users can initiate extraction on a document directly from the documents list/detail view without navigating to a separate extraction-jobs admin panel.
- **Extraction summary on documents**: After extraction completes, each document exposes a summary of what was extracted (object counts by type, relationship counts, last-run timestamp) visible in the document list and detail modal.
- **Extraction progress visibility**: While extraction is running, the document shows live progress (e.g., "Extracting… 12/40 chunks processed") via SSE-driven updates.
- **Status badge improvements in UI**: The documents page renders meaningful status badges for both conversion and extraction states, replacing missing/blank states with actionable labels.

## Capabilities

### New Capabilities

- `document-status-lifecycle`: Defines the full set of document processing statuses covering conversion and extraction phases, including transitions and business rules for when each status applies.
- `document-extraction-trigger`: Ability for users to trigger extraction on a specific document from the documents UI, returning immediately with a job reference.
- `document-extraction-summary`: Per-document extraction summary — objects created by type, relationships created, last extraction timestamp — exposed via the document API and displayed in the UI.

### Modified Capabilities

<!-- No existing specs require requirement-level changes -->

## Impact

- **Backend**: `apps/server/domain/documents/` — document entity, service, repository, handler (new computed/stored status fields, new extraction summary endpoint)
- **Backend**: `apps/server/domain/extraction/` — object extraction jobs service/handler (new per-document summary query, new admin-facing trigger endpoint on documents)
- **Frontend**: `/root/emergent.memory.ui/src/pages/admin/apps/documents/` — document list page (status badges, trigger button, progress indicator, summary column)
- **Frontend**: `/root/emergent.memory.ui/src/components/organisms/DocumentDetailModal/` — processing status tab (live progress, extraction summary)
- **Database**: Possible new computed view or stored column for `extraction_summary` on `kb.documents`; no breaking schema changes
