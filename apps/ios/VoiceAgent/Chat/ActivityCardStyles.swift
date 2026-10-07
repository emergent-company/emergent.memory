import SwiftUI

/// Compact tinted button used on the approval card.
struct DecisionButtonStyle: ButtonStyle {
    let tint: Color

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(MemoryFont.labelEmphasis)
            .foregroundStyle(.white)
            .padding(.horizontal, 3 * .grid)
            .padding(.vertical, 1 * .grid)
            .background(
                Capsule().fill(tint.opacity(configuration.isPressed ? 0.75 : 1))
            )
            // Keep the capsule's visual size while giving it a 44pt tap target.
            .frame(minHeight: 44)
            .contentShape(Rectangle())
    }
}

/// Full-width selectable option row.
struct OptionRowButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(MemoryFont.label)
            .foregroundStyle(configuration.isPressed ? .fg0 : .fg1)
            .padding(2 * .grid)
            .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
            // Keep the row's visual size while giving it a 44pt tap target.
            .frame(minHeight: 44)
            .contentShape(Rectangle())
    }
}
