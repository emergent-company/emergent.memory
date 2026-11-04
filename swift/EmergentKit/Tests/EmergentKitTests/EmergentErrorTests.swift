import XCTest
@testable import EmergentKit

/// Tests for ``EmergentError`` JSON parsing and error mapping.
final class EmergentErrorTests: XCTestCase {

    // MARK: - Error mapping

    func testUnauthorizedMapping() {
        let err = EmergentError.from(message: "401 unauthorized access")
        guard case .unauthorized = err else {
            return XCTFail("Expected .unauthorized, got \(err)")
        }
    }

    func testNotFoundMapping() {
        let err = EmergentError.from(message: "resource not found: 404")
        guard case .notFound = err else {
            return XCTFail("Expected .notFound, got \(err)")
        }
    }

    func testCancelledMapping() {
        let err = EmergentError.from(message: "context canceled")
        guard case .cancelled = err else {
            return XCTFail("Expected .cancelled, got \(err)")
        }
    }

    func testNetworkFailureMapping() {
        let err = EmergentError.from(message: "connection refused")
        guard case .networkFailure = err else {
            return XCTFail("Expected .networkFailure, got \(err)")
        }
    }

    func testServerErrorMapping() {
        let err = EmergentError.from(message: "internal server error 500")
        guard case .serverError = err else {
            return XCTFail("Expected .serverError, got \(err)")
        }
    }

    func testUnknownFallthrough() {
        let err = EmergentError.from(message: "some completely unexpected message")
        guard case .unknown = err else {
            return XCTFail("Expected .unknown, got \(err)")
        }
    }

    // MARK: - localizedDescription

    func testLocalizedDescriptionNotEmpty() {
        let cases: [EmergentError] = [
            .unauthorized("u"),
            .networkFailure("n"),
            .invalidRequest("i"),
            .notFound("nf"),
            .cancelled,
            .serverError("s"),
            .internal("int"),
            .unknown("unk"),
        ]
        for err in cases {
            XCTAssertFalse(err.errorDescription?.isEmpty ?? true, "errorDescription empty for \(err)")
        }
    }
}
