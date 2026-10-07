import SwiftUI

/// Semantic type ramp for the app's primary surfaces.
///
/// Every role maps to a `Font.TextStyle`, so it scales with the user's Dynamic
/// Type setting. Prefer these over `.font(.system(size:))`; the remaining
/// fixed-size call sites are a known migration backlog (see the P0 UI pass).
enum MemoryFont {
    /// Screen / page title in pushed-detail chrome.
    static let screenTitle: Font = .system(.title3, weight: .semibold)
    /// Chat bubble and session-timeline turn body copy.
    static let conversationBody: Font = .body
    /// Card and row headers (tool names, question prompts, settings rows).
    static let cardHeader: Font = .system(.subheadline, weight: .semibold)
    /// Secondary descriptive copy under a header.
    static let bodySmall: Font = .subheadline
    /// Small label / chip (option rows, suggested-prompt chips).
    static let label: Font = .system(.footnote, weight: .medium)
    /// Emphasised small label (action buttons, outcome labels).
    static let labelEmphasis: Font = .system(.footnote, weight: .semibold)
    /// Secondary meta / caption text (timestamps, helper copy).
    static let meta: Font = .caption
    /// Emphasised meta (outcome labels, thinking header).
    static let metaEmphasis: Font = .system(.caption, weight: .semibold)
    /// Monospaced meta (room ids, argument dumps, thinking text).
    static let metaMono: Font = .system(.caption, design: .monospaced)
    /// Uppercased prominent-button label.
    static let buttonLabel: Font = .system(.subheadline, design: .monospaced, weight: .semibold)
}

/// The two speakers in a conversation, shared by the live chat (`ChatView`) and
/// the session timeline (`SessionDetailView.TurnBubble`) so both surfaces use
/// the same alignment, fill, and role label.
enum ConversationRole {
    case user
    case assistant

    var isUser: Bool {
        self == .user
    }

    /// Localization key for the role label. Reuses the session-detail keys so
    /// the label is already translated ("You" / "Agent").
    var labelKey: LocalizedStringKey {
        switch self {
        case .user: "sessions.detail.user"
        case .assistant: "sessions.detail.assistant"
        }
    }

    /// Bubble fill. The user tints with the accent; the assistant uses the
    /// raised card background so it reads clearly against the page background
    /// (`.bg1`) in both light and dark mode.
    var bubbleFill: Color {
        isUser ? .bgAccent : .bg2
    }

    /// Bubble boundary. The user bubble carries an accent border so its
    /// 15%-accent fill still reads as prominent against the black page in dark
    /// mode; the assistant bubble keeps a subtle separator hairline so its
    /// `bg2` fill is legible as a surface in both modes.
    var bubbleBorder: Color {
        isUser ? .separatorAccent : .separator1.opacity(0.5)
    }
}

/// Rounded bubble background shared by the chat bubbles and the activity
/// surfaces: the role fill plus its boundary, so every bubble reads as a
/// surface against `bg1` in both light and dark mode.
struct BubbleBackground: ViewModifier {
    let role: ConversationRole
    let cornerRadius: CGFloat

    func body(content: Content) -> some View {
        content.background(
            RoundedRectangle(cornerRadius: cornerRadius)
                .fill(role.bubbleFill)
                .overlay(
                    RoundedRectangle(cornerRadius: cornerRadius)
                        .stroke(role.bubbleBorder, lineWidth: 1)
                )
        )
    }
}

extension View {
    func bubbleBackground(_ role: ConversationRole, cornerRadius: CGFloat = .cornerRadiusLarge) -> some View {
        modifier(BubbleBackground(role: role, cornerRadius: cornerRadius))
    }
}

extension CGFloat {
    /// The grid spacing used as a design unit.
    static let grid: Self = 4

    #if os(visionOS)
        /// The corner radius for the platform-specific UI elements.
        static let cornerRadiusPerPlatform: Self = 11.5 * grid
    #else
        /// The corner radius for the platform-specific UI elements.
        static let cornerRadiusPerPlatform: Self = 2 * grid
    #endif

    /// The corner radius for the small UI elements.
    static let cornerRadiusSmall: Self = 2 * grid

    /// The corner radius for the large UI elements.
    static let cornerRadiusLarge: Self = 4 * grid
}

struct ProminentButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .textCase(.uppercase)
            .font(MemoryFont.buttonLabel)
            .foregroundStyle(.white)
            .background(.fgAccent.opacity(configuration.isPressed ? 0.75 : 1))
            .cornerRadius(.cornerRadiusSmall)
    }
}

struct RoundButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(.caption, weight: .semibold))
            .foregroundStyle(.white)
            .background(isEnabled ? .fgAccent.opacity(configuration.isPressed ? 0.75 : 1) : .fg4.opacity(0.4))
            .clipShape(Circle())
    }
}

struct ControlBarButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) var isEnabled

    var isToggled: Bool = false
    let foregroundColor: Color
    let backgroundColor: Color
    let borderColor: Color

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 17, weight: .medium))
            .foregroundStyle(isEnabled ? foregroundColor.opacity(configuration.isPressed ? 0.75 : 1) : borderColor)
            .background(
                RoundedRectangle(cornerRadius: .cornerRadiusPerPlatform)
                    .fill(isToggled ? backgroundColor : .clear)
            )
    }
}

struct BlurredTop: ViewModifier {
    func body(content: Content) -> some View {
        content.mask(
            LinearGradient(
                gradient: Gradient(colors: [.clear, .black, .black]),
                startPoint: .top,
                endPoint: .init(x: 0.5, y: 0.2)
            )
        )
    }
}

struct Shimmering: ViewModifier {
    @State private var isShimmering = false

    func body(content: Content) -> some View {
        content
            .mask(
                LinearGradient(
                    colors: [
                        .black.opacity(0.4),
                        .black,
                        .black,
                        .black.opacity(0.4),
                    ],
                    startPoint: isShimmering ? UnitPoint(x: 1, y: 0) : UnitPoint(x: -1, y: 0),
                    endPoint: isShimmering ? UnitPoint(x: 2, y: 0) : UnitPoint(x: 0, y: 0)
                )
                .animation(.linear(duration: 2).repeatForever(autoreverses: false), value: isShimmering)
            )
            .onAppear {
                isShimmering = true
            }
    }
}

extension View {
    func blurredTop() -> some View {
        modifier(BlurredTop())
    }

    func shimmering() -> some View {
        modifier(Shimmering())
    }
}
