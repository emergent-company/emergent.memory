import Foundation
import XCTest
@testable import MemoryConnector

// MARK: - Fakes

private final class FakeLegacySessionReader: LegacySessionReading, @unchecked Sendable {
    private let lock = NSLock()
    private let session: LegacySession?
    private var reads = 0

    init(session: LegacySession?) { self.session = session }

    func read() -> LegacySession? {
        lock.withLock {
            reads += 1
            return session
        }
    }

    var readCount: Int { lock.withLock { reads } }
}

private final class FakeLegacySessionClearer: LegacySessionClearing, @unchecked Sendable {
    private let lock = NSLock()
    private var clearedFlag = false

    func clear() { lock.withLock { clearedFlag = true } }

    var wasCleared: Bool { lock.withLock { clearedFlag } }
}

private final class FakeLegacyKeychain: LegacyKeychainReading, @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String: String]
    private var deleted: [String] = []

    init(values: [String: String] = [:]) { self.values = values }

    func load(account: String) throws -> String? { lock.withLock { values[account] } }

    func delete(account: String) throws {
        lock.withLock {
            deleted.append(account)
            values[account] = nil
        }
    }

    var deletedAccounts: [String] { lock.withLock { deleted } }
}

// MARK: - Tests

final class LegacyMigratorTests: XCTestCase {

    private let server = "https://memory.example.test"
    private let configPath = "/tmp/memory-connector.yml"
    private let binary = URL(fileURLWithPath: "/tmp/memory-connector")

    private let sample = LegacySession(accessToken: "access-1",
                                       refreshToken: "refresh-1",
                                       expiresAt: Date(timeIntervalSince1970: 1_900_000_000),
                                       issuer: "https://issuer.test",
                                       clientID: "client-1")

    // MARK: Helpers

    private func makeDefaults() -> (UserDefaults, String) {
        let name = "LegacyMigratorTests-\(UUID().uuidString)"
        return (UserDefaults(suiteName: name)!, name)
    }

    private func makeCLI(status: ProcessResult,
                         import importResult: ProcessResult) -> (ConnectorCLI, CannedRunner, CannedStdinRunner) {
        let statusRunner = CannedRunner(result: status)
        let importRunner = CannedStdinRunner(result: importResult)
        let cli = ConnectorCLI(binaryURL: binary,
                               runner: statusRunner.runner,
                               timeout: 5,
                               stdinRunner: importRunner.runner)
        return (cli, statusRunner, importRunner)
    }

    private func signedOut() -> ProcessResult {
        ProcessResult(stdout: #"{"schema_version":1,"signed_in":false}"#, exitCode: 0, timedOut: false)
    }

    private func signedIn() -> ProcessResult {
        ProcessResult(stdout: #"{"schema_version":1,"signed_in":true}"#, exitCode: 0, timedOut: false)
    }

    private func importOK() -> ProcessResult {
        ProcessResult(stdout: #"{"schema_version":1,"signed_in":true}"#, exitCode: 0, timedOut: false)
    }

    private func failure() -> ProcessResult {
        ProcessResult(stdout: "boom\n", exitCode: 2, timedOut: false)
    }

    private func makeTempStore() throws -> (AppSecretStore, URL) {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("LegacySessionTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return (AppSecretStore(baseDirectory: dir), dir)
    }

    // MARK: Migrator

    func testSuccessImportsStdinClearsAndMarksDone() async throws {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, statusRunner, importRunner) = makeCLI(status: signedOut(), import: importOK())
        let reader = FakeLegacySessionReader(session: sample)
        let clearer = FakeLegacySessionClearer()
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: server,
                                      reader: reader,
                                      clearer: clearer,
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertEqual(reader.readCount, 1)
        XCTAssertTrue(clearer.wasCleared)
        XCTAssertTrue(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertEqual(statusRunner.calls.count, 1)
        XCTAssertEqual(importRunner.calls.count, 1)
        XCTAssertEqual(importRunner.lastArguments,
                       ["auth", "import", "--server", server, "--json", "--config", configPath])

        let body = try XCTUnwrap(importRunner.lastStdin)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: String])
        XCTAssertEqual(json["access_token"], "access-1")
        XCTAssertEqual(json["refresh_token"], "refresh-1")
        XCTAssertEqual(json["issuer"], "https://issuer.test")
        XCTAssertEqual(json["client_id"], "client-1")

        let expiresAt = try XCTUnwrap(json["expires_at"])
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        XCTAssertEqual(formatter.date(from: expiresAt), sample.expiresAt)
    }

    func testNoLegacySessionMarksDoneWithoutCallingCLI() async {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, statusRunner, importRunner) = makeCLI(status: signedOut(), import: importOK())
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: server,
                                      reader: FakeLegacySessionReader(session: nil),
                                      clearer: FakeLegacySessionClearer(),
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertTrue(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertTrue(statusRunner.calls.isEmpty)
        XCTAssertTrue(importRunner.calls.isEmpty)
    }

    func testNilServerMarksDoneWithoutCallingCLI() async {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, statusRunner, importRunner) = makeCLI(status: signedOut(), import: importOK())
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: nil,
                                      reader: FakeLegacySessionReader(session: sample),
                                      clearer: FakeLegacySessionClearer(),
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertTrue(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertTrue(statusRunner.calls.isEmpty)
        XCTAssertTrue(importRunner.calls.isEmpty)
    }

    func testEmptyServerMarksDoneWithoutCallingCLI() async {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, statusRunner, importRunner) = makeCLI(status: signedOut(), import: importOK())
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: "   ",
                                      reader: FakeLegacySessionReader(session: sample),
                                      clearer: FakeLegacySessionClearer(),
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertTrue(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertTrue(statusRunner.calls.isEmpty)
        XCTAssertTrue(importRunner.calls.isEmpty)
    }

    func testAlreadySignedInMarksDoneWithoutImporting() async {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, statusRunner, importRunner) = makeCLI(status: signedIn(), import: importOK())
        let clearer = FakeLegacySessionClearer()
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: server,
                                      reader: FakeLegacySessionReader(session: sample),
                                      clearer: clearer,
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertTrue(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertEqual(statusRunner.calls.count, 1)
        XCTAssertTrue(importRunner.calls.isEmpty)
        XCTAssertFalse(clearer.wasCleared)
    }

    func testImportFailureLeavesFlagUnsetAndRetries() async {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, _, importRunner) = makeCLI(status: signedOut(), import: failure())
        let reader = FakeLegacySessionReader(session: sample)
        let clearer = FakeLegacySessionClearer()
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: server,
                                      reader: reader,
                                      clearer: clearer,
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertFalse(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertFalse(clearer.wasCleared)
        XCTAssertEqual(importRunner.calls.count, 1)

        // A second launch retries from scratch, since the flag was never set.
        await migrator.migrateIfNeeded()

        XCTAssertEqual(reader.readCount, 2)
        XCTAssertEqual(importRunner.calls.count, 2)
        XCTAssertFalse(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
    }

    func testStatusFailureLeavesFlagUnsetAndDoesNotImport() async {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (cli, _, importRunner) = makeCLI(status: failure(), import: importOK())
        let migrator = LegacyMigrator(cli: cli,
                                      serverURL: server,
                                      reader: FakeLegacySessionReader(session: sample),
                                      clearer: FakeLegacySessionClearer(),
                                      defaults: defaults,
                                      configPath: configPath)

        await migrator.migrateIfNeeded()

        XCTAssertFalse(defaults.bool(forKey: LegacyMigrator.migrationDoneKey))
        XCTAssertTrue(importRunner.calls.isEmpty)
    }

    // MARK: Reader

    func testReaderPrefersFileOverKeychain() throws {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (secrets, dir) = try makeTempStore()
        defer { try? FileManager.default.removeItem(at: dir) }

        let fileJSON = #"{"accessToken":"file-access","refreshToken":"file-refresh","idToken":"id-1","expiry":700000000.0,"issuer":"https://file-issuer","clientID":"file-client"}"#
        try secrets.writeData(Data(fileJSON.utf8), to: AppSecretStore.sessionFileName)

        let keychain = FakeLegacyKeychain(values: [
            DefaultLegacySessionReader.sessionAccount:
                #"{"accessToken":"keychain-access","refreshToken":"keychain-refresh","idToken":"id-2","expiry":1,"issuer":"https://keychain-issuer","clientID":"keychain-client"}"#,
        ])
        let reader = DefaultLegacySessionReader(defaults: defaults, secrets: secrets, keychain: keychain)

        let session = try XCTUnwrap(reader.read())

        XCTAssertEqual(session.accessToken, "file-access")
        XCTAssertEqual(session.refreshToken, "file-refresh")
        XCTAssertEqual(session.issuer, "https://file-issuer")
        XCTAssertEqual(session.clientID, "file-client")
        XCTAssertEqual(session.expiresAt, Date(timeIntervalSinceReferenceDate: 700_000_000))
    }

    func testReaderParsesISOExpiryAndToleratesMissingFields() throws {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (secrets, dir) = try makeTempStore()
        defer { try? FileManager.default.removeItem(at: dir) }

        let fileJSON = #"{"accessToken":"iso-access","expiry":"2030-01-02T03:04:05Z"}"#
        try secrets.writeData(Data(fileJSON.utf8), to: AppSecretStore.sessionFileName)

        let reader = DefaultLegacySessionReader(defaults: defaults,
                                                secrets: secrets,
                                                keychain: FakeLegacyKeychain())
        let session = try XCTUnwrap(reader.read())

        XCTAssertEqual(session.accessToken, "iso-access")
        XCTAssertNil(session.refreshToken)
        XCTAssertNil(session.issuer)
        XCTAssertNil(session.clientID)

        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        XCTAssertEqual(session.expiresAt, formatter.date(from: "2030-01-02T03:04:05Z"))
    }

    func testReaderFallsBackToSplitKeychainAccounts() throws {
        let (defaults, name) = makeDefaults()
        defer { defaults.removePersistentDomain(forName: name) }
        let (secrets, dir) = try makeTempStore()
        defer { try? FileManager.default.removeItem(at: dir) }

        defaults.set("https://defaults-issuer", forKey: DefaultLegacySessionReader.issuerKey)
        defaults.set("defaults-client", forKey: DefaultLegacySessionReader.clientIDKey)

        let keychain = FakeLegacyKeychain(values: [
            DefaultLegacySessionReader.accessAccount: "keychain-access",
            DefaultLegacySessionReader.accessExpiryAccount: "1900000000",
            DefaultLegacySessionReader.refreshAccount: "keychain-refresh",
        ])
        let reader = DefaultLegacySessionReader(defaults: defaults, secrets: secrets, keychain: keychain)

        let session = try XCTUnwrap(reader.read())

        XCTAssertEqual(session.accessToken, "keychain-access")
        XCTAssertEqual(session.refreshToken, "keychain-refresh")
        XCTAssertEqual(session.expiresAt, Date(timeIntervalSince1970: 1_900_000_000))
        XCTAssertEqual(session.issuer, "https://defaults-issuer")
        XCTAssertEqual(session.clientID, "defaults-client")
    }

    func testClearerDeletesFileAndLegacyKeychainAccounts() throws {
        let (secrets, dir) = try makeTempStore()
        defer { try? FileManager.default.removeItem(at: dir) }
        try secrets.writeData(Data("{}".utf8), to: AppSecretStore.sessionFileName)

        let keychain = FakeLegacyKeychain()
        DefaultLegacySessionClearer(secrets: secrets, keychain: keychain).clear()

        XCTAssertNil(secrets.readData(AppSecretStore.sessionFileName))
        XCTAssertEqual(keychain.deletedAccounts, DefaultLegacySessionClearer.legacyAccounts)
    }
}
