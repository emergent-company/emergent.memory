import Foundation
import XCTest
@testable import MemoryConnector

/// Locked, canned process runner for `ConnectorCLI` tests: it records the
/// arguments of every invocation and returns a fixed `ProcessResult`, so no
/// real `memory-connector` process is spawned.
final class CannedRunner: @unchecked Sendable {
    private let lock = NSLock()
    private var _calls: [[String]] = []
    private var result: ProcessResult

    init(result: ProcessResult) {
        self.result = result
    }

    func respond(with result: ProcessResult) {
        lock.withLock { self.result = result }
    }

    var calls: [[String]] {
        lock.withLock { _calls }
    }

    /// Arguments of the most recent invocation, or `[]` when never called.
    var lastArguments: [String] { calls.last ?? [] }

    var runner: ConnectorCLI.Runner {
        { [self] _, arguments, _ in
            lock.withLock {
                _calls.append(arguments)
                return result
            }
        }
    }
}

/// Canned stdin-capable runner, used by the `auth import` tests. Records the
/// argv and stdin payload of every invocation and returns a fixed result, so no
/// real `memory-connector` process is spawned.
final class CannedStdinRunner: @unchecked Sendable {
    private let lock = NSLock()
    private var _calls: [(arguments: [String], stdin: Data?)] = []
    private var result: ProcessResult

    init(result: ProcessResult) {
        self.result = result
    }

    var calls: [(arguments: [String], stdin: Data?)] { lock.withLock { _calls } }

    /// Arguments of the most recent invocation, or `[]` when never called.
    var lastArguments: [String] { calls.last?.arguments ?? [] }

    /// Stdin payload of the most recent invocation, or `nil` when never called.
    var lastStdin: Data? { calls.last?.stdin }

    var runner: ConnectorCLI.StdinRunner {
        { [self] _, arguments, stdin, _ in
            lock.withLock {
                _calls.append((arguments, stdin))
                return result
            }
        }
    }
}

final class ConnectorCLITests: XCTestCase {

    private let binary = URL(fileURLWithPath: "/tmp/memory-connector")
    private let configPath = "/tmp/memory-connector.yml"
    private let serverURL = "https://memory.example.test"

    private func makeCLI(_ runner: CannedRunner, binaryURL: URL? = nil) -> ConnectorCLI {
        ConnectorCLI(binaryURL: binaryURL ?? binary, runner: runner.runner, timeout: 5)
    }

    private func ok(_ json: String) -> ProcessResult {
        ProcessResult(stdout: json, exitCode: 0, timedOut: false)
    }

    // MARK: - auth status

    func testAuthStatusDecodesAndBuildsArguments() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server":"https://memory.example.test","signed_in":true,\
        "email":"user@example.com","issuer":"https://issuer.test",\
        "expires_at":"2030-01-02T03:04:05Z","expired":false}
        """))
        let cli = makeCLI(runner)

        let status = try await cli.authStatus(serverURL: serverURL, configPath: configPath)

        XCTAssertEqual(status, CLIAuthStatus(schemaVersion: 1,
                                             server: serverURL,
                                             signedIn: true,
                                             email: "user@example.com",
                                             issuer: "https://issuer.test",
                                             expiresAt: "2030-01-02T03:04:05Z",
                                             expired: false))
        XCTAssertEqual(runner.lastArguments,
                       ["auth", "status", "--server", serverURL, "--json", "--config", configPath])
    }

    func testAuthStatusOmitsServerWhenNil() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"signed_in":false}
        """))
        let cli = makeCLI(runner)

        let status = try await cli.authStatus(serverURL: nil, configPath: configPath)

        XCTAssertFalse(status.signedIn)
        XCTAssertNil(status.server)
        XCTAssertEqual(runner.lastArguments, ["auth", "status", "--json", "--config", configPath])
    }

    // MARK: - auth start

    func testAuthStartDecodesAndBuildsArguments() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"login_id":"login-1",\
        "authorize_url":"https://issuer.test/authorize?x=1",\
        "state":"state-1","expires_at":"2030-01-02T03:04:05Z"}
        """))
        let cli = makeCLI(runner)

        let start = try await cli.authStart(serverURL: serverURL,
                                            redirectURI: "myapp://callback",
                                            clientID: "native-client",
                                            issuer: "https://issuer.test",
                                            configPath: configPath)

        XCTAssertEqual(start, CLIPKCEStart(schemaVersion: 1,
                                           loginID: "login-1",
                                           authorizeURL: "https://issuer.test/authorize?x=1",
                                           state: "state-1",
                                           expiresAt: "2030-01-02T03:04:05Z"))
        XCTAssertEqual(runner.lastArguments, [
            "auth", "start",
            "--server", serverURL,
            "--client-id", "native-client",
            "--issuer", "https://issuer.test",
            "--redirect-uri", "myapp://callback",
            "--json", "--config", configPath,
        ])
    }

    func testAuthStartOmitsIssuerWhenNil() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"login_id":"l","authorize_url":"u","state":"s","expires_at":"t"}
        """))
        let cli = makeCLI(runner)

        _ = try await cli.authStart(serverURL: nil,
                                    redirectURI: "myapp://callback",
                                    clientID: "native-client",
                                    issuer: nil,
                                    configPath: configPath)

        XCTAssertEqual(runner.lastArguments, [
            "auth", "start",
            "--client-id", "native-client",
            "--redirect-uri", "myapp://callback",
            "--json", "--config", configPath,
        ])
    }

    // MARK: - auth complete

    func testAuthCompleteDecodesAndBuildsArguments() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server":"https://memory.example.test","signed_in":true,\
        "email":"user@example.com"}
        """))
        let cli = makeCLI(runner)

        let status = try await cli.authComplete(serverURL: serverURL,
                                                loginID: "login-1",
                                                code: "code-1",
                                                state: "state-1",
                                                configPath: configPath)

        XCTAssertEqual(status.signedIn, true)
        XCTAssertEqual(status.email, "user@example.com")
        XCTAssertEqual(runner.lastArguments, [
            "auth", "complete",
            "--server", serverURL,
            "--login-id", "login-1",
            "--code", "code-1",
            "--state", "state-1",
            "--json", "--config", configPath,
        ])
    }

    // MARK: - auth access-token

    func testAuthAccessTokenDecodesAndBuildsArguments() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server_url":"https://memory.example.test",\
        "access_token":"access-1","expires_at":"2030-01-02T03:04:05Z"}
        """))
        let cli = makeCLI(runner)

        let token = try await cli.authAccessToken(serverURL: serverURL, configPath: configPath)

        XCTAssertEqual(token, CLIAccessToken(schemaVersion: 1,
                                             serverURL: serverURL,
                                             accessToken: "access-1",
                                             expiresAt: "2030-01-02T03:04:05Z"))
        XCTAssertEqual(runner.lastArguments,
                       ["auth", "access-token", "--server", serverURL, "--json", "--config", configPath])
    }

    // MARK: - projects

    func testProjectsListDecodesAndReturnsProjects() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server":"https://memory.example.test","projects":[\
        {"id":"p1","name":"One","org_id":"org-1","active":true},\
        {"id":"p2","name":"Two","active":false}]}
        """))
        let cli = makeCLI(runner)

        let projects = try await cli.projectsList(serverURL: serverURL, configPath: configPath)

        XCTAssertEqual(projects, [
            CLIProject(id: "p1", name: "One", orgId: "org-1", active: true),
            CLIProject(id: "p2", name: "Two", orgId: nil, active: false),
        ])
        XCTAssertEqual(projects[0].orgId, "org-1", "org_id decodes into orgId")
        XCTAssertNil(projects[1].orgId, "missing org_id decodes as nil")
        XCTAssertEqual(runner.lastArguments,
                       ["projects", "list", "--server", serverURL, "--json", "--config", configPath])
    }

    func testProjectsUseDecodesAndBuildsArguments() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server":"https://memory.example.test",\
        "project":{"id":"p2","name":"Two"},"config":"/tmp/memory-connector.yml"}
        """))
        let cli = makeCLI(runner)

        let use = try await cli.projectsUse(project: "p2",
                                            serverURL: serverURL,
                                            instanceID: "inst-1",
                                            disabledTools: ["a", "b"],
                                            configPath: configPath)

        XCTAssertEqual(use, CLIProjectUse(schemaVersion: 1,
                                          server: serverURL,
                                          project: CLIProjectRef(id: "p2", name: "Two"),
                                          config: configPath))
        XCTAssertEqual(runner.lastArguments, [
            "projects", "use", "p2",
            "--server", serverURL,
            "--instance-id", "inst-1",
            "--disabled-tools", "a,b",
            "--json", "--config", configPath,
        ])
    }

    func testProjectsUseOmitsOptionalFlagsWhenNotSupplied() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server":"s","project":{"id":"p1","name":"One"},"config":"c"}
        """))
        let cli = makeCLI(runner)

        _ = try await cli.projectsUse(project: "p1",
                                      serverURL: nil,
                                      instanceID: nil,
                                      disabledTools: nil,
                                      configPath: configPath)

        XCTAssertEqual(runner.lastArguments,
                       ["projects", "use", "p1", "--json", "--config", configPath])
    }

    func testProjectsUseEmptyDisabledToolsClearsList() async throws {
        let runner = CannedRunner(result: ok("""
        {"schema_version":1,"server":"s","project":{"id":"p1","name":"One"},"config":"c"}
        """))
        let cli = makeCLI(runner)

        _ = try await cli.projectsUse(project: "p1",
                                      serverURL: nil,
                                      instanceID: nil,
                                      disabledTools: [],
                                      configPath: configPath)

        XCTAssertEqual(runner.lastArguments,
                       ["projects", "use", "p1", "--disabled-tools", "", "--json", "--config", configPath])
    }

    // MARK: - auth logout

    func testAuthLogoutSucceedsOnZeroExit() async throws {
        let runner = CannedRunner(result: ok(""))
        let cli = makeCLI(runner)

        try await cli.authLogout(serverURL: serverURL, configPath: configPath)

        XCTAssertEqual(runner.lastArguments,
                       ["auth", "logout", "--server", serverURL, "--config", configPath])
        XCTAssertFalse(runner.lastArguments.contains("--json"),
                       "auth logout emits no JSON, so --json must not be passed")
    }

    func testAuthLogoutThrowsOnNonZeroExit() async {
        let runner = CannedRunner(result: ProcessResult(stdout: "not signed in\n", exitCode: 1, timedOut: false))
        let cli = makeCLI(runner)

        do {
            try await cli.authLogout(serverURL: nil, configPath: configPath)
            XCTFail("expected commandFailed")
        } catch let error as ConnectorCLIError {
            XCTAssertEqual(error, .commandFailed(command: "auth logout", exitCode: 1, message: "not signed in"))
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }

    // MARK: - auth import

    func testAuthImportArgumentsRequiresServer() {
        XCTAssertEqual(ConnectorCLI.authImportArguments(serverURL: serverURL, configPath: configPath),
                       ["auth", "import", "--server", serverURL, "--json", "--config", configPath])
    }

    func testAuthImportPipesSessionAndDecodesStatus() async throws {
        let runner = CannedStdinRunner(result: ok("""
        {"schema_version":1,"server":"https://memory.example.test","signed_in":true,\
        "email":"user@example.com","issuer":"https://issuer.test",\
        "expires_at":"2030-01-02T03:04:05Z","expired":false}
        """))
        let cli = ConnectorCLI(binaryURL: binary, timeout: 5, stdinRunner: runner.runner)
        let sessionJSON = Data(#"{"access_token":"access-1","refresh_token":"refresh-1"}"#.utf8)

        let status = try await cli.authImport(serverURL: serverURL,
                                              sessionJSON: sessionJSON,
                                              configPath: configPath)

        XCTAssertEqual(status, CLIAuthStatus(schemaVersion: 1,
                                             server: serverURL,
                                             signedIn: true,
                                             email: "user@example.com",
                                             issuer: "https://issuer.test",
                                             expiresAt: "2030-01-02T03:04:05Z",
                                             expired: false))
        XCTAssertEqual(runner.lastArguments,
                       ["auth", "import", "--server", serverURL, "--json", "--config", configPath])
        XCTAssertEqual(runner.lastStdin, sessionJSON)
    }

    func testAuthImportThrowsCommandFailedOnNonZeroExit() async {
        let runner = CannedStdinRunner(result: ProcessResult(stdout: "invalid JSON on stdin\n",
                                                             exitCode: 2,
                                                             timedOut: false))
        let cli = ConnectorCLI(binaryURL: binary, timeout: 5, stdinRunner: runner.runner)

        do {
            _ = try await cli.authImport(serverURL: serverURL,
                                         sessionJSON: Data(#"{"access_token":"a"}"#.utf8),
                                         configPath: configPath)
            XCTFail("expected commandFailed")
        } catch let error as ConnectorCLIError {
            XCTAssertEqual(error, .commandFailed(command: "auth import",
                                                 exitCode: 2,
                                                 message: "invalid JSON on stdin"))
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }

    func testAuthImportThrowsDecodingFailedOnMalformedJSON() async {
        let runner = CannedStdinRunner(result: ok("this is not JSON\n"))
        let cli = ConnectorCLI(binaryURL: binary, timeout: 5, stdinRunner: runner.runner)

        do {
            _ = try await cli.authImport(serverURL: serverURL,
                                         sessionJSON: Data(#"{"access_token":"a"}"#.utf8),
                                         configPath: configPath)
            XCTFail("expected decodingFailed")
        } catch let error as ConnectorCLIError {
            guard case .decodingFailed(let command, let message) = error else {
                return XCTFail("unexpected error: \(error)")
            }
            XCTAssertEqual(command, "auth import")
            XCTAssertFalse(message.isEmpty)
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }

    // MARK: - Failure mapping

    func testCommandFailedMapsExitCodeAndTrimmedMessage() async {
        let runner = CannedRunner(result: ProcessResult(stdout: "boom\n", exitCode: 3, timedOut: false))
        let cli = makeCLI(runner)

        do {
            _ = try await cli.authAccessToken(serverURL: serverURL, configPath: configPath)
            XCTFail("expected commandFailed")
        } catch let error as ConnectorCLIError {
            XCTAssertEqual(error, .commandFailed(command: "auth access-token", exitCode: 3, message: "boom"))
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }

    func testMalformedJSONThrowsDecodingFailed() async {
        let runner = CannedRunner(result: ok("this is not JSON\n"))
        let cli = makeCLI(runner)

        do {
            _ = try await cli.authStatus(serverURL: serverURL, configPath: configPath)
            XCTFail("expected decodingFailed")
        } catch let error as ConnectorCLIError {
            guard case .decodingFailed(let command, let message) = error else {
                return XCTFail("unexpected error: \(error)")
            }
            XCTAssertEqual(command, "auth status")
            XCTAssertFalse(message.isEmpty)
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }

    func testMissingBinaryThrowsBinaryNotFound() async {
        let runner = CannedRunner(result: ok("{}"))
        let cli = ConnectorCLI(binaryURL: nil, runner: runner.runner, timeout: 5)

        do {
            _ = try await cli.authStatus(serverURL: serverURL, configPath: configPath)
            XCTFail("expected binaryNotFound")
        } catch let error as ConnectorCLIError {
            XCTAssertEqual(error, .binaryNotFound)
        } catch {
            XCTFail("unexpected error: \(error)")
        }
        XCTAssertTrue(runner.calls.isEmpty, "no process should run without a binary")
    }
}
