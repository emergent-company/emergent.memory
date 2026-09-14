import Foundation
import Testing

@testable import Memory

@Suite struct MemoryTokenSourceTests {
    private func makeConfig(_ mutate: (inout MemoryConfig) -> Void) -> MemoryConfig {
        let suite = "MemoryTokenSourceTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        var config = MemoryConfig(defaults: defaults)
        mutate(&config)
        return config
    }

    @Test func validTokenEndpointBuildsURL() {
        let config = makeConfig { $0.tokenEndpoint = "https://gw.example.com/api/token" }
        let source = MemoryTokenSource(config: config)
        #expect(source.url.absoluteString == "https://gw.example.com/api/token")
    }

    /// Regression: a malformed endpoint used to force-unwrap (`URL(string:)!`)
    /// and crash. It must now fall back without trapping.
    @Test func malformedTokenEndpointDoesNotCrash() {
        let config = makeConfig { $0.tokenEndpoint = "" }
        let source = MemoryTokenSource(config: config)
        // Must not trap; the fallback is a non-failable file URL.
        #expect(source.url.path.contains("invalid-token-endpoint"))
    }

    /// The API key is passed through as the X-API-Key header.
    @Test func headersCarryAPIKey() {
        let config = makeConfig { $0.apiKey = "sekret" }
        let source = MemoryTokenSource(config: config)
        #expect(source.headers["X-API-Key"] == "sekret")
    }
}
