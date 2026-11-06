import Foundation

/// One turn the transport sends to the gateway's `/api/chat`.
///
/// A surface action rides a turn rather than an endpoint of its own: the server
/// frames the wording the agent receives (the A2A transport does the same), so
/// no client invents that prose. `message` is then empty.
struct ChatTurnRequest: Equatable, Sendable {
    var conversationID: String?
    var agentDefinitionID: String?
    var message: String
    var surfaceAction: ChatSurfaceAction?
}

/// An A2UI surface action: the surface the user acted on and the payload its
/// component carries. The web chat sends the same shape
/// (`{surfaceId, action: {componentId, response}}`).
struct ChatSurfaceAction: Equatable, Sendable {
    let surfaceID: String
    let componentID: String
    /// The component's response value, kept as the decoded A2UI prop so a card
    /// that answers with a number or an object sends what it rendered.
    let response: ChatUIPropValue
}

/// The surface action as the server reads it off the request body.
struct ChatSurfaceActionBody: Encodable {
    let surfaceId: String
    let action: Payload

    struct Payload: Encodable {
        let componentId: String
        let response: ChatUIPropValue
    }
}

/// The wire body. A struct, not a dictionary, because the action nests; the
/// optionals stay absent, so a typed turn still creates a conversation when
/// `conversationId` is missing and uses the project's default agent when
/// `agentDefinitionId` is. (`surfaceAction` itself requires a conversation.)
struct ChatTurnBody: Encodable {
    let message: String
    let agentDefinitionId: String?
    let conversationId: String?
    let surfaceAction: ChatSurfaceActionBody?

    init(_ turn: ChatTurnRequest) {
        message = turn.message
        agentDefinitionId = turn.agentDefinitionID
        conversationId = turn.conversationID
        surfaceAction = turn.surfaceAction.map { action in
            ChatSurfaceActionBody(
                surfaceId: action.surfaceID,
                action: .init(componentId: action.componentID, response: action.response)
            )
        }
    }
}
