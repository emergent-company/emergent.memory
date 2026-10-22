import LiveKitComponents
import SwiftUI

/// The connect call-to-action on `StartView`.
///
/// Extracted from `StartView` so the accessibility sizing added for #1948 can be
/// measured by a hosted layout test (`AccessibilityLayoutTests`): the pill keeps
/// its 58×11-grid footprint at default text sizes, but at accessibility sizes
/// the width goes flexible and the height becomes a minimum, so a long label
/// grows the button instead of being clipped.
struct ConnectCTA: View {
    /// Runs when the pill is tapped. The caller owns tracing and the session
    /// controller, so this view stays a pure affordance.
    let action: () async -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Namespace private var button

    var body: some View {
        AsyncButton(action: action) {
            labelSizing {
                HStack {
                    Spacer()
                    Text("connect.start")
                        .matchedGeometryEffect(id: "connect", in: button)
                    Spacer()
                }
            }
        } busyLabel: {
            labelSizing {
                HStack(spacing: 4 * .grid) {
                    Spacer()
                    Spinner(tint: .white)
                        .transition(ConnectMotion.busySpinnerTransition(reduceMotion: reduceMotion))
                    Text("connect.connecting")
                        .matchedGeometryEffect(id: "connect", in: button)
                    Spacer()
                }
            }
        }
        #if os(visionOS)
        .buttonStyle(.borderedProminent)
        .controlSize(.extraLarge)
        #else
        .buttonStyle(ProminentButtonStyle())
        #endif
    }

    /// The connect CTA's footprint: a 58×11-grid pill at default text sizes.
    /// At accessibility sizes the width becomes flexible so the label can use
    /// the whole row, and the height is only a minimum so the label grows the
    /// button instead of being clipped by a fixed box (#1948).
    private func labelSizing(@ViewBuilder _ label: () -> some View) -> some View {
        label()
            .frame(width: dynamicTypeSize.isAccessibilitySize ? nil : 58 * .grid)
            .frame(minHeight: 11 * .grid)
    }
}
