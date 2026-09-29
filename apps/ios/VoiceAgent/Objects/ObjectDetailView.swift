import SwiftUI

/// Shows one object's content: its identity and metadata plus its properties.
///
/// Object properties are written by the agent and are untrusted. Every value is
/// rendered through ``JSONValue/display`` into a plain ``Text``; SwiftUI never
/// interprets markup, and no attributed HTML is built from the values.
struct ObjectDetailView: View {
    let object: GraphObject

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            header()
            ScrollView {
                VStack(alignment: .leading, spacing: 4 * .grid) {
                    metadata
                    properties
                }
                .padding(4 * .grid)
            }
        }
        .background(.bg1)
    }

    // MARK: - Chrome

    private func header() -> some View {
        HStack(spacing: 2 * .grid) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "chevron.left")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(.fg3)
                    .padding(2 * .grid)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("agents.level.back")

            VStack(alignment: .leading, spacing: 1 * .grid) {
                Text("objects.detail.title")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(.fg0)
                Text(object.displayTitle)
                    .font(.system(size: 11))
                    .foregroundStyle(.fg3)
                    .lineLimit(1)
            }
            Spacer()
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.top, 1 * .grid)
        .padding(.bottom, 2 * .grid)
    }

    // MARK: - Metadata

    private var metadata: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            if !object.type.isEmpty {
                metaRow("objects.detail.type", object.type)
            }
            if !object.key.isEmpty {
                metaRow("objects.detail.key", object.key)
            }
            if !object.status.isEmpty {
                metaRow("objects.detail.status", object.status)
            }
            if let labels = object.labels, !labels.isEmpty {
                metaRow("objects.detail.labels", labels.joined(separator: ", "))
            }
            if !object.id.isEmpty {
                metaRow("objects.detail.id", object.id, monospaced: true)
            }
            if let createdAt = object.createdAt, !createdAt.isEmpty {
                metaRow("objects.detail.created", createdAt)
            }
        }
    }

    private func metaRow(_ label: LocalizedStringKey, _ value: String, monospaced: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(label)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(.fg3)
            Text(value)
                .font(.system(size: 14, design: monospaced ? .monospaced : .default))
                .foregroundStyle(.fg0)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: - Properties

    private var properties: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Text("objects.detail.properties")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(.fg0)

            if let properties = object.properties, !properties.isEmpty {
                ForEach(properties.sorted(by: { $0.key < $1.key }), id: \.key) { key, value in
                    propertyRow(key, value)
                }
            } else {
                Text("objects.detail.noProperties")
                    .font(.system(size: 13))
                    .foregroundStyle(.fg3)
            }
        }
    }

    private func propertyRow(_ key: String, _ value: JSONValue) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(key)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(.fg3)
            Text(value.display)
                .font(.system(size: 14, design: .monospaced))
                .foregroundStyle(.fg1)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .multilineTextAlignment(.leading)
        }
        .padding(3 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}

#Preview {
    NavigationStack {
        ObjectDetailView(object: GraphObject(
            id: "00000000-0000-0000-0000-000000000001",
            type: "person",
            key: "sam",
            status: "active",
            properties: [
                "name": .string("Sam"),
                "age": .number(42),
                "tags": .array([.string("colleague")]),
            ],
            labels: ["person"],
            createdAt: "2026-09-01T10:00:00Z"
        ))
    }
}
