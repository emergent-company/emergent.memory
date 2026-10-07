import SwiftUI

/// The Projects list inside Settings.
///
/// Lists every onboarded connection with the active one checked, switches on
/// tap, removes with a confirmation, and offers "Add project" (which opens the
/// setup-QR scanner owned by `AppShellView`). The manual connection fields in
/// ``MemorySettingsView`` edit whichever project is active.
struct MemoryProjectsSection: View {
    @EnvironmentObject private var agentStore: AgentStore
    @EnvironmentObject private var projectStore: MemoryProjectStore

    /// Opens the setup-QR scanner.
    var onAddProject: () -> Void

    /// Project pending removal, for the confirmation dialog.
    @State private var projectPendingRemoval: MemoryProjectConnection?

    var body: some View {
        VStack(alignment: .leading, spacing: 3 * .grid) {
            title
            list
            addRow
            footer
        }
        .confirmationDialog(
            "projects.remove.confirm.title",
            isPresented: removalDialogBinding,
            titleVisibility: .visible
        ) {
            if let connection = projectPendingRemoval {
                Button("projects.remove", role: .destructive) {
                    remove(connection)
                }
            }
            Button("settings.cancel", role: .cancel) {
                projectPendingRemoval = nil
            }
        } message: {
            Text("projects.remove.confirm.message")
        }
    }

    // MARK: - Rows

    private var title: some View {
        Text("projects.section.title")
            .font(MemoryFont.metaEmphasis)
            .foregroundStyle(.fg3)
            .padding(.top, 2 * .grid)
    }

    private var list: some View {
        VStack(spacing: 0) {
            ForEach(Array(projectStore.connections.enumerated()), id: \.element.projectID) { index, connection in
                if index > 0 {
                    Divider().overlay(.separator2)
                }
                projectRow(connection)
            }
            if projectStore.connections.isEmpty {
                Text("projects.empty")
                    .font(MemoryFont.bodySmall)
                    .foregroundStyle(.fg3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(3 * .grid)
            }
        }
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }

    private func projectRow(_ connection: MemoryProjectConnection) -> some View {
        let isActive = connection.projectID == projectStore.activeProjectID
        return HStack(spacing: 2 * .grid) {
            Button {
                switchProject(connection)
            } label: {
                HStack(spacing: 2 * .grid) {
                    Image(systemName: isActive ? "checkmark.circle.fill" : "circle")
                        .font(.body)
                        .foregroundStyle(isActive ? .fgAccent : .fg3)
                    VStack(alignment: .leading, spacing: 0.5 * .grid) {
                        Text(connection.displayName)
                            .font(MemoryFont.cardHeader)
                            .foregroundStyle(.fg0)
                            .lineLimit(1)
                        if connection.projectName.isEmpty, connection.hasIdentity {
                            Text(connection.projectID)
                                .font(MemoryFont.meta)
                                .foregroundStyle(.fg3)
                                .lineLimit(1)
                        }
                    }
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(connection.displayName)
            .accessibilityAddTraits(isActive ? .isSelected : [])

            Button {
                projectPendingRemoval = connection
            } label: {
                Image(systemName: "trash")
                    .font(.subheadline)
                    .foregroundStyle(.fg3)
                    .padding(1 * .grid)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("projects.remove")
        }
        .padding(3 * .grid)
    }

    private var addRow: some View {
        Button(action: onAddProject) {
            HStack(spacing: 2 * .grid) {
                Image(systemName: "plus.circle.fill")
                    .font(.body)
                    .foregroundStyle(.fgAccent)
                Text("projects.add")
                    .font(MemoryFont.cardHeader)
                    .foregroundStyle(.fg0)
                Spacer()
                Image(systemName: "qrcode.viewfinder")
                    .font(.subheadline)
                    .foregroundStyle(.fg3)
            }
            .padding(3 * .grid)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var footer: some View {
        Text("projects.section.footer")
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
    }

    // MARK: - Actions

    private var removalDialogBinding: Binding<Bool> {
        Binding(
            get: { projectPendingRemoval != nil },
            set: {
                if !$0 {
                    projectPendingRemoval = nil
                }
            }
        )
    }

    private func switchProject(_ connection: MemoryProjectConnection) {
        guard connection.projectID != projectStore.activeProjectID else { return }
        projectStore.setActive(projectID: connection.projectID)
        agentStore.setActiveProject(projectStore.activeProjectID)
        Task { @MainActor in
            await agentStore.load()
        }
    }

    private func remove(_ connection: MemoryProjectConnection) {
        let wasActive = projectStore.activeProjectID == connection.projectID
        projectStore.remove(projectID: connection.projectID)
        projectPendingRemoval = nil
        guard wasActive else { return }
        agentStore.setActiveProject(projectStore.activeProjectID)
        Task { @MainActor in
            await agentStore.load()
        }
    }
}

#Preview {
    ScrollView {
        MemoryProjectsSection(onAddProject: {})
            .padding(4 * .grid)
    }
    .background(.bg1)
    .environmentObject(AgentStore())
    .environmentObject(MemoryProjectStore())
}
