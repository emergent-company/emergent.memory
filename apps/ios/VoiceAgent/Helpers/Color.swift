import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

/// System-color mappings replacing the custom asset-catalog palette.
///
/// Defined as `ShapeStyle where Self == Color` (the same pattern SwiftUI uses
/// for `.red`, `.primary`, …) so the tokens resolve both as `Color.fg1` and in
/// `ShapeStyle` contexts like `.foregroundStyle(.fg1)` / `.background(.bg2)`.
/// Each token now maps to a platform-default (light/dark adaptive) color.
extension ShapeStyle where Self == Color {
    // MARK: Text / foreground

    static var fg0: Color { Color.primary }
    static var fg1: Color { Color.primary }
    static var fg2: Color { Color.secondary }
    static var fg3: Color { Color.secondary }
    static var fg4: Color { Color.secondary }
    static var fgAccent: Color { Color.accentColor }
    static var fgModerate: Color { Color.secondary }
    static var fgSerious: Color { Color.red }
    static var fgSuccess: Color { Color.green }

    // MARK: Backgrounds

    static var bg1: Color { Color(uiColor: .systemBackground) }
    static var bg2: Color { Color(uiColor: .secondarySystemBackground) }
    static var bg3: Color { Color(uiColor: .tertiarySystemBackground) }
    static var bgAccent: Color { Color.accentColor.opacity(0.15) }
    static var bgModerate: Color { Color(uiColor: .secondarySystemBackground) }
    static var bgSerious: Color { Color.red.opacity(0.15) }
    static var bgSuccess: Color { Color.green.opacity(0.15) }

    // MARK: Separators / borders

    static var separator1: Color { Color(uiColor: .separator) }
    static var separator2: Color { Color(uiColor: .separator).opacity(0.5) }
    static var separatorAccent: Color { Color.accentColor.opacity(0.4) }
    static var separatorModerate: Color { Color.secondary.opacity(0.3) }
    static var separatorSerious: Color { Color.red.opacity(0.4) }
    static var separatorSuccess: Color { Color.green.opacity(0.4) }
}
