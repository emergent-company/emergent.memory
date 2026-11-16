import Foundation
@testable import MemoryConnector
import XCTest

/// The silent-re-auth surface of `ConnectorCLI`: `prompt`/`login_hint` on
/// `auth start` and the `auth cancel` wrapper that discards a declined attempt.
///
/// Split out of `ConnectorCLITests.swift` so that class stays at its SwiftLint
/// `type_body_length` baseline; `CannedRunner` is shared from there.
final class ConnectorCLIAuthStartTests: XCTestCase {
    private let binary = URL(fileURLWithPath: "/tmp/memory-connector")
    private let configPath = "/tmp/memory-connector.yml"
    private let serverURL = "https://memory.example.test"

    private func makeCLI(_ runner: CannedRunner) -> ConnectorCLI {
        ConnectorCLI(binaryURL: binary, runner: runner.runner, timeout: 5)
    }

    private func ok(_ json: String) -> ProcessResult {
        ProcessResult(stdout: json, exitCode: 0, timedOut: false)
    }

    func testAuthStartCarriesSilentPromptAndLoginHint() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"login_id":"l","authorize_url":"u","state":"s","expires_at":"t"}
        """))
        let cli = makeCLI(runner)

        _ = try await cli.authStart(serverURL: serverURL,
                                    redirectURI: "myapp://callback",
                                    clientID: "native-client",
                                    issuer: nil,
                                    prompt: ConnectorCLI.silentPrompt,
                                    loginHint: "a@example.test",
                                    configPath: configPath)

        XCTAssertEqual(runner.lastArguments, [
            "auth", "start",
            "--server", serverURL,
            "--client-id", "native-client",
            "--prompt", "none",
            "--login-hint", "a@example.test",
            "--redirect-uri", "myapp://callback",
            "--json", "--config", configPath,
        ])
    }

    func testAuthStartOmitsSilentFlagsWhenUnset() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"login_id":"l","authorize_url":"u","state":"s","expires_at":"t"}
        """))
        let cli = makeCLI(runner)

        _ = try await cli.authStart(serverURL: nil,
                                    redirectURI: "myapp://callback",
                                    clientID: "native-client",
                                    issuer: nil,
                                    prompt: "",
                                    loginHint: "",
                                    configPath: configPath)

        XCTAssertFalse(runner.lastArguments.contains("--prompt"))
        XCTAssertFalse(runner.lastArguments.contains("--login-hint"),
                       "an empty hint must not be sent as an empty flag")
    }

    func testAuthCancelBuildsArguments() async throws {
        let runner = CannedRunner(result: ok(""))
        let cli = makeCLI(runner)

        try await cli.authCancel(loginID: "login-1", configPath: configPath)

        XCTAssertEqual(runner.lastArguments, [
            "auth", "cancel",
            "--login-id", "login-1",
            "--config", configPath,
        ])
    }
}
