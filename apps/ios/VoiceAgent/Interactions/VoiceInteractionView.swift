import SwiftUI

/// A multiplatform view that shows voice-specific interaction controls.
///
/// Shows the agent participant view (avatar camera or audio visualizer).
///
/// - Note: The layout is determined by the horizontal size class.
struct VoiceInteractionView: View {
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    var body: some View {
        if horizontalSizeClass == .regular {
            regular()
        } else {
            compact()
        }
    }

    private func regular() -> some View {
        HStack {
            Spacer()
                .frame(width: 50 * .grid)
            AgentView()
            Spacer()
                .frame(width: 50 * .grid)
        }
        .safeAreaPadding()
        // Cap and centre the wide layout so the centre agent column does not
        // stretch edge-to-edge on iPad.
        .frame(maxWidth: 200 * .grid)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func compact() -> some View {
        AgentView()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .ignoresSafeArea()
    }
}
