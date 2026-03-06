import XCTest
@testable import EmergentKit
@testable import EmergentBridge

/// Tests for the bridge JSON envelope decoding utilities.
final class BridgeEnvelopeTests: XCTestCase {

    // MARK: - decodeBridgeResponse

    struct Payload: Decodable, Equatable {
        let value: String
    }

    func testDecodeSuccess() throws {
        let json = #"{"result":{"value":"hello"}}"#
        let payload: Payload = try decodeBridgeResponse(json)
        XCTAssertEqual(payload.value, "hello")
    }

    func testDecodeErrorThrows() {
        let json = #"{"error":"not found: 404"}"#
        XCTAssertThrowsError(try { let _: Payload = try decodeBridgeResponse(json) }()) { error in
            guard case EmergentError.notFound = error else {
                return XCTFail("Expected .notFound, got \(error)")
            }
        }
    }

    func testDecodeMalformedJSONThrows() {
        let json = "not json at all"
        XCTAssertThrowsError(try { let _: Payload = try decodeBridgeResponse(json) }())
    }

    func testDecodeEmptyResultThrows() {
        let json = #"{"result":null}"#
        XCTAssertThrowsError(try { let _: Payload = try decodeBridgeResponse(json) }()) { error in
            guard case EmergentError.internal = error else {
                return XCTFail("Expected .internal, got \(error)")
            }
        }
    }
}
