-- +goose Up
ALTER TABLE kb.projects ADD COLUMN project_info text;
UPDATE kb.projects SET project_info = kb_purpose WHERE kb_purpose IS NOT NULL AND kb_purpose != '';
ALTER TABLE kb.projects DROP COLUMN kb_purpose;

-- +goose Down
ALTER TABLE kb.projects ADD COLUMN kb_purpose text;
UPDATE kb.projects SET kb_purpose = project_info WHERE project_info IS NOT NULL AND project_info != '';
ALTER TABLE kb.projects DROP COLUMN project_info;
