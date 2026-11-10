import SwiftUI

/// The editable workflow strip of a work item: a status picker and an assignee
/// picker, the native twins of the web drawer's `objectWorkControls`.
///
/// The card only shows these when the server reports a work-item key and at
/// least one allowed status (`ObjectCard.canEditWorkflow`), because the two
/// writes the gateway offers are work-item routes.
///
/// No optimistic flip: each picker's selection getter reads the card, so the
/// control keeps showing the stored value until the refreshed card arrives. A
/// failed write leaves the old value in place and renders the reason inline —
/// the drawer's behaviour, which re-renders with its action error.
struct ObjectCardWorkControls: View {
    let store: ObjectCardStore
    let workflow: ObjectCardWorkflow

    var body: some View {
        LabeledContent("objects.card.status") {
            statusPicker
        }
        assigneeRow
        if let error = store.actionError {
            Label {
                Text(verbatim: error)
            } icon: {
                Image(systemName: "exclamationmark.circle")
            }
            .font(MemoryFont.meta)
            .foregroundStyle(.fgSerious)
            .accessibilityLabel(Text("objects.card.updateFailed") + Text(verbatim: ": \(error)"))
        }
    }

    // MARK: - Status

    /// The picker offers the type's allowed statuses in board order. A stored
    /// status outside that set (the type changed under the item) is kept as a
    /// leading option so the control can show what the object actually is.
    private var statusOptions: [ObjectCardStatus] {
        var options = workflow.allowedStatuses
        if let current = workflow.status, !options.contains(where: { $0.value == current.value }) {
            options.insert(current, at: 0)
        }
        return options
    }

    private var statusPicker: some View {
        Picker("objects.card.status", selection: statusSelection) {
            ForEach(statusOptions, id: \.value) { status in
                Text(verbatim: status.label).tag(status.value)
            }
        }
        .pickerStyle(.menu)
        .labelsHidden()
        .disabled(store.isSaving)
    }

    private var statusSelection: Binding<String> {
        Binding(
            get: { workflow.status?.value ?? "" },
            set: { value in
                guard value != (workflow.status?.value ?? "") else { return }
                Task { await store.setStatus(value) }
            }
        )
    }

    // MARK: - Assignee

    /// One picker option. The unassigned entry has an empty `value`, the exact
    /// payload that clears the assignment.
    private struct AssigneeOption: Identifiable, Hashable {
        let value: String
        let label: String

        var id: String {
            value
        }
    }

    /// Agents first, then members, then the stored assignee when the candidates
    /// do not include it (a member's name is not offered to a device).
    private var assigneeOptions: [AssigneeOption] {
        var options = [AssigneeOption(value: "", label: String(localized: "objects.card.unassigned"))]
        for candidate in workflow.assignees where !candidate.value.isEmpty {
            options.append(AssigneeOption(value: candidate.value, label: candidate.label))
        }
        if !workflow.assignee.isEmpty, !options.contains(where: { $0.value == workflow.assignee }) {
            options.append(AssigneeOption(value: workflow.assignee, label: workflow.assignee))
        }
        return options
    }

    @ViewBuilder private var assigneeRow: some View {
        if assigneeOptions.count > 1 {
            LabeledContent("objects.card.assignee") {
                assigneePicker
            }
        } else {
            // Nothing to choose from and nothing assigned: keep the read-only
            // dash rather than a one-entry menu.
            LabeledContent("objects.card.assignee") {
                Text(verbatim: "—")
                    .foregroundStyle(.fg3)
                    .accessibilityLabel(Text("objects.card.empty"))
            }
        }
    }

    private var assigneePicker: some View {
        Picker("objects.card.assignee", selection: assigneeSelection) {
            ForEach(assigneeOptions) { option in
                Text(verbatim: option.label).tag(option.value)
            }
        }
        .pickerStyle(.menu)
        .labelsHidden()
        .disabled(store.isSaving)
    }

    private var assigneeSelection: Binding<String> {
        Binding(
            get: { workflow.assignee },
            set: { value in
                guard value != workflow.assignee else { return }
                Task { await store.setAssignee(value) }
            }
        )
    }
}
