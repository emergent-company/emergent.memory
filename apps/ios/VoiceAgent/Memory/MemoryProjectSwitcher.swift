import SwiftUI

/// Global project switcher shown in the agents-picker header.
///
/// Shows the active project and, as a native `Menu`, lists every onboarded
/// connection with a checkmark on the active one. Choosing another project
/// re-scopes the whole app to that connection; the last item opens the setup-QR
/// scanner to add one more. It is presented from `AppShellView` — the only
/// place reachable from every screen — so switching never requires leaving the
/// agent picker.
struct MemoryProjectSwitcher: View {
    @EnvironmentObject private var projectStore: MemoryProjectStore

    /// Called when the user picks a (possibly different) project.
    var onSwitch: (MemoryProjectConnection) -> Void
    /// Called when the user chooses "Add project".
    var onAddProject: () -> Void

    var body: some View {
        Menu {
            ForEach(projectStore.connections, id: \.projectID) { connection in
                Button {
                    onSwitch(connection)
                } label: {
                    if connection.projectID == projectStore.activeProjectID {
                        Label(connection.displayName, systemImage: "checkmark")
                    } else {
                        Text(connection.displayName)
                    }
                }
            }
            Divider()
            Button(action: onAddProject) {
                Label("projects.add", systemImage: "qrcode.viewfinder")
            }
        } label: {
            label
        }
        .accessibilityLabel("projects.switcher.accessibility")
    }

    /// The compact pill: project glyph, active name, and a menu chevron.
    ///
    /// `AppShellView` only shows the pill when at least one project is
    /// connected, so the name always resolves; the first connection covers a
    /// transient mismatch between the active id and the stored list.
    private var label: some View {
        HStack(spacing: 1 * .grid) {
            Image(systemName: "square.stack.3d.up")
                .font(MemoryFont.label)
                .foregroundStyle(.fgAccent)
            Text(displayName)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.fg0)
                .lineLimit(1)
            Image(systemName: "chevron.up.chevron.down")
                .font(.caption2.weight(.semibold))
                .foregroundStyle(.fg3)
        }
        .padding(.horizontal, 3 * .grid)
        .padding(.vertical, 2 * .grid)
        .background(.bg2, in: Capsule())
        .contentShape(Capsule())
    }

    private var displayName: String {
        projectStore.activeConnection?.displayName
            ?? projectStore.connections.first?.displayName
            ?? String(localized: "projects.none")
    }
}

#Preview {
    MemoryProjectSwitcher(onSwitch: { _ in }, onAddProject: {})
        .environmentObject(MemoryProjectStore())
        .padding()
}
