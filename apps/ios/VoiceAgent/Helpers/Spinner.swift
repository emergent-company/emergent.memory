import SwiftUI

/// A simple replacement for the native circular progress indicator.
///
/// The stroke is tinted with a foreground token (default `fgAccent`) instead of
/// a hardcoded white, so it stays visible on both light and dark backgrounds.
/// When the user has Reduce Motion enabled the continuous rotation is replaced
/// by a static three-quarter arc.
struct Spinner: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var rotation: Double = 0

    /// Stroke tint. Defaults to the accent token; pass `.white` when the
    /// spinner sits on an accent-filled surface (e.g. the connect button).
    var tint: Color = .fgAccent

    var body: some View {
        Group {
            if reduceMotion {
                Circle()
                    .trim(from: 0, to: 0.75)
                    .stroke(tint, style: StrokeStyle(lineWidth: 3, lineCap: .round))
                    .rotationEffect(.degrees(-90))
            } else {
                Circle()
                    .stroke(
                        AngularGradient(
                            gradient: Gradient(colors: [tint.opacity(0), tint]),
                            center: .center,
                            startAngle: .degrees(0),
                            endAngle: .degrees(360)
                        ),
                        style: StrokeStyle(lineWidth: 3, lineCap: .round)
                    )
                    .rotationEffect(.degrees(rotation))
                    .onAppear {
                        withAnimation(.linear(duration: 0.5).repeatForever(autoreverses: false)) {
                            rotation = 360
                        }
                    }
            }
        }
        .frame(width: 16, height: 16)
    }
}

#Preview {
    Spinner()
}
