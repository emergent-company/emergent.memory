import XCTest
@testable import EmergentKit

/// Tests for JSON encoding / decoding of Swift data models.
final class ModelsTests: XCTestCase {

    private let decoder = JSONDecoder()
    private let encoder = JSONEncoder()

    // MARK: - HealthResponse

    func testHealthResponseDecoding() throws {
        let json = """
        {"status":"healthy","timestamp":"2026-01-01T00:00:00Z","version":"1.0.0"}
        """
        let model = try decoder.decode(HealthResponse.self, from: Data(json.utf8))
        XCTAssertEqual(model.status, "healthy")
        XCTAssertEqual(model.version, "1.0.0")
    }

    // MARK: - SearchRequest

    func testSearchRequestEncoding() throws {
        let req = SearchRequest(query: "machine learning", limit: 5, resultTypes: "text")
        let data = try encoder.encode(req)
        let dict = try JSONSerialization.jsonObject(with: data) as! [String: Any]
        XCTAssertEqual(dict["query"] as? String, "machine learning")
        XCTAssertEqual(dict["limit"] as? Int, 5)
        XCTAssertEqual(dict["result_types"] as? String, "text")
    }

    // MARK: - SearchResponse

    func testSearchResponseDecoding() throws {
        let json = """
        {
          "results": [
            {"type":"text","score":0.95,"rank":1,"content":"Hello world","chunk_id":"c1","document_id":"d1"}
          ],
          "metadata": {"totalResults": 1}
        }
        """
        let resp = try decoder.decode(SearchResponse.self, from: Data(json.utf8))
        XCTAssertEqual(resp.results.count, 1)
        XCTAssertEqual(resp.results[0].content, "Hello world")
        XCTAssertEqual(resp.results[0].type, "text")
        XCTAssertEqual(resp.metadata?.total, 1)
    }

    // MARK: - ChatRequest

    func testChatRequestEncoding() throws {
        let req = ChatRequest(message: "Hello!", conversationID: "conv_123")
        let data = try encoder.encode(req)
        let dict = try JSONSerialization.jsonObject(with: data) as! [String: Any]
        XCTAssertEqual(dict["message"] as? String, "Hello!")
        XCTAssertEqual(dict["conversation_id"] as? String, "conv_123")
    }

    // MARK: - ChatResponse

    func testChatResponseDecoding() throws {
        let json = """
        {"conversation_id":"conv_abc","content":"Hi there!"}
        """
        let resp = try decoder.decode(ChatResponse.self, from: Data(json.utf8))
        XCTAssertEqual(resp.conversationID, "conv_abc")
        XCTAssertEqual(resp.content, "Hi there!")
    }

    // MARK: - ListDocumentsResponse

    func testListDocumentsDecoding() throws {
        let json = """
        {
          "documents": [
            {"id":"doc_1","filename":"report.pdf","source_type":"upload"}
          ],
          "total": 1
        }
        """
        let resp = try decoder.decode(ListDocumentsResponse.self, from: Data(json.utf8))
        XCTAssertEqual(resp.total, 1)
        XCTAssertEqual(resp.documents.first?.filename, "report.pdf")
    }

    // MARK: - Sendable conformance check (compile-time)

    func testSendableConformance() {
        // If these compile, Sendable conformance is correct.
        func requireSendable<T: Sendable>(_: T) {}
        requireSendable(HealthResponse(status: "ok"))
        requireSendable(SearchRequest(query: "q"))
        requireSendable(SearchResponse(results: [], metadata: nil))
        requireSendable(ChatRequest(message: "m"))
        requireSendable(ChatResponse(conversationID: nil, content: "c"))
        requireSendable(ListDocumentsResponse(documents: [], total: 0, nextCursor: nil))
    }
}
