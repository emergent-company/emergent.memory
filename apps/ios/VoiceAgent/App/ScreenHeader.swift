import SwiftUI

/// Shared header for pushed detail screens: a back chevron plus a title and an
/// optional subtitle, at consistent paddings and type.
///
/// Adopted by the agent level, object browser, object detail, session detail,
/// chats list and chat room screens so every pushed screen reads the same. The
/// back button keeps the `agents.level.back` accessibility label the previous
/// headers used.
struct ScreenHeader: View {
    /// Title line. Pass `Text("some.key")` for localized copy or `Text(name)`
    /// for runtime data.
    let title: Text
    /// Optional subtitle line.
    var subtitle: Text?
    /// Subtitle font; defaults to the meta ramp (the session detail passes a
    /// monospaced font for the room id).
    var subtitleFont: Font = MemoryFont.meta
    /// Optional trailing control (mode picker, list actions). `AnyView`
    /// because the header is a concrete `View` and its callers pass different
    /// controls.
    var trailing: AnyView?
    let onBack: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(spacing: 2 * .grid) {
            Button(action: onBack) {
                Image(systemName: "chevron.left")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.fg3)
                    .padding(2 * .grid)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("agents.level.back")

            VStack(alignment: .leading, spacing: 1 * .grid) {
                title
                    .font(MemoryFont.screenTitle)
                    .foregroundStyle(.fg0)
                    // One line reads best at default sizes, but at accessibility
                    // sizes a truncated title is the only cue for which screen is
                    // showing (titles can carry runtime agent names) (#1948).
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
                if let subtitle {
                    subtitle
                        .font(subtitleFont)
                        .foregroundStyle(.fg3)
                        .lineLimit(1)
                }
            }
            Spacer()
            if let trailing {
                trailing
            }
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.top, 1 * .grid)
        .padding(.bottom, 2 * .grid)
    }
}
