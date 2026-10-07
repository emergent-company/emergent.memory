import LiveKitComponents

/// A view that shows the screen share preview.
struct ScreenShareView: View {
    @EnvironmentObject private var localMedia: LocalMedia

    @Environment(\.namespace) private var namespace

    var body: some View {
        if let screenShareTrack = localMedia.screenShareTrack {
            let video = SwiftUIVideoView(screenShareTrack)
                .clipShape(RoundedRectangle(cornerRadius: .cornerRadiusPerPlatform))
                .aspectRatio(screenShareTrack.aspectRatio, contentMode: .fit)
                .shadow(radius: 20, y: 10)
                .transition(.scale.combined(with: .opacity))
            if let namespace {
                video
                    .matchedGeometryEffect(id: "screen", in: namespace)
            } else {
                video
            }
        }
    }
}
