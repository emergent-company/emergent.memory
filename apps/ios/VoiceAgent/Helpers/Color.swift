import SwiftUI
#if canImport(UIKit)
    import UIKit
#endif

// Adaptive design-token palette.
//
// Defined as `ShapeStyle where Self == Color` (the same pattern SwiftUI uses
// for `.red`, `.primary`, …) so the tokens resolve both as `Color.fg1` and in
// `ShapeStyle` contexts like `.foregroundStyle(.fg1)` / `.background(.bg2)`.
//
// The foreground ramp (`fg0`…`fg4`) is a real five-step hierarchy and the
// background ramp (`bg1`…`bg3`) a real three-step surface scale, each backed by
// a `UIColor` dynamic provider so light and dark mode are both correct.
#if canImport(UIKit)
    extension ShapeStyle where Self == Color {
        // MARK: - Ramp helpers

        /// A light/dark adaptive color backing the ramps.
        private static func adaptive(light: UIColor, dark: UIColor) -> Color {
            Color(uiColor: UIColor { traits in
                traits.userInterfaceStyle == .dark ? dark : light
            })
        }

        /// One step of the five-step foreground ramp: a black-on-white step in
        /// light mode and a white-on-black step in dark mode, matched so the
        /// hierarchy reads the same in both.
        private static func labelRamp(_ lightAlpha: CGFloat, _ darkAlpha: CGFloat) -> Color {
            adaptive(
                light: UIColor.black.withAlphaComponent(lightAlpha),
                dark: UIColor.white.withAlphaComponent(darkAlpha)
            )
        }

        // MARK: Text / foreground

        /// Primary — titles and the strongest text.
        static var fg0: Color {
            labelRamp(1.00, 1.00)
        }

        /// Body copy — primary, one step softer than `fg0`.
        static var fg1: Color {
            labelRamp(0.86, 0.90)
        }

        /// Secondary labels.
        static var fg2: Color {
            labelRamp(0.70, 0.72)
        }

        /// Tertiary / meta captions (timestamps, helper copy).
        static var fg3: Color {
            labelRamp(0.54, 0.56)
        }

        /// Quaternary — disabled / faintest.
        static var fg4: Color {
            labelRamp(0.40, 0.42)
        }

        /// Disabled control foreground. Kept distinct from `fg4` (which is too faint
        /// to read a disabled icon reliably) so disabled states stay legible.
        static var fgDisabled: Color {
            labelRamp(0.45, 0.50)
        }

        static var fgAccent: Color {
            Color.accentColor
        }

        static var fgModerate: Color {
            Color.orange
        }

        static var fgSerious: Color {
            Color.red
        }

        static var fgSuccess: Color {
            Color.green
        }

        // MARK: Backgrounds

        /// Page background.
        static var bg1: Color {
            adaptive(light: .white, dark: .black)
        }

        /// Raised surface — cards, bubbles, fields.
        static var bg2: Color {
            adaptive(
                light: UIColor(red: 0.949, green: 0.949, blue: 0.969, alpha: 1), // #F2F2F7
                dark: UIColor(red: 0.110, green: 0.110, blue: 0.118, alpha: 1) // #1C1C1E
            )
        }

        /// Inset / tertiary surface — one step past `bg2` in each mode.
        static var bg3: Color {
            adaptive(
                light: UIColor(red: 0.898, green: 0.898, blue: 0.918, alpha: 1), // #E5E5EA
                dark: UIColor(red: 0.173, green: 0.173, blue: 0.180, alpha: 1) // #2C2C2E
            )
        }

        /// Code surface (fenced blocks and inline spans). A step darker than `bg2`
        /// in light mode and a step lighter in dark mode, so code reads as a surface
        /// on both the page and the bubble backgrounds.
        static var codeBackground: Color {
            adaptive(
                light: UIColor(red: 0.851, green: 0.851, blue: 0.878, alpha: 1), // #D9D9E0
                dark: UIColor(red: 0.227, green: 0.227, blue: 0.235, alpha: 1) // #3A3A3C
            )
        }

        /// Disabled control fill (e.g. the send button with an empty composer).
        static var bgDisabled: Color {
            adaptive(
                light: UIColor(red: 0.820, green: 0.820, blue: 0.839, alpha: 1), // #D1D1D6
                dark: UIColor(red: 0.227, green: 0.227, blue: 0.235, alpha: 1) // #3A3A3C
            )
        }

        static var bgAccent: Color {
            Color.accentColor.opacity(0.15)
        }

        static var bgModerate: Color {
            Color.orange.opacity(0.15)
        }

        static var bgSerious: Color {
            Color.red.opacity(0.15)
        }

        static var bgSuccess: Color {
            Color.green.opacity(0.15)
        }

        // MARK: Separators / borders

        static var separator1: Color {
            Color(uiColor: .separator)
        }

        static var separator2: Color {
            Color(uiColor: .separator).opacity(0.5)
        }

        static var separatorAccent: Color {
            Color.accentColor.opacity(0.4)
        }

        static var separatorModerate: Color {
            Color.orange.opacity(0.4)
        }

        static var separatorSerious: Color {
            Color.red.opacity(0.4)
        }

        static var separatorSuccess: Color {
            Color.green.opacity(0.4)
        }
    }
#endif
