import Foundation

// MARK: - Gateway stream

extension ChatTimelineStore {
    /// Appends one live stream frame to the same list history lands in.
    func apply(_ event: ChatStreamEvent) {
        switch event {
        case let .meta(_, runID):
            beginLiveRun(runID)
        case let .text(delta):
            appendToken(delta)
        case let .activity(activity):
            apply(activity)
        case let .citations(citations):
            setCitations(citations)
        case let .failure(message):
            closeReasoning()
            closeLiveRun(status: "failed", error: message)
        case .done:
            closeReasoning()
            closeLiveRun(status: "completed", error: nil)
        case let .replay(events):
            for event in events {
                apply(event)
            }
        case .refresh:
            // Run facts belong to the status/subagent surfaces, not a row.
            break
        }
    }

    /// Appends a delta to the newest text row when it is the answer being
    /// streamed, otherwise opens a new one — this is what keeps text between
    /// tools in place instead of piling every token at the end of the turn.
    func appendToken(_ delta: String) {
        guard !delta.isEmpty else { return }
        if let last = lastItem, case let .assistantText(markdown, citations) = last.kind {
            replaceLastKind(.assistantText(markdown: markdown + delta, citations: citations))
            return
        }
        closeReasoning()
        appendRow(TimelineItem(
            id: nextLiveAssistantID(),
            runID: liveRun,
            createdAt: now(),
            kind: .assistantText(markdown: delta, citations: [])
        ))
    }

    /// The turn's grounded references join its newest answer's Sources footer.
    func setCitations(_ citations: [ChatCitation]) {
        guard let index = items.lastIndex(where: { item in
            if case .assistantText = item.kind {
                return true
            }
            return false
        }), case let .assistantText(markdown, existing) = items[index].kind
        else { return }
        replaceKind(
            at: index,
            with: .assistantText(
                markdown: markdown,
                citations: ChatCitation.deduplicated(existing + citations)
            )
        )
    }

    /// Closes the live run's window when the turn ends, so its footer can draw
    /// without waiting for the recorded history reload. The stream forwards no
    /// model, so the line gains one only after the record lands; the end time
    /// and duration are the client's own, which is all a live run can know.
    private func closeLiveRun(status: String, error: String?) {
        guard let end = claimLiveRunEnd() else { return }
        let id = "live-run-end-\(end.id)"
        // A replayed terminal frame must not stack a second window, even if a
        // replayed `meta` reopened the run.
        guard !items.contains(where: { $0.id == id }) else { return }
        appendRow(TimelineItem(
            id: id,
            runID: end.id,
            createdAt: now(),
            kind: .runBoundary(RunWindow(
                id: end.id,
                status: status,
                startedAt: end.startedAt,
                completedAt: now(),
                error: error
            ))
        ))
    }

    /// The user's own turn, added optimistically while the request is in flight
    /// (and by the voice surface as the room delivers it). Keyed by message id:
    /// the room may re-deliver the same message, which updates the row instead
    /// of stacking a second one.
    func appendUserMessage(id: String, text: String) {
        guard !text.isEmpty else { return }
        if let index = items.firstIndex(where: { $0.id == id }) {
            replaceKind(at: index, with: .userMessage(text: text))
            return
        }
        appendRow(TimelineItem(
            id: id,
            runID: liveRun,
            createdAt: now(),
            kind: .userMessage(text: text)
        ))
    }
}

// MARK: - Worker/HTTP activity events

extension ChatTimelineStore {
    /// Appends one decoded activity event (tool, thinking, card) in arrival
    /// order. Tool results settle the matching call by id.
    func apply(_ event: ChatEvent) {
        switch event {
        case let .thinking(id, delta):
            appendLiveReasoning(id: id, delta: delta)
        case .thinkingDone:
            stopReasoningShimmer()
        case let .toolCall(call):
            appendToolCall(call)
        case let .toolResult(result):
            closeReasoning()
            settleToolResult(result)
        case let .approval(approval):
            appendApproval(approval)
        case let .question(question):
            appendQuestion(question)
        case .ui, .subagent, .conversation:
            // Store-level: the activity store owns surfaces and subagents.
            break
        }
    }

    private func appendLiveReasoning(id: String?, delta: String) {
        guard !delta.isEmpty else { return }
        // A reasoning delta after an answer is a new segment; the answer row
        // stays exactly where it was, which is the whole point of the list.
        if let last = lastItem, case let .reasoning(text, _, isOpen) = last.kind, isOpen {
            replaceLastKind(.reasoning(text: text + delta, isLive: true, isOpen: true))
            return
        }
        appendRow(TimelineItem(
            id: id.flatMap { $0.isEmpty ? nil : $0 } ?? "live-reasoning-\(UUID().uuidString)",
            runID: liveRun,
            createdAt: now(),
            kind: .reasoning(text: delta, isLive: true, isOpen: true)
        ))
    }

    /// A `done` frame stops the shimmer without ending the segment: the next
    /// delta before an activity boundary joins the same row.
    private func stopReasoningShimmer() {
        guard let last = lastItem, case let .reasoning(text, _, isOpen) = last.kind else { return }
        replaceLastKind(.reasoning(text: text, isLive: false, isOpen: isOpen))
    }

    private func appendToolCall(_ call: ChatToolCallEvent) {
        closeReasoning()
        appendRow(TimelineItem(
            id: call.id,
            runID: liveRun,
            createdAt: now(),
            kind: .tool(ToolCallInfo(
                id: call.id,
                name: call.tool,
                arguments: call.arguments,
                result: nil,
                isError: false,
                isRunning: true,
                startedAt: now(),
                showsID: true,
                category: call.category,
                subject: call.subject
            ))
        ))
    }

    private func appendApproval(_ approval: ChatApprovalEvent) {
        closeReasoning()
        appendRow(TimelineItem(
            id: approval.questionId,
            runID: liveRun,
            createdAt: now(),
            kind: .approval(ApprovalRequest(
                questionId: approval.questionId,
                tool: approval.tool,
                arguments: approval.arguments
            ))
        ))
    }

    private func appendQuestion(_ question: ChatQuestionEvent) {
        closeReasoning()
        appendRow(TimelineItem(
            id: question.questionId,
            runID: liveRun,
            createdAt: now(),
            kind: .question(QuestionRequest(
                questionId: question.questionId,
                question: question.question,
                interactionType: question.interactionType,
                options: question.options,
                placeholder: question.placeholder,
                maxLength: question.maxLength
            ))
        ))
    }

    /// Settles the announced call by id, keeping its start time for a duration
    /// the server does not have to send.
    private func settleToolResult(_ result: ChatToolResultEvent) {
        guard let index = items.lastIndex(where: { item in
            if case let .tool(info) = item.kind {
                return info.id == result.id
            }
            return false
        }), case let .tool(existing) = items[index].kind
        else { return }
        replaceKind(at: index, with: .tool(ToolCallInfo(
            id: existing.id,
            name: existing.name,
            arguments: existing.arguments,
            result: result.result,
            isError: result.isError,
            isRunning: false,
            startedAt: existing.startedAt,
            duration: existing.startedAt.map { max(0, now().timeIntervalSince($0)) },
            responseSize: result.outputSize,
            showsID: existing.showsID,
            category: result.category ?? existing.serverCategory,
            subject: existing.serverSubject,
            presentation: result.view?.presentation
        )))
    }

    /// Closes the open reasoning row at an activity boundary. A segment that is
    /// not last is untouched: only the newest row can still take deltas.
    private func closeReasoning() {
        guard let last = lastItem, case let .reasoning(text, _, _) = last.kind else { return }
        replaceLastKind(.reasoning(text: text, isLive: false, isOpen: false))
    }
}

// MARK: - Room transcripts

extension ChatTimelineStore {
    /// Replaces the text rows of a LiveKit room transcript, keyed by message id:
    /// a streaming update replaces its row in place, and a new message appends.
    func syncLiveMessages(_ messages: [LiveTranscriptMessage]) {
        for message in messages where !message.text.isEmpty {
            let kind: TimelineItem.Kind = switch message.role {
            case .user:
                .userMessage(text: message.text)
            case .assistant:
                .assistantText(markdown: message.text, citations: message.citations)
            }
            if let index = items.firstIndex(where: { $0.id == message.id }) {
                replaceKind(at: index, with: kind)
            } else {
                appendRow(TimelineItem(
                    id: message.id,
                    runID: liveRun,
                    createdAt: now(),
                    kind: kind
                ))
            }
        }
    }
}
