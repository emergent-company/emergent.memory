import SwiftUI

/// One object's read-only card: the native counterpart of the web chat's
/// object preview drawer (`object_preview.templ`), with the same sections in
/// the same order — header, workflow, Properties, "<Type> Properties", Labels,
/// History.
///
/// It is the body both presentations share: ``ObjectCardSheet`` (opened from a
/// chat reference) and ``ObjectCardScreen`` (pushed from the object browser).
/// The workflow strip, editable in the web drawer, is read-only here, like the
/// rest of the card.
///
/// Every value is agent-written and untrusted. It is shown through plain
/// `Text(verbatim:)`, so nothing in it is interpreted as markup or a link.
struct ObjectCardView: View {
    let store: ObjectCardStore

    var body: some View {
        Group {
            switch store.loadState {
            case .idle, .loading:
                if let card = store.card {
                    content(card)
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            case .failed:
                unavailable
            case .loaded:
                if let card = store.card {
                    content(card)
                } else {
                    unavailable
                }
            }
        }
        .task { await store.load() }
    }

    // MARK: - States

    /// The drawer's "could not load" state, with its copy.
    private var unavailable: some View {
        ContentUnavailableView {
            Label("objects.card.unavailable", systemImage: "exclamationmark.circle")
        } description: {
            Text("objects.card.unavailable.description")
        } actions: {
            Button("objects.retry") {
                Task { await store.load() }
            }
            .buttonStyle(.bordered)
        }
    }

    // MARK: - Content

    private func content(_ card: ObjectCard) -> some View {
        List {
            Section {
                ObjectCardHeader(card: card)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 2 * .grid, leading: 0, bottom: 0, trailing: 0))
            }

            if let workflow = card.workflow {
                workflowSection(workflow)
            }

            ForEach(card.sections) { section in
                Section {
                    ForEach(section.fields) { field in
                        ObjectCardFieldRow(field: field)
                    }
                } header: {
                    Text(verbatim: section.title)
                }
                .listRowBackground(Color.bg2)
            }

            labelsSection(card.labels)

            Section("objects.card.history") {
                ObjectCardHistoryRows(store: store)
            }
            .listRowBackground(Color.bg2)
        }
        .listStyle(.insetGrouped)
        .scrollContentBackground(.hidden)
        .background(.bg1)
    }

    /// The workflow strip of a workflow-declaring type, read-only here.
    private func workflowSection(_ workflow: ObjectCardWorkflow) -> some View {
        Section("objects.card.workflow") {
            LabeledContent("objects.card.status") {
                if let status = workflow.status {
                    ObjectStatusBadge(status: status)
                } else {
                    emptyDash
                }
            }
            LabeledContent("objects.card.assignee") {
                if workflow.assignee.isEmpty {
                    emptyDash
                } else {
                    Text(verbatim: workflow.assignee)
                        .textSelection(.enabled)
                }
            }
        }
        .listRowBackground(Color.bg2)
    }

    /// The object's labels as one scrolling line of badges, or a dash.
    private func labelsSection(_ labels: [String]) -> some View {
        Section("objects.card.labels") {
            if labels.isEmpty {
                emptyDash
            } else {
                // One scrolling line of badges: labels are few, and a
                // line keeps the section compact in the medium detent.
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 1.5 * .grid) {
                        ForEach(labels, id: \.self) { label in
                            ObjectBadge(text: label)
                        }
                    }
                    .padding(.vertical, 1 * .grid)
                }
            }
        }
        .listRowBackground(Color.bg2)
    }

    private var emptyDash: some View {
        Text(verbatim: "—")
            .foregroundStyle(.fg3)
            .accessibilityLabel(Text("objects.card.empty"))
    }
}

// MARK: - Header

/// Icon tile, type chip and status badge, the title with its copy control, and
/// the "Referenced via …" line — the drawer's header block.
private struct ObjectCardHeader: View {
    let card: ObjectCard

    @State private var copied = false

    var body: some View {
        HStack(alignment: .top, spacing: 3 * .grid) {
            ObjectTypeIconTile(type: card.type)

            VStack(alignment: .leading, spacing: 1.5 * .grid) {
                HStack(spacing: 1.5 * .grid) {
                    ObjectBadge(
                        text: card.type.label,
                        tint: ObjectTypeAppearance.color(css: card.type.color) ?? .fgAccent
                    )
                    if let status = card.status {
                        ObjectStatusBadge(status: status)
                    }
                }

                HStack(alignment: .firstTextBaseline, spacing: 1 * .grid) {
                    Text(verbatim: card.title)
                        .font(.headline)
                        .foregroundStyle(.fg0)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                    copyButton
                }

                if let via = card.referencedVia {
                    Label {
                        Text("objects.card.referencedVia \(via)")
                    } icon: {
                        Image(systemName: "link")
                    }
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg3)
                }
            }
            Spacer(minLength: 0)
        }
    }

    /// Copies the canonical id, like the drawer's copy control. The glyph turns
    /// into a check for a moment, as the web copy buttons do.
    private var copyButton: some View {
        Button {
            #if canImport(UIKit)
                UIPasteboard.general.string = card.copyID
            #endif
            copied = true
            Task {
                try? await Task.sleep(for: .seconds(1.4))
                copied = false
            }
        } label: {
            Image(systemName: copied ? "checkmark" : "doc.on.doc")
                .font(.footnote)
                .foregroundStyle(copied ? .fgSuccess : .fg3)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        #if os(iOS)
            .sensoryFeedback(.success, trigger: copied) { _, now in now }
        #endif
            .accessibilityLabel(Text(copied ? LocalizedStringKey("objects.card.copied") : LocalizedStringKey("objects.card.copyID")))
    }
}

/// A status value as the web badge draws it: a dot and the label in its tone.
struct ObjectStatusBadge: View {
    let status: ObjectCardStatus

    var body: some View {
        ObjectBadge(
            text: status.label,
            tint: ObjectTypeAppearance.toneColor(status.tone),
            showsDot: true
        )
        .accessibilityLabel(Text("objects.card.status") + Text(verbatim: ": \(status.label)"))
    }
}

// MARK: - Field row

/// One property: its label (with the schema description behind an info
/// button), and its value, or a muted dash when it has none.
private struct ObjectCardFieldRow: View {
    let field: ObjectCardField

    @State private var showsDescription = false

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            HStack(spacing: 1 * .grid) {
                Text(verbatim: field.label)
                    .font(MemoryFont.metaEmphasis)
                    .foregroundStyle(.fg3)
                if !field.description.isEmpty {
                    Button {
                        showsDescription = true
                    } label: {
                        Image(systemName: "info.circle")
                            .font(MemoryFont.meta)
                            .foregroundStyle(.fg3)
                            .frame(minWidth: 28, minHeight: 28)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(Text("objects.card.fieldInfo \(field.label)"))
                    .popover(isPresented: $showsDescription) {
                        Text(verbatim: field.description)
                            .font(MemoryFont.bodySmall)
                            .padding(4 * .grid)
                            .frame(maxWidth: 320)
                            .presentationCompactAdaptation(.popover)
                    }
                }
            }

            if field.isEmpty {
                Text(verbatim: "—")
                    .font(.system(.subheadline, design: .monospaced))
                    .foregroundStyle(.fg4)
                    .accessibilityLabel(Text("objects.card.empty"))
            } else {
                Text(verbatim: field.value)
                    .font(.system(.subheadline, design: field.isMonospaced ? .monospaced : .default))
                    .foregroundStyle(.fg0)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.vertical, 0.5 * .grid)
        .accessibilityElement(children: .combine)
        .accessibilityHint(field.description.isEmpty ? Text(verbatim: "") : Text(verbatim: field.description))
    }
}
