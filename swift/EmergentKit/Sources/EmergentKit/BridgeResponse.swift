/// BridgeResponse.swift — decoding of the standard bridge JSON envelope.
///
/// Every Go bridge function returns `{"result":…}` or `{"error":"…"}` as a JSON
/// string. This lives in `EmergentKit` rather than in `EmergentBridge` because
/// the error mapping it performs produces ``EmergentError``, which is part of
/// this module — a target can only reference the modules it depends on, and
/// `EmergentBridge` sits *below* `EmergentKit` in the graph.

import Foundation

// MARK: - JSON envelope

/// The standard JSON wrapper used by every bridge function.
struct BridgeEnvelope<T: Decodable>: Decodable {
    let result: T?
    let error: String?
}

/// Decodes the standard `{"result":…,"error":"…"}` envelope from a bridge
/// JSON string, throwing an `EmergentError` if the envelope contains an error
/// or if decoding fails.
func decodeBridgeResponse<T: Decodable>(_ json: String) throws -> T {
    guard let data = json.data(using: .utf8) else {
        throw EmergentError.internal("Bridge returned non-UTF8 data")
    }
    let envelope = try JSONDecoder().decode(BridgeEnvelope<T>.self, from: data)
    if let errMsg = envelope.error, !errMsg.isEmpty {
        throw EmergentError.from(message: errMsg)
    }
    guard let result = envelope.result else {
        throw EmergentError.internal("Bridge envelope had no result and no error")
    }
    return result
}
