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

    @Test func projectIdentityRoundTripsAndResets() throws {
        let suite = "MemoryConfigTests.projects.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        var config = MemoryConfig(defaults: defaults)
        #expect(config.projectID == MemoryConfig.defaultProjectID)
        #expect(config.orgID == MemoryConfig.defaultOrgID)
        #expect(config.projectName == MemoryConfig.defaultProjectName)

        config.projectID = "proj-1"
        config.orgID = "org-1"
        config.projectName = "Project One"
        config.save(defaults: defaults)

        let reloaded = MemoryConfig(defaults: defaults)
        #expect(reloaded.projectID == "proj-1")
        #expect(reloaded.orgID == "org-1")
        #expect(reloaded.projectName == "Project One")

        MemoryConfig.reset(defaults: defaults)
        let reset = MemoryConfig(defaults: defaults)
        #expect(reset.projectID == "")
        #expect(reset.orgID == "")
        #expect(reset.projectName == "")
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

    @Test func legacyDefaultsFallBackToCurrentDefaults() throws {
        let suite = "MemoryConfigTests.legacy.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        defaults.set("memory-google-rt", forKey: MemoryConfig.agentNameKey)
        defaults.set("http://100.69.175.118:8080/api/token", forKey: MemoryConfig.tokenEndpointKey)
        defaults.set("http://100.69.175.118:8080", forKey: MemoryConfig.apiBaseURLKey)

        let config = MemoryConfig(defaults: defaults)
        #expect(config.agentName == MemoryConfig.defaultAgentName)
        #expect(config.tokenEndpoint == MemoryConfig.defaultTokenEndpoint)
        #expect(config.apiBaseURL == MemoryConfig.defaultAPIBaseURL)
    }

    @Test func customValuesArePreserved() throws {
        let suite = "MemoryConfigTests.custom.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        defaults.set("coder", forKey: MemoryConfig.agentNameKey)
        defaults.set("http://10.0.0.9:9000/api/token", forKey: MemoryConfig.tokenEndpointKey)
        defaults.set("http://10.0.0.9:9000", forKey: MemoryConfig.apiBaseURLKey)

        let config = MemoryConfig(defaults: defaults)
        #expect(config.agentName == "coder")
        #expect(config.tokenEndpoint == "http://10.0.0.9:9000/api/token")
        #expect(config.apiBaseURL == "http://10.0.0.9:9000")
    }
}
