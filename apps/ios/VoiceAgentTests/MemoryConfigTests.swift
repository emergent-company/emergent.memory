import Foundation
@testable import Memory
import Testing

struct MemoryConfigTests {
    @Test func buildDefaultsApply() throws {
        let suite = "MemoryConfigTests.defaults.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        let config = MemoryConfig(defaults: defaults)
        #expect(config.apiBaseURL == MemoryConfig.defaultAPIBaseURL)
        #expect(config.agentName == MemoryConfig.defaultAgentName)
        #expect(config.apiKey == MemoryConfig.defaultAPIKey)
    }

    @Test func saveAndReloadRoundTrips() throws {
        let suite = "MemoryConfigTests.roundtrip.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        var config = MemoryConfig(defaults: defaults)
        config.apiBaseURL = "http://10.0.0.9:9999"
        config.agentName = "test-agent"
        config.apiKey = "sekret"
        config.save(defaults: defaults)

        let reloaded = MemoryConfig(defaults: defaults)
        #expect(reloaded.apiBaseURL == "http://10.0.0.9:9999")
        #expect(reloaded.agentName == "test-agent")
        #expect(reloaded.apiKey == "sekret")
    }

    @Test func resetRestoresBuildDefaults() throws {
        let suite = "MemoryConfigTests.reset.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        var config = MemoryConfig(defaults: defaults)
        config.apiBaseURL = "http://x:1"
        config.save(defaults: defaults)
        MemoryConfig.reset(defaults: defaults)

        #expect(MemoryConfig(defaults: defaults).apiBaseURL == MemoryConfig.defaultAPIBaseURL)
    }
}
