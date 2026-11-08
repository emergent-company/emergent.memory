import SwiftUI

/// How an object type and a status badge look on iOS, resolved from what the
/// gateway sends for the web UI: a Lucide icon name, a CSS color, a badge tone.
enum ObjectTypeAppearance {
    /// The SF Symbol drawn for a Lucide icon name from the type's schema.
    ///
    /// The keys are exactly the web's supported set (`supportedTypeIconClasses`,
    /// `apps/web-ui/gateway/type_icons.go`); the server already normalizes an
    /// unknown or undeclared icon to `box`, and anything unmapped here falls
    /// back to the same box.
    static func symbol(forLucide name: String) -> String {
        lucideSymbols[name] ?? fallbackSymbol
    }

    static let fallbackSymbol = "shippingbox"

    static let lucideSymbols: [String: String] = [
        "box": "shippingbox",
        "file-text": "doc.text",
        "file": "doc",
        "git-branch": "arrow.triangle.branch",
        "git-commit": "circle.circle",
        "git-merge": "arrow.triangle.merge",
        "git-pull-request": "arrow.triangle.pull",
        "shield": "shield",
        "shield-check": "checkmark.shield",
        "zap": "bolt",
        "layers": "square.3.layers.3d",
        "tag": "tag",
        "star": "star",
        "heart": "heart",
        "alert-triangle": "exclamationmark.triangle",
        "bell": "bell",
        "book": "book.closed",
        "book-open": "book",
        "bot": "cpu",
        "briefcase": "briefcase",
        "calendar": "calendar",
        "camera": "camera",
        "check": "checkmark",
        "check-circle": "checkmark.circle",
        "circle": "circle",
        "clock": "clock",
        "cloud": "cloud",
        "code": "chevron.left.forwardslash.chevron.right",
        "cog": "gearshape",
        "settings": "gearshape",
        "database": "cylinder.split.1x2",
        "edit": "square.and.pencil",
        "eye": "eye",
        "folder": "folder",
        "globe": "globe",
        "hash": "number",
        "home": "house",
        "image": "photo",
        "info": "info.circle",
        "key": "key",
        "link": "link",
        "list": "list.bullet",
        "lock": "lock",
        "mail": "envelope",
        "map": "map",
        "map-pin": "mappin.and.ellipse",
        "message-circle": "message",
        "monitor": "display",
        "package": "shippingbox",
        "paperclip": "paperclip",
        "pen": "pencil",
        "phone": "phone",
        "play": "play",
        "plus": "plus",
        "puzzle": "puzzlepiece",
        "search": "magnifyingglass",
        "send": "paperplane",
        "server": "server.rack",
        "share": "square.and.arrow.up",
        "sparkles": "sparkles",
        "terminal": "terminal",
        "trash": "trash",
        "user": "person",
        "users": "person.2",
        "wrench": "wrench.and.screwdriver",
        "x": "xmark",
    ]

    /// The type's declared CSS color as a SwiftUI color, or nil when it is
    /// undeclared or a shape the app does not read (the caller then uses the
    /// accent, as the web falls back to its primary tone).
    ///
    /// Reads `#rgb`, `#rrggbb`, `#rrggbbaa` and the basic CSS color names.
    static func color(css raw: String) -> Color? {
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !value.isEmpty else { return nil }
        if value.hasPrefix("#") {
            return hexColor(String(value.dropFirst()))
        }
        return namedColors[value]
    }

    private static func hexColor(_ hex: String) -> Color? {
        guard hex.allSatisfy(\.isHexDigit) else { return nil }
        let expanded: String = switch hex.count {
        case 3, 4: hex.map { "\($0)\($0)" }.joined()
        case 6, 8: hex
        default: ""
        }
        guard let number = UInt64(expanded, radix: 16) else { return nil }
        let hasAlpha = expanded.count == 8
        let red = Double((number >> (hasAlpha ? 24 : 16)) & 0xFF) / 255
        let green = Double((number >> (hasAlpha ? 16 : 8)) & 0xFF) / 255
        let blue = Double((number >> (hasAlpha ? 8 : 0)) & 0xFF) / 255
        let alpha = hasAlpha ? Double(number & 0xFF) / 255 : 1
        return Color(.sRGB, red: red, green: green, blue: blue, opacity: alpha)
    }

    private static let namedColors: [String: Color] = [
        "red": .red,
        "orange": .orange,
        "yellow": .yellow,
        "green": .green,
        "mint": .mint,
        "teal": .teal,
        "cyan": .cyan,
        "blue": .blue,
        "indigo": .indigo,
        "purple": .purple,
        "violet": .purple,
        "pink": .pink,
        "brown": .brown,
        "gray": .gray,
        "grey": .gray,
        "black": .black,
        "white": .white,
    ]

    /// The foreground of a status badge for the web's badge tone.
    static func toneColor(_ tone: String) -> Color {
        switch tone {
        case "success": .fgSuccess
        case "warning": .fgModerate
        case "error": .fgSerious
        case "info": .blue
        case "accent": .fgAccent
        default: .fg2
        }
    }
}

// MARK: - Shared pieces

/// The type's icon in a tinted rounded tile: the card header's leading mark,
/// as `typeIconTile` draws it on the web.
struct ObjectTypeIconTile: View {
    let type: ObjectCardType
    var size: CGFloat = 11 * .grid

    var body: some View {
        let tint = ObjectTypeAppearance.color(css: type.color) ?? .fgAccent
        ZStack {
            RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                .fill(tint.opacity(0.12))
            RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                .strokeBorder(tint.opacity(0.2), lineWidth: 1)
            if !type.glyph.isEmpty {
                Text(verbatim: type.glyph)
                    .font(.title3)
            } else {
                Image(systemName: ObjectTypeAppearance.symbol(forLucide: type.icon))
                    .font(.title3.weight(.medium))
                    .foregroundStyle(tint)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// A small capsule badge: the type chip, a status, or a label.
struct ObjectBadge: View {
    let text: String
    var tint: Color = .fg2
    /// Status badges lead with a dot, like the web's `StatusBadge`.
    var showsDot = false
    var systemImage: String?

    var body: some View {
        HStack(spacing: 1 * .grid) {
            if showsDot {
                Circle()
                    .fill(tint)
                    .frame(width: 6, height: 6)
                    .accessibilityHidden(true)
            }
            if let systemImage {
                Image(systemName: systemImage)
                    .imageScale(.small)
                    .accessibilityHidden(true)
            }
            Text(verbatim: text)
                .lineLimit(1)
        }
        .font(MemoryFont.metaEmphasis)
        .foregroundStyle(tint)
        .padding(.horizontal, 2 * .grid)
        .padding(.vertical, 0.5 * .grid)
        .background(tint.opacity(0.12), in: Capsule())
    }
}
