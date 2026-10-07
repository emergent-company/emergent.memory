import SwiftUI

/// Presentation of a connection failure with a retry affordance
/// (OpenSpec task 3.4).
///
/// Styled on the shared token system: a serious-tinted warning badge, the card
/// background, the medium corner radius, and the prominent button style. The
/// state is a plain `MemoryFailure` enum, so switching presentation requires
/// no changes outside this view.
struct MemoryFailureView: View {
    let failure: MemoryFailure
    let onRetry: () -> Void

    var body: some View {
        VStack(spacing: 3 * .grid) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.title2)
                .foregroundStyle(.fgSerious)
                .padding(3 * .grid)
                .background(.bgSerious, in: Circle())

            Text(title)
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg0)
                .multilineTextAlignment(.center)

            description
                .font(MemoryFont.bodySmall)
                // `.secondary` on `bg2` is ~3.1:1 in light; a primary tint
                // clears 4.5:1 while staying visually secondary.
                .foregroundStyle(.fg1.opacity(0.8))
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)

            Button(action: onRetry) {
                Text("memory.failure.retry")
                    .padding(.horizontal, 6 * .grid)
                    .padding(.vertical, 2 * .grid)
            }
            .buttonStyle(ProminentButtonStyle())
            .padding(.top, 1 * .grid)
        }
        .padding(6 * .grid)
        .frame(maxWidth: 84 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusMedium))
        .overlay(
            RoundedRectangle(cornerRadius: .cornerRadiusMedium)
                .stroke(.separatorSerious, lineWidth: 1)
        )
        .shadow(color: .black.opacity(0.18), radius: 12, y: 4)
        .padding(4 * .grid)
    }

    /// Keyed headline for the failure kind (see `memory.failure.*`).
    private var title: LocalizedStringKey {
        switch failure {
        case .tokenFetch:
            "memory.failure.tokenFetch"
        case .serverUnreachable:
            "memory.failure.serverUnreachable"
        case .agentNotJoined:
            "memory.failure.agentNotJoined"
        case .micPermissionDenied:
            "memory.failure.micPermissionDenied"
        case .unknown:
            "memory.failure.unknown"
        }
    }

    /// Runtime detail from the failure, or a keyed generic when absent. The
    /// runtime message is rendered verbatim (not as a localization key) so it is
    /// never reinterpreted as markdown or a lookup key.
    private var description: Text {
        if let message = failure.errorDescription {
            return Text(verbatim: message)
        }
        return Text("memory.failure.description")
    }
}
