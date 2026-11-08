import Foundation

/// Which route the view is adopting: the session's, a rebuilt one, or the same
/// room configuration restarted as a fresh conversation.
///
/// Split out of `ChatSessionView` so its body and turn wiring stay readable; the
/// decision itself only reads the active session and the transport.
extension ChatSessionView {
    /// What the route asked for, versus what the active session already holds.
    enum RouteAdoption {
        /// The controller is already listening on the route's conversation.
        case unchanged
        /// A new controller is being installed by the app root.
        case rebuiltController
        /// The same room configuration was restarted as a fresh conversation.
        case restartInPlace
    }

    func adoptRoute() -> RouteAdoption {
        let sameAgent = activeSession.agentName == route.agentName
        let sameLaunch = activeSession.launchConversationID == route.conversationID

        guard let conversationID = route.conversationID else {
            // This "new chat" may have created its conversation over HTTP and
            // then been re-pointed at it so voice could join. The transport
            // still holds that id, so adopt the installed controller instead of
            // starting a second conversation.
            if sameAgent, activeSession.launchConversationID != nil,
               activeSession.launchConversationID == transport.conversationID
            {
                return .unchanged
            }
            if sameAgent, sameLaunch {
                // Re-entering "new chat" after a live id was learned: the same
                // launch key is still valid, so start a fresh conversation in
                // place rather than rebuilding the controller.
                guard activeSession.liveConversationID != nil else { return .unchanged }
                activeSession.startNew(agentName: route.agentName, startsMuted: true)
                return .restartInPlace
            }
            activeSession.startNew(agentName: route.agentName, startsMuted: true)
            resetTranscript()
            return .rebuiltController
        }

        if sameAgent, sameLaunch {
            return .unchanged
        }
        activeSession.open(
            agentName: route.agentName,
            conversationID: conversationID,
            startsMuted: true
        )
        resetTranscript()
        return .rebuiltController
    }

    func resetTranscript() {
        transcript.reset()
        transport.reset()
        history.reset()
    }
}
