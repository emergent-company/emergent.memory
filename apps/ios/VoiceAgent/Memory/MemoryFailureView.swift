import SwiftUI

/// Minimal, restyle-friendly presentation of a connection failure with a
/// retry affordance (OpenSpec task 3.4).
///
/// Kept intentionally plain: a separate design pass will replace the styling.
/// The state is a plain `MemoryFailure` enum, so switching presentation
/// requires no changes outside this view.
struct MemoryFailureView: View {
    let failure: MemoryFailure
    let onRetry: () -> Void

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .font(.system(size: 28))
                .foregroundStyle(.orange)

            Text(title)
                .font(.headline)

            Text(failure.errorDescription ?? "Unknown error")
                .font(.subheadline)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)

            Button("Retry", action: onRetry)
                .buttonStyle(.borderedProminent)
                .padding(.top, 4)
        }
        .padding(24)
        .frame(maxWidth: 340)
        .background(.regularMaterial)
        .clipShape(RoundedRectangle(cornerRadius: 16))
        .overlay(
            RoundedRectangle(cornerRadius: 16)
                .stroke(.separator, lineWidth: 1)
        )
        .shadow(radius: 8)
        .padding()
    }

    private var title: String {
        switch failure {
        case .tokenFetch:
            "Token server unreachable"
        case .serverUnreachable:
            "Server unreachable"
        case .agentNotJoined:
            "Agent not available"
        case .micPermissionDenied:
            "Microphone permission needed"
        case .unknown:
            "Connection failed"
        }
    }
}
