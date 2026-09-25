## MODIFIED Requirements

### Requirement: Restore re-uploads files
Restore SHALL re-upload file payloads from the archive to the configured S3-compatible object store under their original `storage_key`, and SHALL restore the `documents.storage_key` column to reference them.

#### Scenario: Files are restored
- **WHEN** a restore completes for a project whose snapshot contains files
- **THEN** the files SHALL be present in the configured object store under their original storage keys
- **THEN** restored documents SHALL reference their correct storage keys
