import Foundation

// MARK: - Reload

extension ChatTimelineStore {
    /// Applies a fetched history: replaces the rows of every run the record now
    /// holds and keeps the live rows that are still newer, so a reload never
    /// duplicates the last turn.
    ///
    /// A live text row is kept only when the record does not already contain
    /// it: the optimistic user message is matched by text, an assistant
    /// partial by being a prefix of a recorded reply. A live reasoning segment
    /// that was still open stays open if the record now holds the same text.
    /// A tool group's own disclosure choice survives a reload because its row
    /// identity is the first call's id (``ChatTimelineRows``), not a position.
    func replaceHistory(_ history: [TimelineItem]) {
        let recordedRunIDs = Set(history.compactMap(\.runID))
        let recordedUserTexts = Set(history.compactMap { item -> String? in
            guard case let .userMessage(text) = item.kind else { return nil }
            return text
        })
        let recordedReplies = history.compactMap { item -> String? in
            guard case let .assistantText(markdown, _) = item.kind else { return nil }
            return markdown
        }

        let kept = items.filter { item in
            if let runID = item.runID, recordedRunIDs.contains(runID) {
                return false
            }
            switch item.kind {
            case let .userMessage(text):
                return !recordedUserTexts.contains(text)
            case let .assistantText(markdown, _):
                guard !markdown.isEmpty else { return false }
                return !recordedReplies.contains { $0.hasPrefix(markdown) }
            default:
                return true
            }
        }

        var merged = history
        markOpenReasoning(merged: &merged)
        replaceRows(merged + kept, historyIDs: Set(history.map(\.id)))
    }

    /// Keeps a reasoning row open when the live copy of the same segment was
    /// still streaming: the reload is the same segment, not a new one. Rows are
    /// matched by text (the live id and the recorded message id are different
    /// namespaces), and only while the live text is a prefix of the record's.
    private func markOpenReasoning(merged: inout [TimelineItem]) {
        for index in merged.indices {
            guard case let .reasoning(text, isLive, isOpen) = merged[index].kind, !isOpen else { continue }
            let wasOpen = items.contains { item in
                guard case let .reasoning(previous, _, open) = item.kind else { return false }
                return open && !previous.isEmpty && (text == previous || text.hasPrefix(previous))
            }
            if wasOpen {
                merged[index].kind = .reasoning(text: text, isLive: isLive, isOpen: true)
            }
        }
    }
}

// MARK: - History conversion

/// One conversion for the history endpoint's items, so the order in the record
/// is the order on screen: a `message` that carries reasoning emits the
/// reasoning row **then** the answer, consecutive reasoning in one run merges,
/// and a tool call between them starts a new segment.
enum ChatTimelineHistory {
    static func items(from history: ConversationHistory) -> [TimelineItem] {
        var rows: [TimelineItem] = []

        for (index, item) in history.items.enumerated() {
            let context = Context(item: item, index: index)
            switch item.kind {
            case "message":
                appendMessage(context, to: &rows)
            case "tool_call":
                appendTool(context, to: &rows)
            case "run_start":
                appendRunStart(context, to: &rows)
            case "run_end":
                appendRunEnd(context, to: &rows)
            default:
                continue
            }
        }
        appendApprovals(history.pendingApprovals, to: &rows)
        return rows
    }

    /// One item's resolved identity and timing: the server id when it sends one
    /// (tool calls and runs only), otherwise the created-at stamp plus the
    /// item's position in the response.
    ///
    /// Message rows carry **no** id (`id` is populated for tool calls only) and
    /// `step_number` is always present (`0` for the stored, run-less rows), so a
    /// step-based fallback would give every message the same id — a duplicate
    /// `Identifiable` in `ForEach`, and one collapsed `historyIDs` entry.
    private struct Context {
        let item: HistoryItem
        let id: String
        let runID: String?
        let createdAt: Date?

        init(item: HistoryItem, index: Int) {
            self.item = item
            let runID = item.runID?.isEmpty == false ? item.runID : nil
            self.runID = runID
            createdAt = TimelineDate.parse(item.createdAt)
            let stamp = item.createdAt ?? "row-\(index)"
            let fallback = "\(runID ?? "conversation"):\(stamp):\(index):\(item.kind)"
            id = item.id.flatMap { $0.isEmpty ? nil : $0 } ?? fallback
        }
    }

    private static func appendMessage(_ context: Context, to rows: inout [TimelineItem]) {
        guard let content = context.item.content else { return }
        if let reasoning = content.reasoning, !reasoning.isEmpty {
            appendReasoning(
                reasoning,
                runID: context.runID,
                createdAt: context.createdAt,
                id: "\(context.id):reasoning",
                to: &rows
            )
        }
        let text = content.text ?? ""
        guard !text.isEmpty else { return }
        switch roleKind(context.item.role) {
        case .user:
            rows.append(TimelineItem(
                id: context.id,
                runID: context.runID,
                createdAt: context.createdAt,
                kind: .userMessage(text: text)
            ))
        case .reply:
            rows.append(TimelineItem(
                id: context.id,
                runID: context.runID,
                createdAt: context.createdAt,
                kind: .assistantText(
                    markdown: text,
                    citations: ChatCitation.deduplicated(context.item.citations ?? [])
                )
            ))
        case .ignored:
            // Tool/system rows carry no chat text of their own.
            return
        }
    }

    /// How a recorded `role` classifies, ported from the server's
    /// `IsAgentReplyRole` (`apps/server/domain/agents/agent_run_once.go`).
    ///
    /// An agent turn's role is the **ADK author** — the sanitised agent name
    /// (`coder`, `Norwegian_Law_Assistant`) — not the literal `assistant`, which
    /// only the `kb.chat_messages` rows use. Only the empty role, `user`, `tool`,
    /// `tool_result` and `system` are not a reply; every other author is one.
    /// Do not "tidy" this into `role == "assistant"`: that drops every real
    /// agent reply, which is why the server has a runrole-guard gate for it
    /// (`emergent.memory#1832`).
    private enum RoleKind {
        case user
        case reply
        case ignored
    }

    private static func roleKind(_ role: String?) -> RoleKind {
        switch role ?? "" {
        case "user":
            .user
        case "", "tool", "tool_result", "system":
            .ignored
        default:
            .reply
        }
    }

    private static func appendTool(_ context: Context, to rows: inout [TimelineItem]) {
        guard let toolName = context.item.toolName, !toolName.isEmpty else { return }
        if toolName == "ask_user",
           let question = context.item.answeredQuestion(
               id: context.id,
               runID: context.runID,
               createdAt: context.createdAt
           )
        {
            rows.append(question)
            return
        }
        let status = context.item.toolStatus ?? ""
        let running = status.isEmpty || status == "running" || status == "started"
        let info = ToolCallInfo(
            id: context.id,
            name: toolName,
            arguments: context.item.toolInput?.jsonString ?? "",
            result: context.item.toolOutput?.jsonString,
            isError: !status.isEmpty && !running && status != "completed",
            isRunning: running,
            duration: context.item.durationMs.map { TimeInterval($0) / 1000 },
            responseSize: context.item.toolOutput.flatMap { ToolResultSize.byteCount(of: $0.jsonString) },
            showsID: true,
            category: context.item.toolCategory,
            subject: context.item.toolSubject,
            presentation: context.item.toolView?.presentation
        )
        rows.append(TimelineItem(
            id: context.id,
            runID: context.runID,
            createdAt: context.createdAt,
            kind: .tool(info)
        ))
    }

    private static func appendRunStart(_ context: Context, to rows: inout [TimelineItem]) {
        rows.append(TimelineItem(
            id: "run-start-\(context.runID ?? context.id)",
            runID: context.runID,
            createdAt: context.createdAt,
            kind: .runBoundary(RunWindow(
                id: context.runID ?? context.id,
                model: context.item.runModel,
                status: context.item.runStatus ?? "running",
                startedAt: context.createdAt
            ))
        ))
    }

    private static func appendRunEnd(_ context: Context, to rows: inout [TimelineItem]) {
        // Read the pair before appending: the end window borrows `rows` while
        // `append` needs exclusive access to it.
        let model = context.item.runModel ?? recordedModel(for: context.runID, in: rows)
        rows.append(TimelineItem(
            id: "run-end-\(context.runID ?? context.id)",
            runID: context.runID,
            createdAt: context.createdAt,
            kind: .runBoundary(RunWindow(
                id: context.runID ?? context.id,
                // `run_start` is where the record puts the model; `run_end`
                // usually carries none, so fall back to the run's start window.
                model: model,
                status: context.item.runStatus,
                startedAt: context.createdAt,
                completedAt: TimelineDate.parse(context.item.completedAt),
                error: context.item.errorMessage
            ))
        ))
    }

    /// The model a run announced on its `run_start` item, for the `run_end`
    /// window that shares its id.
    private static func recordedModel(for runID: String?, in rows: [TimelineItem]) -> String? {
        guard let runID else { return nil }
        for row in rows.reversed() {
            guard case let .runBoundary(window) = row.kind else { continue }
            if window.id == runID, let model = window.model {
                return model
            }
        }
        return nil
    }

    private static func appendApprovals(
        _ pending: [HistoryPendingApproval],
        to rows: inout [TimelineItem]
    ) {
        for approval in pending where !approval.questionID.isEmpty {
            rows.append(TimelineItem(
                id: "approval-\(approval.questionID)",
                runID: nil,
                createdAt: nil,
                kind: .approval(ApprovalRequest(
                    questionId: approval.questionID,
                    tool: approval.tool,
                    arguments: approval.input?.jsonString ?? ""
                ))
            ))
        }
    }

    /// Appends a reasoning segment, merging into the previous one of the same
    /// run; a tool call (or any other row) between them starts a new segment.
    /// A recorded item with no run id joins whatever came before it, which is
    /// the web's rule (`sameRun = !item.run_id || …`) for stored rows.
    private static func appendReasoning(
        _ text: String,
        runID: String?,
        createdAt: Date?,
        id: String,
        to rows: inout [TimelineItem]
    ) {
        if let last = rows.last, runID == nil || last.runID == runID,
           case let .reasoning(existing, live, open) = last.kind
        {
            rows[rows.count - 1].kind = .reasoning(
                text: existing + "\n\n" + text,
                isLive: live,
                isOpen: open
            )
            return
        }
        rows.append(TimelineItem(
            id: id,
            runID: runID,
            createdAt: createdAt,
            kind: .reasoning(text: text, isLive: false, isOpen: false)
        ))
    }
}

// MARK: - Recorded question cards

extension HistoryItem {
    /// An answered `ask_user` call as a question card, or nil for a plain tool
    /// call. The gateway injects the user's answer at `tool_output.response`.
    func answeredQuestion(id: String, runID: String?, createdAt: Date?) -> TimelineItem? {
        guard let input = toolInput?.objectValue,
              let answer = toolOutput?.objectValue?["response"]?.stringValue,
              !answer.isEmpty,
              let question = input["question"]?.stringValue,
              !question.isEmpty
        else { return nil }
        let options = input["options"]?.arrayValue ?? []
        return TimelineItem(
            id: id,
            runID: runID,
            createdAt: createdAt,
            kind: .question(QuestionRequest(
                questionId: id,
                question: question,
                interactionType: QuestionInteractionType(wire: input["interaction_type"]?.stringValue),
                options: options.compactMap { option in
                    guard let object = option.objectValue,
                          let label = object["label"]?.stringValue,
                          let value = object["value"]?.stringValue
                    else { return nil }
                    return ChatQuestionOption(
                        label: label,
                        value: value,
                        description: object["description"]?.stringValue
                    )
                },
                placeholder: input["placeholder"]?.stringValue,
                maxLength: input["max_length"]?.numberValue.map { Int($0) },
                submittedAnswer: answer
            ))
        )
    }
}

extension QuestionInteractionType {
    /// The recorded input's spelling of an interaction kind; unknown values
    /// fall back to buttons, as the live decoder does.
    init(wire: String?) {
        switch wire {
        case "multi_select": self = .multiSelect
        case "text", "free_text": self = .freeText
        default: self = .buttons
        }
    }
}
