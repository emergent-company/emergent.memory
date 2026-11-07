import Foundation
@testable import MemoryConnector
import Testing

/// `ChildEnvironment` widens the engine child's PATH with the locations a
/// Finder-launched app never inherits. These tests pin the composition rules:
/// existing entries survive in order, duplicates collapse, only directories that
/// exist are appended, and an unreadable system list is not a failure.
struct ChildEnvironmentTests {
    // MARK: - Fixtures

    /// A throwaway home directory with the given home-relative bin dirs created.
    private func makeHome(_ relativeDirectories: [String]) throws -> URL {
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("ChildEnvironmentTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        for relative in relativeDirectories {
            try FileManager.default.createDirectory(
                at: home.appendingPathComponent(relative, isDirectory: true),
                withIntermediateDirectories: true
            )
        }
        return home
    }

    private func cleanup(_ home: URL) {
        try? FileManager.default.removeItem(at: home)
    }

    // MARK: - PATH composition

    @Test func existingEntriesSurviveInOrder() throws {
        let home = try makeHome([".local/bin"])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: "/usr/bin:/bin",
                                         home: home.path,
                                         systemDirectories: [])
        #expect(path.hasPrefix("/usr/bin:/bin:"))
        #expect(path.hasSuffix("\(home.path)/.local/bin"))
    }

    /// The reported bug: `uvx` lives in `~/.local/bin`, which a Finder-launched
    /// app does not have on PATH. It must be appended even though the inherited
    /// PATH is non-empty.
    @Test func appendsUserBinDirectoryThatExists() throws {
        let home = try makeHome([".local/bin"])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: "/usr/bin:/bin", home: home.path,
                                         systemDirectories: [])
        #expect(path.split(separator: ":").contains(Substring("\(home.path)/.local/bin")))
    }

    @Test func skipsUserBinDirectoriesThatDoNotExist() throws {
        let home = try makeHome([])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: "/usr/bin", home: home.path,
                                         systemDirectories: [])
        #expect(path == "/usr/bin")
    }

    @Test func systemDirectoriesFollowExistingEntries() throws {
        let home = try makeHome([])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: "/usr/bin",
                                         home: home.path,
                                         systemDirectories: ["/etc/one", "/etc/two"])
        #expect(path == "/usr/bin:/etc/one:/etc/two")
    }

    @Test func duplicatesCollapseAcrossSources() throws {
        let home = try makeHome([".local/bin"])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(
            existing: "/usr/bin:/opt/homebrew/bin:/usr/bin",
            home: home.path,
            systemDirectories: ["/opt/homebrew/bin", "/usr/local/bin"]
        )
        let entries = path.split(separator: ":").map(String.init)
        #expect(entries == ["/usr/bin", "/opt/homebrew/bin", "/usr/local/bin",
                            "\(home.path)/.local/bin"])
    }

    @Test func emptyAndWhitespaceEntriesAreDropped() throws {
        let home = try makeHome([])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: ":/usr/bin::  :/bin",
                                         home: home.path,
                                         systemDirectories: [" "])
        #expect(path == "/usr/bin:/bin")
    }

    @Test func nilPathIsRebuiltFromTheFallbacks() throws {
        let home = try makeHome([".local/bin"])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: nil, home: home.path,
                                         systemDirectories: ["/usr/local/bin"])
        let entries = path.split(separator: ":").map(String.init)
        #expect(entries == ["/usr/local/bin", "\(home.path)/.local/bin"])
    }

    // MARK: - Environment

    @Test func appendsUserBinDirectoriesInTheirDeclaredOrder() throws {
        let home = try makeHome([".local/bin", ".cargo/bin"])
        defer { cleanup(home) }

        let path = ChildEnvironment.path(existing: "/usr/bin", home: home.path,
                                         systemDirectories: [])
        #expect(path == "/usr/bin:\(home.path)/.local/bin:\(home.path)/.cargo/bin")
    }

    @Test func emptyCompositionLeavesPathUntouched() {
        // Nothing inherited, nothing to add: an empty PATH would mean "current
        // directory only", so the variable must survive as it was.
        let environment = ChildEnvironment.launchEnvironment(
            base: ["PATH": ""],
            home: "/nonexistent-home",
            systemDirectories: []
        )
        #expect(environment["PATH"] == "")
    }

    @Test func launchEnvironmentKeepsEveryOtherVariable() throws {
        let home = try makeHome([".local/bin"])
        defer { cleanup(home) }

        let environment = ChildEnvironment.launchEnvironment(
            base: ["PATH": "/usr/bin", "HOME": "/Users/someone", "MEMORY_X": "1"],
            home: home.path,
            systemDirectories: []
        )
        #expect(environment["HOME"] == "/Users/someone")
        #expect(environment["MEMORY_X"] == "1")
        #expect(environment["PATH"]?.hasSuffix("\(home.path)/.local/bin") == true)
    }

    // MARK: - System list

    @Test func systemPathDirectoriesReadPathsThenPathsDInNameOrder() throws {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent("ChildEnvironmentPaths-\(UUID().uuidString)", isDirectory: true)
        let pathsFile = root.appendingPathComponent("paths")
        let pathsDirectory = root.appendingPathComponent("paths.d", isDirectory: true)
        try FileManager.default.createDirectory(at: pathsDirectory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }

        try "/usr/bin\n/bin\n# a comment\n\n".write(to: pathsFile, atomically: true, encoding: .utf8)
        try "/opt/b\n".write(to: pathsDirectory.appendingPathComponent("20-b"),
                             atomically: true, encoding: .utf8)
        try "/opt/a\n".write(to: pathsDirectory.appendingPathComponent("10-a"),
                             atomically: true, encoding: .utf8)

        // macOS and its tools drop dotfiles from these lists; so do we.
        try "/opt/hidden\n".write(to: pathsDirectory.appendingPathComponent(".hidden"),
                                  atomically: true, encoding: .utf8)

        let directories = ChildEnvironment.systemPathDirectories(
            pathsFile: pathsFile.path,
            pathsDirectory: pathsDirectory.path
        )
        #expect(directories == ["/usr/bin", "/bin", "/opt/a", "/opt/b"])
    }

    @Test func unreadableSystemListYieldsNothing() {
        let directories = ChildEnvironment.systemPathDirectories(
            pathsFile: "/nonexistent/paths",
            pathsDirectory: "/nonexistent/paths.d"
        )
        #expect(directories.isEmpty)
    }
}
