import SwiftUI

/// Settings tab: the projects this device is connected to.
///
/// Connection details (control-plane URL, token endpoint, API key, base URL)
/// are never edited here. They arrive with the setup QR code and are owned by
/// ``MemoryProjectStore``, which mirrors the active connection into
/// `MemoryConfig` — so the screen is a project list, not a form: list,
/// switch, remove, add.
struct MemorySettingsView: View {
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    /// Presents the setup-QR scanner (owned by `AppShellView`).
    var onAddProject: () -> Void = {}

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 5 * .grid) {
                header()
                MemoryProjectsSection(onAddProject: onAddProject)
            }
            .padding(4 * .grid)
            .padding(.bottom, 2 * .grid)
            // Constrain the list to a comfortable reading width and centre it
            // on regular-width devices (iPad).
            .frame(maxWidth: horizontalSizeClass == .regular ? 128 * .grid : .infinity)
            .frame(maxWidth: .infinity)
        }
        .background(.bg1)
    }

    // MARK: - Header

    /// This screen is the Settings tab root, so it has no dismiss affordance.
    private func header() -> some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Text("settings.title")
                .font(MemoryFont.screenTitle)
                .foregroundStyle(.fg0)
            Text("settings.intro")
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
        }
    }
}

#Preview {
    MemorySettingsView()
        .environment(AgentStore())
        .environmentObject(MemoryProjectStore())
}
