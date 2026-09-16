## Why

Projects are permanently bound to the organization that created them. When a project's scope, team, or billing moves — or a project is created under the wrong org — there is no way to correct the association short of recreating the project, which loses its history, settings, and members.

## What Changes

- Add a per-project **Transfer** action in the organization view's project row menu (alongside Open and Delete).
- Transfer moves a project from its current organization to a destination organization the user selects in a dialog.
- Add a new gateway POST route handling the transfer, backed by a new `MemoryClient`/backend capability that reparents the project's org association.
- The source organization view redirects back with a flash confirming the move (or an error, when the transfer is rejected).
- UI surfaces the action only when it is valid for the acting user; the backend remains the authority on authorization.

## Capabilities

### New Capabilities
- `project-transfer`: reparenting a project between organizations from the organization view — the row action, destination picker, gateway route and backend semantics that keep a project's identity while changing its owning organization.

### Modified Capabilities
<!-- None. The adjacent org-view capability (org-context) is an un-archived in-flight delta under
     openspec/changes/org-context-and-project-picker/ and is not merged into openspec/specs/ yet;
     this change introduces transfer behavior rather than editing that delta, mirroring the
     org-context change's own convention for building on in-flight work. -->

## Impact

- **Gateway UI**: per-project row action menu in the org landing (`gateway/org_context.templ`), a transfer dialog (destination-org picker), and role-aware visibility of the action.
- **Gateway routes/handlers**: new POST route registered in `gateway/main.go` alongside `/projects/delete`; handler modeled on `uiDeleteProjects` (`gateway/org_context.go`) with HTMX + PRG dual-path handling and flash redirect back to the source org.
- **Gateway client**: new `MemoryClient`/`MemoryBackend` method for the transfer (the current `UpdateProject` covers name/info fields only; org reassignment needs a dedicated operation), extending `gateway/memory.go` and `gateway/backend.go`.
- **Backend (Memory REST service)**: new or extended endpoint that reparents the project's org association and applies org-scope consequences (membership, role, access), enforced server-side. The backend service is external to this repo; the gateway change depends on that endpoint existing.
- **Overlaps** `org-context-and-project-picker` (org landing + row menu host) and `add-org-members-ui` (org membership model). This change adds one action to the org view rather than restructuring it.
- **Not in scope**: transferring organization ownership between users, org rename/merge, moving multiple projects in bulk.
