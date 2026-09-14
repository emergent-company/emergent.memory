import Foundation

/// Errors surfaced by `ConnectorCLI`.
///
/// The bundled `memory-connector` process writes combined stdout/stderr, so a
/// failed command's diagnostic text arrives in `ProcessResult.stdout`; it is
/// surfaced as `message` after trimming surrounding whitespace.
enum ConnectorCLIError: Error, Equatable, LocalizedError {
    /// The embedded `memory-connector` binary could not be located.
    case binaryNotFound
    /// The command exited non-zero.
    case commandFailed(command: String, exitCode: Int32, message: String)
    /// The command exited zero but its stdout was not the expected JSON.
    case decodingFailed(command: String, message: String)

    var errorDescription: String? {
        switch self {
        case .binaryNotFound:
            return "The bundled memory-connector binary could not be found."
        case .commandFailed(let command, let exitCode, let message):
            let detail = message.isEmpty ? "no output" : message
            return "memory-connector \(command) failed (exit \(exitCode)): \(detail)"
        case .decodingFailed(let command, let message):
            return "Could not read memory-connector \(command) output: \(message)"
        }
    }
}

/// Additive wrapper around the bundled `memory-connector` CLI's JSON contract.
///
/// It shells out with `ProcessRunner`, asks for `--json`, and decodes the
/// stable documents in `CLIJSON.swift`. This type is intentionally inert: it
/// owns no app state and performs no UI work, so adopting it changes no
/// existing behavior. Tests inject a `Runner` (and a `binaryURL`) to avoid
/// spawning real processes.
final class ConnectorCLI: @unchecked Sendable {

    /// Signature of one CLI invocation. `@Sendable` so the wrapper can be used
    /// from any executor without data races.
    typealias Runner = @Sendable (_ executable: URL, _ arguments: [String], _ timeout: TimeInterval) async -> ProcessResult

    /// Signature of an invocation that pipes a JSON body to the child's stdin.
    /// Kept separate from `Runner` so existing command wiring is unchanged.
    typealias StdinRunner = @Sendable (_ executable: URL, _ arguments: [String], _ stdin: Data?, _ timeout: TimeInterval) async -> ProcessResult

    /// Default runner: the real `ProcessRunner`.
    static let defaultRunner: Runner = { executable, arguments, timeout in
        await ProcessRunner.run(executable: executable, arguments: arguments, timeout: timeout)
    }

    /// Default stdin-capable runner: the real `ProcessRunner` stdin overload.
    static let defaultStdinRunner: StdinRunner = { executable, arguments, stdin, timeout in
        await ProcessRunner.run(executable: executable, arguments: arguments, stdin: stdin, timeout: timeout)
    }

    /// Resolves the embedded engine binary from the app bundle.
    static func defaultBinaryURL() -> URL? {
        Bundle.main.url(forResource: "memory-connector", withExtension: nil)
    }

    private let binaryURL: URL?
    private let runner: Runner
    private let stdinRunner: StdinRunner
    private let timeout: TimeInterval

    /// - Parameters:
    ///   - binaryURL: the CLI to run. Defaults to the bundle-resolved engine;
    ///     pass `nil` explicitly to model a missing binary (`.binaryNotFound`).
    ///   - runner: injected process runner (defaults to `ProcessRunner.run`).
    ///   - stdinRunner: injected stdin-capable runner, used by `authImport`.
    ///   - timeout: per-invocation timeout.
    init(binaryURL: URL? = ConnectorCLI.defaultBinaryURL(),
         runner: @escaping Runner = ConnectorCLI.defaultRunner,
         timeout: TimeInterval = 15,
         stdinRunner: @escaping StdinRunner = ConnectorCLI.defaultStdinRunner) {
        self.binaryURL = binaryURL
        self.runner = runner
        self.stdinRunner = stdinRunner
        self.timeout = timeout
    }

    // MARK: - Public commands

    func authStatus(serverURL: String?, configPath: String) async throws -> CLIAuthStatus {
        try await runJSON(CLIAuthStatus.self,
                          command: "auth status",
                          arguments: ConnectorCLI.authStatusArguments(serverURL: serverURL, configPath: configPath))
    }

    func authStart(serverURL: String?,
                   redirectURI: String,
                   clientID: String,
                   issuer: String?,
                   configPath: String) async throws -> CLIPKCEStart {
        try await runJSON(CLIPKCEStart.self,
                          command: "auth start",
                          arguments: ConnectorCLI.authStartArguments(serverURL: serverURL,
                                                                      redirectURI: redirectURI,
                                                                      clientID: clientID,
                                                                      issuer: issuer,
                                                                      configPath: configPath))
    }

    func authComplete(serverURL: String?,
                      loginID: String,
                      code: String,
                      state: String,
                      configPath: String) async throws -> CLIAuthStatus {
        try await runJSON(CLIAuthStatus.self,
                          command: "auth complete",
                          arguments: ConnectorCLI.authCompleteArguments(serverURL: serverURL,
                                                                         loginID: loginID,
                                                                         code: code,
                                                                         state: state,
                                                                         configPath: configPath))
    }

    func authAccessToken(serverURL: String?, configPath: String) async throws -> CLIAccessToken {
        try await runJSON(CLIAccessToken.self,
                          command: "auth access-token",
                          arguments: ConnectorCLI.authAccessTokenArguments(serverURL: serverURL, configPath: configPath))
    }

    func projectsList(serverURL: String?, configPath: String) async throws -> [CLIProject] {
        let document = try await runJSON(CLIProjectsList.self,
                                         command: "projects list",
                                         arguments: ConnectorCLI.projectsListArguments(serverURL: serverURL, configPath: configPath))
        return document.projects
    }

    func projectsUse(project: String,
                     serverURL: String?,
                     instanceID: String?,
                     disabledTools: [String]?,
                     configPath: String) async throws -> CLIProjectUse {
        try await runJSON(CLIProjectUse.self,
                          command: "projects use",
                          arguments: ConnectorCLI.projectsUseArguments(project: project,
                                                                        serverURL: serverURL,
                                                                        instanceID: instanceID,
                                                                        disabledTools: disabledTools,
                                                                        configPath: configPath))
    }

    /// `auth logout` emits no JSON: success is simply a zero exit.
    func authLogout(serverURL: String?, configPath: String) async throws {
        _ = try await execute(command: "auth logout",
                              arguments: ConnectorCLI.authLogoutArguments(serverURL: serverURL, configPath: configPath))
    }

    /// `auth import` reads a session JSON body from stdin (never argv, so the
    /// tokens stay out of `ps`) and prints the resulting `auth status` document.
    /// Used to migrate a legacy Keychain session into the connector config.
    func authImport(serverURL: String, sessionJSON: Data, configPath: String) async throws -> CLIAuthStatus {
        try await runJSON(CLIAuthStatus.self,
                          command: "auth import",
                          arguments: ConnectorCLI.authImportArguments(serverURL: serverURL, configPath: configPath),
                          stdin: sessionJSON)
    }

    // MARK: - Command builders (internal for tests)

    static func authStatusArguments(serverURL: String?, configPath: String) -> [String] {
        ["auth", "status"] + serverArguments(serverURL) + ["--json", "--config", configPath]
    }

    static func authStartArguments(serverURL: String?,
                                   redirectURI: String,
                                   clientID: String,
                                   issuer: String?,
                                   configPath: String) -> [String] {
        var arguments = ["auth", "start"] + serverArguments(serverURL)
        arguments += ["--client-id", clientID]
        if let issuer, !issuer.isEmpty {
            arguments += ["--issuer", issuer]
        }
        arguments += ["--redirect-uri", redirectURI, "--json", "--config", configPath]
        return arguments
    }

    static func authCompleteArguments(serverURL: String?,
                                      loginID: String,
                                      code: String,
                                      state: String,
                                      configPath: String) -> [String] {
        ["auth", "complete"] + serverArguments(serverURL)
            + ["--login-id", loginID, "--code", code, "--state", state, "--json", "--config", configPath]
    }

    static func authAccessTokenArguments(serverURL: String?, configPath: String) -> [String] {
        ["auth", "access-token"] + serverArguments(serverURL) + ["--json", "--config", configPath]
    }

    static func projectsListArguments(serverURL: String?, configPath: String) -> [String] {
        ["projects", "list"] + serverArguments(serverURL) + ["--json", "--config", configPath]
    }

    static func projectsUseArguments(project: String,
                                     serverURL: String?,
                                     instanceID: String?,
                                     disabledTools: [String]?,
                                     configPath: String) -> [String] {
        var arguments = ["projects", "use", project] + serverArguments(serverURL)
        if let instanceID, !instanceID.isEmpty {
            arguments += ["--instance-id", instanceID]
        }
        if let disabledTools {
            arguments += ["--disabled-tools", disabledTools.joined(separator: ",")]
        }
        arguments += ["--json", "--config", configPath]
        return arguments
    }

    static func authLogoutArguments(serverURL: String?, configPath: String) -> [String] {
        ["auth", "logout"] + serverArguments(serverURL) + ["--config", configPath]
    }

    /// `auth import` requires `--server`; the session body arrives on stdin.
    static func authImportArguments(serverURL: String, configPath: String) -> [String] {
        ["auth", "import", "--server", serverURL, "--json", "--config", configPath]
    }

    /// `--server <url>` only when a non-empty URL is supplied.
    private static func serverArguments(_ serverURL: String?) -> [String] {
        guard let serverURL, !serverURL.isEmpty else { return [] }
        return ["--server", serverURL]
    }

    // MARK: - Execution

    /// Runs the command, throwing `.binaryNotFound` when there is no binary and
    /// `.commandFailed` on a non-zero exit.
    private func execute(command: String, arguments: [String]) async throws -> ProcessResult {
        guard let binaryURL else {
            throw ConnectorCLIError.binaryNotFound
        }
        let result = await runner(binaryURL, arguments, timeout)
        guard result.exitCode == 0 else {
            let message = result.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
            throw ConnectorCLIError.commandFailed(command: command, exitCode: result.exitCode, message: message)
        }
        return result
    }

    /// Stdin-capable variant of `execute`, used by `authImport`. Uses the
    /// injected `stdinRunner` (default: `ProcessRunner.run(…, stdin:, …)`).
    private func execute(command: String, arguments: [String], stdin: Data?) async throws -> ProcessResult {
        guard let binaryURL else {
            throw ConnectorCLIError.binaryNotFound
        }
        let result = await stdinRunner(binaryURL, arguments, stdin, timeout)
        guard result.exitCode == 0 else {
            let message = result.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
            throw ConnectorCLIError.commandFailed(command: command, exitCode: result.exitCode, message: message)
        }
        return result
    }

    /// Runs the command and decodes its stdout as `T`.
    private func runJSON<T: Decodable>(_ type: T.Type, command: String, arguments: [String]) async throws -> T {
        let result = try await execute(command: command, arguments: arguments)
        return try ConnectorCLI.decodeJSON(type, command: command, stdout: result.stdout)
    }

    /// Stdin-capable variant of `runJSON`.
    private func runJSON<T: Decodable>(_ type: T.Type,
                                       command: String,
                                       arguments: [String],
                                       stdin: Data?) async throws -> T {
        let result = try await execute(command: command, arguments: arguments, stdin: stdin)
        return try ConnectorCLI.decodeJSON(type, command: command, stdout: result.stdout)
    }

    private static func decodeJSON<T: Decodable>(_ type: T.Type, command: String, stdout: String) throws -> T {
        guard let data = stdout.data(using: .utf8) else {
            throw ConnectorCLIError.decodingFailed(command: command, message: "stdout was not valid UTF-8")
        }
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw ConnectorCLIError.decodingFailed(command: command, message: error.localizedDescription)
        }
    }
}
