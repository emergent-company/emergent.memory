import Foundation
@testable import Memory
import Testing

struct ObjectBrowserClientTests {
    private func client(endpoint: String = "http://host:8080/api/token") -> ObjectBrowserClient {
        var config = MemoryConfig()
        config.tokenEndpoint = endpoint
        return ObjectBrowserClient(config: config)
    }

    @Test func baseURLStripsTokenPath() {
        #expect(client().gatewayBaseURL == URL(string: "http://host:8080"))
        #expect(client(endpoint: "https://host/gw/api/token").gatewayBaseURL == URL(string: "https://host/gw"))
    }

    @Test func buildsURLWithProvenanceAndNoCursor() throws {
        let url = try client().makeURL(agentID: "a1", provenance: .any, cursor: nil)
        #expect(url.path == "/api/agents/a1/objects")
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        #expect(query.contains(URLQueryItem(name: "provenance", value: "any")))
        #expect(query.contains(where: { $0.name == "cursor" }) == false)
    }

    @Test func buildsURLWithProvenanceAndCursor() throws {
        let url = try client().makeURL(agentID: "a1", provenance: .updated, cursor: "cur2")
        #expect(url.path == "/api/agents/a1/objects")
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        #expect(query.contains(URLQueryItem(name: "provenance", value: "updated")))
        #expect(query.contains(URLQueryItem(name: "cursor", value: "cur2")))
    }

    @Test func createdAtModeUsesCreatedValue() throws {
        let url = try client().makeURL(agentID: "a1", provenance: .created, cursor: "")
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        #expect(query.contains(URLQueryItem(name: "provenance", value: "created")))
        #expect(query.contains(where: { $0.name == "cursor" }) == false)
    }

    @Test func decodePageReadsItemsAndCursor() throws {
        let json = #"{"items":[{"id":"o1","type":"person","key":"sam","status":"active"}],"next_cursor":"cur2"}"#
        let page = try ObjectBrowserClient.decodePage(Data(json.utf8))
        #expect(page.items.map(\.id) == ["o1"])
        #expect(page.items.first?.type == "person")
        #expect(page.nextCursor == "cur2")
    }

    @Test func decodePageEmptyCursorIsNil() throws {
        let json = #"{"items":[],"next_cursor":""}"#
        let page = try ObjectBrowserClient.decodePage(Data(json.utf8))
        #expect(page.items.isEmpty)
        #expect(page.nextCursor == nil)
    }

    @Test func decodePageMissingItemsIsEmpty() throws {
        let page = try ObjectBrowserClient.decodePage(Data(#"{"next_cursor":"c1"}"#.utf8))
        #expect(page.items.isEmpty)
        #expect(page.nextCursor == "c1")
    }

    @Test func decodesObjectProperties() throws {
        let json = #"{"id":"o1","canonical_id":"c1","type":"person","key":"sam","status":"active","labels":["person"],"properties":{"name":"Sam","age":42,"tags":["colleague"]},"created_at":"2026-09-01T00:00:00Z"}"#
        let object = try JSONDecoder().decode(GraphObject.self, from: Data(json.utf8))
        #expect(object.id == "o1")
        #expect(object.canonicalID == "c1")
        #expect(object.labels == ["person"])
        #expect(object.properties?["name"]?.display == "Sam")
        #expect(object.properties?["age"]?.display == "42")
        #expect(object.properties?["tags"]?.display == #"["colleague"]"#)
        #expect(object.createdAt == "2026-09-01T00:00:00Z")
        #expect(object.displayTitle == "sam")
    }

    @Test func decodesDefensively() throws {
        let object = try JSONDecoder().decode(GraphObject.self, from: Data(#"{}"#.utf8))
        #expect(object.id == "")
        #expect(object.key == "")
        #expect(object.properties == nil)
        #expect(object.labels == nil)
        #expect(object.matches(""))
        #expect(object.matches("anything") == false)
    }
}
