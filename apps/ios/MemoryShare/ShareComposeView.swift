import SwiftUI

/// The share extension's sheet: what was shared, one comment field, Remember.
///
/// Deliberately one screen and no configuration: a share sheet is a detour on
/// the way somewhere else, so the only decisions are what to say about the item
/// and whether to keep it.
struct ShareComposeView: View {
    @ObservedObject var model: ShareComposeModel
    let onCancel: () -> Void
    let onFinished: () -> Void

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Remember")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel", action: onCancel)
                            .disabled(model.isSaving)
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        saveButton
                    }
                }
        }
        .interactiveDismissDisabled(model.isSaving)
        .onChange(of: model.outcome) { _, outcome in
            // Give the confirmation a beat to be read, then hand the user back
            // to where they came from: nothing in a share extension should make
            // them hunt for a close button after it worked.
            guard case .saved = outcome else { return }
            Task {
                try? await Task.sleep(for: .seconds(1.1))
                onFinished()
            }
        }
    }

    @ViewBuilder
    private var saveButton: some View {
        if model.isSaving {
            ProgressView()
        } else {
            Button("Remember") {
                Task { await model.save() }
            }
            .fontWeight(.semibold)
            .disabled(!model.canSave)
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.outcome {
        case let .saved(queued):
            savedView(queued: queued)
        case let .failed(message):
            failureView(message)
        case .composing, .saving:
            composeView
        }
    }

    // MARK: - Compose

    private var composeView: some View {
        VStack(alignment: .leading, spacing: 16) {
            previewCard
            commentField
            Spacer(minLength: 0)
            Text(model.destination)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding(20)
        .background(Color(.systemGroupedBackground))
    }

    private var previewCard: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: model.payload.symbolName)
                .font(.title3)
                .foregroundStyle(.tint)
                .frame(width: 28)
            VStack(alignment: .leading, spacing: 3) {
                Text(model.payload.previewTitle)
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(3)
                let detail = model.payload.previewDetail
                if !detail.isEmpty, detail != model.payload.previewTitle {
                    Text(detail)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(14)
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 12))
    }

    private var commentField: some View {
        ZStack(alignment: .topLeading) {
            if model.comment.isEmpty {
                Text("Add a note (optional)")
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 5)
                    .padding(.vertical, 8)
                    .allowsHitTesting(false)
            }
            TextEditor(text: $model.comment)
                .scrollContentBackground(.hidden)
                .frame(minHeight: 110)
                .disabled(model.isSaving)
        }
        .padding(8)
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 12))
    }

    // MARK: - Outcomes

    private func savedView(queued: Bool) -> some View {
        VStack(spacing: 12) {
            Image(systemName: queued ? "tray.and.arrow.down.fill" : "checkmark.circle.fill")
                .font(.largeTitle)
                .foregroundStyle(.tint)
            Text(queued ? "Saved to Memory" : "Remembered")
                .font(.headline)
            if queued {
                Text("It uploads next time you open the Memory app.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(24)
        .background(Color(.systemGroupedBackground))
    }

    private func failureView(_ message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.largeTitle)
                .foregroundStyle(.orange)
            Text("Not saved")
                .font(.headline)
            Text(message)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Try Again") {
                model.outcome = .composing
            }
            .buttonStyle(.borderedProminent)
            .padding(.top, 4)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(24)
        .background(Color(.systemGroupedBackground))
    }
}
