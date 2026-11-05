package blueprints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
)

// BuildMergePreviewPacks converts blueprint packs into the schema merge-preview
// request shape. ObjectTypes is marshalled with the same array encoding the
// server's pack application uses, so both sides parse identical definitions.
func BuildMergePreviewPacks(packs []PackFile) ([]sdkschemas.MergePreviewPack, error) {
	out := make([]sdkschemas.MergePreviewPack, 0, len(packs))
	for _, p := range packs {
		raw, err := json.Marshal(p.ObjectTypes)
		if err != nil {
			return nil, fmt.Errorf("marshal object types for pack %q: %w", p.Name, err)
		}
		out = append(out, sdkschemas.MergePreviewPack{
			Name:        p.Name,
			ObjectTypes: raw,
		})
	}
	return out, nil
}

// PreviewPackConflicts asks the server which incoming object type names already
// exist in the project and returns the property-level conflicts. It is
// read-only. A blueprint with no packs returns no conflicts without a request.
func PreviewPackConflicts(ctx context.Context, schemas *sdkschemas.Client, packs []PackFile) ([]sdkschemas.MergePreviewConflict, error) {
	previewPacks, err := BuildMergePreviewPacks(packs)
	if err != nil {
		return nil, err
	}
	if len(previewPacks) == 0 {
		return nil, nil
	}
	result, err := schemas.PreviewMerge(ctx, &sdkschemas.MergePreviewRequest{Packs: previewPacks})
	if err != nil {
		return nil, fmt.Errorf("preview object type conflicts: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return result.Conflicts, nil
}

// PrintMergeConflicts renders object-type conflicts for a human: type name,
// existing pack/version, incoming pack, added properties, and conflicting
// properties (which always keep the existing definition).
func PrintMergeConflicts(out io.Writer, conflicts []sdkschemas.MergePreviewConflict) {
	if len(conflicts) == 0 {
		return
	}
	_, _ = fmt.Fprintln(out, "Existing object type names detected:")
	for _, c := range conflicts {
		existing := c.ExistingSchemaName
		if existing == "" {
			existing = "(manual type)"
		}
		if c.ExistingSchemaVersion != "" {
			existing += " " + c.ExistingSchemaVersion
		}
		incoming := c.IncomingPackName
		if incoming == "" {
			incoming = "(unknown pack)"
		}
		_, _ = fmt.Fprintf(out, "  type %q: existing pack %q → incoming pack %q\n", c.TypeName, existing, incoming)

		if len(c.AddedProperties) > 0 {
			added := append([]string(nil), c.AddedProperties...)
			sort.Strings(added)
			_, _ = fmt.Fprintf(out, "    added properties: %s\n", strings.Join(added, ", "))
		}
		if len(c.ConflictingProperties) > 0 {
			names := make([]string, 0, len(c.ConflictingProperties))
			for _, pc := range c.ConflictingProperties {
				names = append(names, pc.Property)
			}
			sort.Strings(names)
			_, _ = fmt.Fprintf(out, "    conflicting properties (existing definition wins): %s\n", strings.Join(names, ", "))
		}
	}
}

// FormatMergeConflictNames returns a compact one-line description of the
// conflicting type names, suitable for an error message.
func FormatMergeConflictNames(conflicts []sdkschemas.MergePreviewConflict) string {
	if len(conflicts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		if c.IncomingPackName != "" {
			parts = append(parts, fmt.Sprintf("%s (pack %s)", c.TypeName, c.IncomingPackName))
			continue
		}
		parts = append(parts, c.TypeName)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
