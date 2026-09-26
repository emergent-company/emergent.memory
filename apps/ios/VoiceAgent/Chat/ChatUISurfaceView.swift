import SwiftUI

// MARK: - Component registry

/// Native SwiftUI renderer for a validated A2UI surface: resolves catalog ids
/// to card views and falls back to a summary card for unknown components.
/// Renders only the pre-approved catalog — never raw markup or a web view.
struct A2UIComponentCard: View {
    let component: ChatUIComponent
    let submittedResponse: ChatUIPropValue?
    let onAction: (ChatUIAction) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            content
        }
        .padding(3 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }

    @ViewBuilder
    private var content: some View {
        switch component.component {
        case "proposal": proposalCard
        case "approval": approvalCard
        case "question": questionCard
        case "code": codeCard
        case "entity": entityCard
        case "object-form": objectFormCard
        case "todo": todoCard
        case "result": resultCard
        default: summaryCard
        }
    }

    // MARK: Headers

    private func header(_ title: String, badge: String? = nil) -> some View {
        HStack(spacing: 2 * .grid) {
            if let badge, !badge.isEmpty {
                Text(badge)
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.fg2)
                    .padding(.horizontal, 2 * .grid)
                    .padding(.vertical, 1)
                    .overlay(Capsule().stroke(.separator1, lineWidth: 1))
            }
            Text(title)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(.fg3)
                .textCase(.uppercase)
                .tracking(0.6)
            Spacer()
        }
    }

    private func label(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 14))
            .foregroundStyle(.fg1)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func pre(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 11, design: .monospaced))
            .foregroundStyle(.fg2)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(2 * .grid)
            .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }

    private func rows(_ pairs: [(String, String)]) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            ForEach(Array(pairs.enumerated()), id: \.offset) { _, pair in
                VStack(alignment: .leading, spacing: 1) {
                    Text(pair.0)
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(.fg3)
                        .textCase(.uppercase)
                        .tracking(0.5)
                    Text(pair.1)
                        .font(.system(size: 13))
                        .foregroundStyle(.fg1)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    // MARK: Actions

    @ViewBuilder
    private func actions(_ pairs: [(label: String, response: String, primary: Bool)]) -> some View {
        if let submittedResponse {
            HStack(spacing: 1 * .grid) {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 13))
                    .foregroundStyle(.fgSuccess)
                Text(submittedResponse.displayText)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.fgSuccess)
            }
        } else {
            HStack(spacing: 2 * .grid) {
                ForEach(Array(pairs.enumerated()), id: \.offset) { _, pair in
                    Button {
                        onAction(ChatUIAction(componentId: component.id, response: pair.response))
                    } label: {
                        Text(pair.label)
                    }
                    .buttonStyle(DecisionButtonStyle(tint: pair.primary ? .fgAccent : .fg3))
                }
            }
        }
    }

    // MARK: Cards

    private var proposalCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Proposal", badge: component.text("kind"))
            if let summary = component.text("summary"), !summary.isEmpty { label(summary) }
            if let body = component.text("body"), !body.isEmpty { pre(body) }
            actions([
                (label: "Reject", response: "reject", primary: false),
                (label: "Accept", response: "accept", primary: true),
            ])
        }
    }

    private var approvalCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Approval", badge: component.text("tool"))
            if let input = component.text("input"), !input.isEmpty { pre(input) }
            actions([
                (label: "Deny", response: "deny", primary: false),
                (label: "Approve", response: "approve", primary: true),
            ])
        }
    }

    private var questionCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Question")
            if let prompt = component.text("prompt"), !prompt.isEmpty { label(prompt) }
            if case let .array(options)? = component.value("options"), !options.isEmpty {
                if submittedResponse != nil {
                    actions([])
                } else {
                    VStack(spacing: 1 * .grid) {
                        ForEach(Array(options.enumerated()), id: \.offset) { _, option in
                            optionButton(option)
                        }
                    }
                }
            } else if submittedResponse != nil {
                actions([])
            }
        }
    }

    private func optionButton(_ option: ChatUIPropValue) -> some View {
        let values = optionValues(option)
        return Button {
            onAction(ChatUIAction(componentId: component.id, response: values.response))
        } label: {
            Text(values.label)
        }
        .buttonStyle(OptionRowButtonStyle())
    }

    private func optionValues(_ option: ChatUIPropValue) -> (label: String, response: ChatUIPropValue) {
        if case let .object(object) = option {
            let label = (object["label"] ?? object["value"])?.displayText ?? ""
            return (label, object["value"] ?? option)
        }
        return (option.displayText, option)
    }

    private var codeCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            if let lang = component.text("lang"), !lang.isEmpty {
                Text(lang)
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.fg3)
                    .textCase(.uppercase)
                    .tracking(0.5)
            }
            pre(component.text("code") ?? "")
        }
    }

    private var entityCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Entity", badge: component.text("type"))
            let properties = objectRows(component.value("properties"))
            if !properties.isEmpty {
                sectionLabel("Properties")
                rows(properties)
            }
            let relationships = relationshipRows(component.value("relationships"))
            if !relationships.isEmpty {
                sectionLabel("Relationships")
                rows(relationships)
            }
        }
    }

    private var objectFormCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Form")
            let fields = component.rows("fields")
            if fields.isEmpty {
                label("Structured input requested — form rendering is not implemented yet.")
            } else {
                rows(fields)
            }
        }
    }

    private var todoCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Tasks")
            let items = component.value("items") ?? component.value("todos")
            if case let .array(list)? = items, !list.isEmpty {
                VStack(alignment: .leading, spacing: 1 * .grid) {
                    ForEach(Array(list.enumerated()), id: \.offset) { _, item in
                        todoRow(item)
                    }
                }
            }
        }
    }

    private func todoRow(_ item: ChatUIPropValue) -> some View {
        let values = todoValues(item)
        return HStack(spacing: 2 * .grid) {
            Image(systemName: values.done ? "checkmark.square.fill" : "square")
                .font(.system(size: 13))
                .foregroundStyle(values.done ? .fgSuccess : .fg3)
            Text(values.text)
                .font(.system(size: 13))
                .foregroundStyle(.fg1)
                .strikethrough(values.done, color: .fg3)
            Spacer()
        }
    }

    private func todoValues(_ item: ChatUIPropValue) -> (done: Bool, text: String) {
        switch item {
        case let .object(object):
            var done = false
            if case let .bool(value)? = object["done"] { done = value }
            return (done, (object["label"] ?? object["text"])?.displayText ?? "")
        default:
            return (false, item.displayText)
        }
    }

    private var resultCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header("Result")
            let pairs = component.extraProps
            if pairs.isEmpty {
                rows([("result", "—")])
            } else {
                rows(pairs)
            }
        }
    }

    private var summaryCard: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header(component.component.isEmpty ? "component" : component.component)
            let pairs = component.extraProps
            if pairs.isEmpty {
                label("—")
            } else {
                pre(pairs.map { "\($0.0): \($0.1)" }.joined(separator: "\n"))
            }
        }
    }

    // MARK: Prop helpers

    private func sectionLabel(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 10, weight: .semibold))
            .foregroundStyle(.fg3)
            .textCase(.uppercase)
            .tracking(0.5)
    }

    /// `properties` as ordered key/value rows (object or array of pairs).
    private func objectRows(_ value: ChatUIPropValue?) -> [(String, String)] {
        switch value {
        case let .object(object):
            return object.sorted { $0.key < $1.key }.map { ($0.key, $0.value.displayText) }
        case let .array(items):
            return items.compactMap { item in
                guard case let .object(object) = item else { return nil }
                let key = (object["key"] ?? object["label"] ?? object["name"])?.displayText ?? ""
                let val = (object["value"] ?? object["text"])?.displayText ?? ""
                if key.isEmpty { return nil }
                return (key, val)
            }
        default:
            return []
        }
    }

    /// `relationships` as relation → target rows (strings or objects).
    private func relationshipRows(_ value: ChatUIPropValue?) -> [(String, String)] {
        guard case let .array(items)? = value else { return [] }
        return items.map { item in
            switch item {
            case let .string(text): return (text, "")
            case let .object(object):
                let relation = (object["relation"] ?? object["type"] ?? object["label"])?.displayText ?? "related"
                let target = (object["target"] ?? object["entity"] ?? object["id"])?.displayText ?? ""
                return (relation, target)
            default:
                return (item.displayText, "")
            }
        }
    }
}
