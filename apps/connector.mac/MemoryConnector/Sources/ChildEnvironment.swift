import Foundation

/// Environment handed to the engine child (`memory-connector relay`).
///
/// The child inherits this app's environment, and a Finder-launched app inherits
/// launchd's, not a login shell's: `~/.zprofile`, `~/.zshrc` and
/// `eval "$(brew shellenv)"` are never read. Tools a developer installs into
/// their home directory — `uv`/`uvx` (`~/.local/bin`), cargo, bun, volta, nix —
/// are therefore invisible to the child, and a stdio MCP server configured as
/// `uvx ha-mcp` fails with `exec: "uvx": executable file not found in $PATH`
/// even though the same command works in Terminal.
///
/// Shell startup files cannot be read reliably from a GUI app (they may prompt,
/// print, take seconds, or assume an interactive terminal), so the child's PATH
/// is composed from *locations* instead:
///
/// 1. the app's own PATH first, in its own order — blank entries dropped and
///    duplicates collapsed, otherwise untouched;
/// 2. the system list (`/etc/paths` + `/etc/paths.d/*`), which is what macOS's
///    `path_helper` gives login shells;
/// 3. the well-known per-user bin directories that exist on disk.
///
/// Everything appended comes after what the app already had, so a tool installed
/// system-wide still wins over a home-directory copy — this widens the search
/// path, it does not re-prioritise it. A configured MCP server command may also
/// be an absolute path, which works regardless of this list.
enum ChildEnvironment {
    /// Home-relative directories where user-installed CLI tools land, appended
    /// in this order. The order is part of the contract only among themselves —
    /// all of them come after the system list.
    static let userBinDirectories = [
        ".local/bin",
        ".local/share/memory/bin",
        ".cargo/bin",
        ".bun/bin",
        ".volta/bin",
        ".nix-profile/bin",
    ]

    /// The environment for the engine child: the app's environment with `PATH`
    /// widened by `path(existing:home:systemDirectories:fileManager:)`.
    static func launchEnvironment(
        base: [String: String] = ProcessInfo.processInfo.environment,
        home: String = NSHomeDirectory(),
        systemDirectories: [String] = systemPathDirectories(),
        fileManager: FileManager = .default
    ) -> [String: String] {
        var environment = base
        let widened = path(existing: base["PATH"],
                           home: home,
                           systemDirectories: systemDirectories,
                           fileManager: fileManager)
        // Never blank a PATH that had nothing to widen: an empty `PATH` means
        // "current directory only" to exec, which is worse than inheriting.
        if !widened.isEmpty {
            environment["PATH"] = widened
        }
        return environment
    }

    /// The widened PATH: preserved entries first, then the system and per-user
    /// directories, de-duplicated, empty entries dropped.
    ///
    /// Only directories that exist are appended, so a child never carries a PATH
    /// full of dead entries that make "command not found" reports misleading.
    static func path(
        existing: String?,
        home: String = NSHomeDirectory(),
        systemDirectories: [String] = systemPathDirectories(),
        fileManager: FileManager = .default
    ) -> String {
        var seen = Set<String>()
        var directories: [String] = []

        func append(_ directory: String) {
            let trimmed = directory.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty, seen.insert(trimmed).inserted else { return }
            directories.append(trimmed)
        }

        (existing ?? "").split(separator: ":").forEach { append(String($0)) }
        systemDirectories.forEach { append($0) }
        for relative in userBinDirectories {
            let absolute = (home as NSString).appendingPathComponent(relative)
            guard fileManager.fileExists(atPath: absolute) else { continue }
            append(absolute)
        }
        return directories.joined(separator: ":")
    }

    /// Directories listed by `/etc/paths` and `/etc/paths.d/*`, in file order —
    /// the same list `path_helper` feeds to login shells. Unreadable files are
    /// skipped: this is a widening of PATH, never a reason to fail a launch.
    static func systemPathDirectories(
        pathsFile: String = "/etc/paths",
        pathsDirectory: String = "/etc/paths.d",
        fileManager: FileManager = .default
    ) -> [String] {
        var directories: [String] = []
        func appendEntries(ofFileAt path: String) {
            guard let contents = try? String(contentsOfFile: path, encoding: .utf8) else { return }
            for line in contents.split(separator: "\n") {
                let entry = line.trimmingCharacters(in: .whitespaces)
                guard !entry.isEmpty, !entry.hasPrefix("#") else { continue }
                directories.append(entry)
            }
        }

        appendEntries(ofFileAt: pathsFile)
        let extra = (try? fileManager.contentsOfDirectory(atPath: pathsDirectory)) ?? []
        for name in extra.sorted() where !name.hasPrefix(".") {
            appendEntries(ofFileAt: (pathsDirectory as NSString).appendingPathComponent(name))
        }
        return directories
    }
}
