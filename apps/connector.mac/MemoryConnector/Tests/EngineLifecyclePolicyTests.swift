import Foundation
@testable import MemoryConnector
import Testing

/// The "should the engine run?" gate: engine runs iff a project is connected
/// AND its config exists. Stale configs for non-connected projects must never
/// start it.
struct EngineLifecyclePolicyTests {
    private func makeConfigURL() throws -> (url: URL, cleanup: () -> Void) {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-lifecycle-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent("memory-connector.yml")
        return (url, { try? FileManager.default.removeItem(at: dir) })
    }

    private func writeConfigFile(at configURL: URL) throws {
        try EngineConfigWriter.write(configURL: configURL, content: """
        server_url: https://api.example.test
        token: emt_x
        instance_id: host-connector
        project_id: p1
        """)
    }

    /// Writes a config with the given project id (omitted when `nil`) and
    /// server URL.
    private func writeConfig(projectID: String?,
                             serverURL: String = "https://api.example.test",
                             at configURL: URL) throws
    {
        var content = "server_url: \(serverURL)\ntoken: emt_x\ninstance_id: host-connector\n"
        if let projectID {
            content += "project_id: \(projectID)\n"
        }
        try EngineConfigWriter.write(configURL: configURL, content: content)
    }

    @Test func noConnectedProjectDoesNotStartEvenWithStaleConfig() throws {
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfigFile(at: configURL) // stale config left on disk from a previous project

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: nil, configURL: configURL)
            == .noConnectedProject)
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: nil, configURL: configURL))
    }

    @Test func connectedProjectWithConfigRuns() throws {
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfigFile(at: configURL)

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
            == .run)
        #expect(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    @Test func connectedProjectMissingConfigIsError() throws {
        // No file written.
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
            == .missingConfig(projectID: "p1"))
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    @Test func connectedProjectWithInvalidConfigIsError() throws {
        // File exists but has no server_url → not a usable config.
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try EngineConfigWriter.write(configURL: configURL, content: "token: emt_x\ninstance_id: h\n")

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
            == .missingConfig(projectID: "p1"))
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    @Test func noConnectionTakesPrecedenceOverMissingConfig() throws {
        // Disconnected + missing file is still "no connected project", not an
        // error: we simply keep the engine stopped.
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: nil, configURL: configURL)
            == .noConnectedProject)
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: nil, configURL: configURL))
    }

    @Test func differentProjectIsWrongProject() throws {
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfig(projectID: "p2", at: configURL)

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
            == .wrongProject(projectID: "p1", configuredProjectID: "p2"))
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    @Test func configWithoutProjectIDIsWrongProject() throws {
        // A config that parses but binds no project id must never serve the
        // connected project.
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfig(projectID: nil, at: configURL)

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
            == .wrongProject(projectID: "p1", configuredProjectID: nil))
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    @Test func serverMismatchIsWrongProject() throws {
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfig(projectID: "p1", serverURL: "https://api.example.test", at: configURL)

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1",
                                               configURL: configURL,
                                               expectedServerURL: "https://other.test")
                == .wrongProject(projectID: "p1", configuredProjectID: "p1"))
        #expect(!EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1",
                                                 configURL: configURL,
                                                 expectedServerURL: "https://other.test"))
    }

    @Test func matchingProjectAndServerRuns() throws {
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfig(projectID: "p1", serverURL: "https://api.example.test", at: configURL)

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1",
                                               configURL: configURL,
                                               expectedServerURL: "https://api.example.test")
                == .run)
        #expect(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1",
                                                configURL: configURL,
                                                expectedServerURL: "https://api.example.test"))
    }

    @Test func blankExpectedServerSkipsServerCheck() throws {
        let (configURL, cleanup) = try makeConfigURL()
        defer { cleanup() }
        try writeConfig(projectID: "p1", serverURL: "https://api.example.test", at: configURL)

        #expect(EngineLifecyclePolicy.decision(connectedProjectID: "p1",
                                               configURL: configURL,
                                               expectedServerURL: "   ")
                == .run)
        #expect(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1",
                                                configURL: configURL,
                                                expectedServerURL: "   "))
    }
}
