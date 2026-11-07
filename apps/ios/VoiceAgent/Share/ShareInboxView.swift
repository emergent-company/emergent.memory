import SwiftUI

/// The sheet listing captures that have not reached Memory yet.
///
/// A share the extension could not deliver itself (a file, or anything shared
/// while offline) waits here, so "I shared it but nothing happened" always has
/// a visible answer: the item, what state it is in, and a way to retry or drop
/// it.
struct ShareInboxView: View {
    @EnvironmentObject private var inbox: ShareInbox
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if inbox.captures.isEmpty {
                    empty
                } else {
                    list
                }
            }
            .navigationTitle("share.inbox.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("share.inbox.done") { dismiss() }
                }
            }
            .task { await inbox.drain() }
            .refreshable { await inbox.drain() }
        }
    }

    private var list: some View {
        List {
            Section {
                ForEach(inbox.captures) { capture in
                    row(capture)
                        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                            Button(role: .destructive) {
                                inbox.discard(capture.id)
                            } label: {
                                Label("share.inbox.remove", systemImage: "trash")
                            }
                        }
                        .swipeActions(edge: .leading) {
                            if capture.state == .failed {
                                Button {
                                    Task { await inbox.drain() }
                                } label: {
                                    Label("share.inbox.retry", systemImage: "arrow.clockwise")
                                }
                                .tint(.fgAccent)
                            }
                        }
                }
            } footer: {
                Text("share.inbox.footer")
            }
        }
        .listStyle(.insetGrouped)
        .overlay(alignment: .top) {
            if inbox.isDraining {
                ProgressView("share.inbox.uploading")
                    .padding(8)
            }
        }
    }

    private func row(_ capture: ShareCapture) -> some View {
        HStack(alignment: .top, spacing: 2 * .grid) {
            Image(systemName: capture.kind == .file ? "doc" : "link")
                .font(.title3)
                .foregroundStyle(.fg2)
                .frame(width: 24)
            VStack(alignment: .leading, spacing: 0.5 * .grid) {
                Text(capture.summary)
                    .font(MemoryFont.cardHeader)
                    .foregroundStyle(.fg0)
                    .lineLimit(2)
                if !capture.comment.isEmpty {
                    Text(capture.comment)
                        .font(MemoryFont.meta)
                        .foregroundStyle(.fg3)
                        .lineLimit(2)
                }
                status(capture)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 0.5 * .grid)
    }

    @ViewBuilder
    private func status(_ capture: ShareCapture) -> some View {
        switch capture.state {
        case .queued:
            HStack(spacing: 0.5 * .grid) {
                Text("share.inbox.queued")
                if !capture.sizeLabel.isEmpty {
                    Text("·")
                    Text(capture.sizeLabel)
                }
            }
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
        case .failed:
            VStack(alignment: .leading, spacing: 0.25 * .grid) {
                Text("share.inbox.failed")
                    .font(MemoryFont.metaEmphasis)
                    .foregroundStyle(.orange)
                if let error = capture.lastError {
                    Text(error)
                        .font(MemoryFont.meta)
                        .foregroundStyle(.fg3)
                        .lineLimit(2)
                }
            }
        }
    }

    private var empty: some View {
        EmptyStateView(
            title: "share.inbox.empty.title",
            description: "share.inbox.empty.description",
            systemImage: "tray",
            topPadding: 6 * .grid
        )
    }
}
