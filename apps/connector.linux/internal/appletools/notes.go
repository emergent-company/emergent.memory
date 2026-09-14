package appletools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// maxNotesLimit caps notes_search results.
const maxNotesLimit = 200

// notesSearchScript emits JSON: [{"name":..., "snippet":...}, ...]. Snippet is
// the first line of the note body (<= 200 chars). query/folder are embedded as
// single-line AppleScript expressions; folder "" searches all folders.
func notesSearchScript(query, folder string, limit int) string {
	return escapeHelpers + fmt.Sprintf(`set q to %s
set f to %s
set mLimit to %d
set rows to ""
set matched to 0
tell application "Notes"
	if f is "" then
		set candidates to notes
	else
		set candidates to notes of folder f
	end if
	repeat with n in candidates
		if matched is greater than or equal to mLimit then
			exit repeat
		end if
		set nm to name of n
		set bodyText to plaintext of n
		if nm contains q or bodyText contains q then
			set paras to paragraphs of bodyText
			set snip to ""
			if (count of paras) is greater than 0 then
				set snip to item 1 of paras as text
			end if
			if (count of characters of snip) is greater than 200 then
				set snip to text 1 thru 200 of snip
			end if
			if rows is not "" then
				set rows to rows & ","
			end if
			set nmJSON to my escapeForJSON(nm)
			set snipJSON to my escapeForJSON(snip)
			set rows to rows & "{\"name\":" & nmJSON & ",\"snippet\":" & snipJSON & "}"
			set matched to matched + 1
		end if
	end repeat
end tell
return "[" & rows & "]"
`, appleExpr(query), appleExpr(folder), limit)
}

// notesCreateScript emits JSON: {"title":..., "created":true}. Apple Notes
// exposes no stable note id via AppleScript, so success reports title only.
func notesCreateScript(title, body string) string {
	return escapeHelpers + fmt.Sprintf(`set t to %s
set b to %s
set titleJSON to escapeForJSON(t)
tell application "Notes"
	make new note with properties {name:t, body:b}
end tell
return "{\"title\":" & titleJSON & ",\"created\":true}"
`, appleExpr(title), appleExpr(body))
}

// newNotesSearchTool builds the notes_search tool backed by r.
func newNotesSearchTool(r Runner) toolreg.Tool {
	return toolreg.Tool{
		Name:        "notes_search",
		Description: "Search Apple Notes notes whose name or body contains the query, within an optional folder; returns matching note names and first-line snippets.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":  map[string]any{"type": "string", "description": "text to search for in note names and bodies"},
				"folder": map[string]any{"type": "string", "description": "Notes folder name; empty searches all folders"},
				"limit":  map[string]any{"type": "integer", "description": "maximum number of results (default 20)"},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			query, err := requireString(args, "query")
			if err != nil {
				return nil, err
			}
			folder := optionalString(args, "folder")
			limit, err := optionalInt(args, "limit", 20)
			if err != nil {
				return nil, err
			}
			if limit < 1 || limit > maxNotesLimit {
				return nil, fmt.Errorf("argument %q must be between 1 and %d", "limit", maxNotesLimit)
			}

			out, err := r.Run(ctx, notesSearchScript(query, folder, limit), 0)
			if err != nil {
				return nil, mapRunnerError(err)
			}
			var rows []struct {
				Name    string `json:"name"`
				Snippet string `json:"snippet"`
			}
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				return nil, fmt.Errorf("notes_search: parse AppleScript output: %w (output: %s)", err, preview(out))
			}
			notes := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				notes = append(notes, map[string]any{"name": row.Name, "snippet": row.Snippet})
			}
			return map[string]any{"notes": notes}, nil
		},
	}
}

// newNotesCreateTool builds the notes_create tool backed by r.
func newNotesCreateTool(r Runner) toolreg.Tool {
	return toolreg.Tool{
		Name:        "notes_create",
		Description: "Create a new Apple Notes note in the default Notes folder with the given title and optional body.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string", "description": "note title"},
				"body":  map[string]any{"type": "string", "description": "note body (default empty)"},
			},
			"required": []string{"title"},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			title, err := requireString(args, "title")
			if err != nil {
				return nil, err
			}
			body := optionalString(args, "body")

			out, err := r.Run(ctx, notesCreateScript(title, body), 0)
			if err != nil {
				return nil, mapRunnerError(err)
			}
			var result struct {
				Title   string `json:"title"`
				Created bool   `json:"created"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				return nil, fmt.Errorf("notes_create: parse AppleScript output: %w (output: %s)", err, preview(out))
			}
			return map[string]any{"title": result.Title, "created": result.Created}, nil
		},
	}
}
