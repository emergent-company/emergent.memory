import SwiftUI

// MARK: - Presentations

/// The card as a sheet, opened from a chat reference. Medium and large
/// detents, as a native preview: the reader glances at it over the chat, or
/// drags it up to read everything.
struct ObjectCardSheet: View {
    @State private var store: ObjectCardStore
    @Environment(\.dismiss) private var dismiss

    init(reference: ObjectReference) {
        _store = State(initialValue: ObjectCardStore(reference: reference))
    }

    var body: some View {
        NavigationStack {
            ObjectCardView(store: store)
                .navigationTitle(Text("objects.detail.title"))
            #if os(iOS)
                .navigationBarTitleDisplayMode(.inline)
            #endif
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("objects.card.done") { dismiss() }
                    }
                }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .presentationBackground(.bg1)
    }
}

/// The card pushed from the object browser, in the app's own screen chrome
/// (its header chevron is the only back affordance).
struct ObjectCardScreen: View {
    @State private var store: ObjectCardStore
    @Environment(\.dismiss) private var dismiss

    init(reference: ObjectReference) {
        _store = State(initialValue: ObjectCardStore(reference: reference))
    }

    var body: some View {
        VStack(spacing: 0) {
            ScreenHeader(
                title: Text("objects.detail.title"),
                subtitle: store.card.map { Text(verbatim: $0.title) }
            ) {
                dismiss()
            }
            ObjectCardView(store: store)
        }
        .background(.bg1)
        // The object browser reveals the system bar for its search field, so
        // this pushed screen hides it explicitly (one back affordance).
        #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
        #endif
    }
}

// MARK: - Chat integration

extension View {
    /// Opens object references in chat content as an object card sheet.
    ///
    /// An agent's inline link (`[Name](/objects/<id>)`) and a Sources row both
    /// reach the view as an `openURL` of an `/objects/…` link; this handler
    /// claims those and leaves every other link to the system. Scoped to chat
    /// transcripts, as the web drawer only intercepts chat content.
    func presentsObjectReferences() -> some View {
        modifier(ObjectReferencePresenter())
    }
}

private struct ObjectReferencePresenter: ViewModifier {
    @State private var presented: ObjectReference?

    func body(content: Content) -> some View {
        content
            .environment(\.openURL, OpenURLAction { url in
                guard let reference = ObjectReference(url: url) else { return .systemAction }
                Log.nav.info("open object card \(reference.ref)")
                presented = reference
                return .handled
            })
            .sheet(item: $presented) { reference in
                ObjectCardSheet(reference: reference)
            }
    }
}

#Preview("Card sheet") {
    Color.bg1
        .sheet(isPresented: .constant(true)) {
            ObjectCardSheet(reference: ObjectReference(ref: "00000000-0000-0000-0000-000000000001"))
        }
}
