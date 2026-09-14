// Pure, EventKit-free helpers for the memory-reminders CLI.
//
// Why a separate file: `main.swift` owns all EventKit access and top-level
// execution, which makes it awkward to unit-test. Argument parsing, the JSON
// row shapes, and RFC3339 handling carry the contract the Go connector relies
// on, so they live here where the test target can compile and exercise them
// directly (see project.yml -> MemoryConnectorTests sources).
//
// This file must remain free of EventKit imports and top-level executable
// statements: it is compiled both into the helper binary (swiftc alongside
// main.swift) and into the app's unit-test bundle.

import Foundation

// MARK: - Errors

enum HelperError: Error, CustomStringConvertible, Equatable {
    case invalid(String)

    var description: String {
        switch self {
        case .invalid(let message):
            return message
        }
    }
}

// MARK: - Commands

enum HelperCommand: String, CaseIterable {
    case list
    case lists
    case add
    case update
    case delete
}

let helperUsage = """
usage: memory-reminders <command> [options]
  list   [--list NAME] [--include-completed] [--include-notes]
  lists
  add    --title T [--list NAME] [--due RFC3339] [--notes N]
  update --id ID [--title T] [--due RFC3339 | --clear-due]
                  [--notes N | --clear-notes] [--priority N]
                  [--completed true|false] [--list NAME]
  delete --id ID
"""

// ParsedArguments is the normalized result of parsing a command line. A non-nil
// optional means "the caller supplied this flag"; clearDue/clearNotes are
// explicit so that "leave alone" and "remove" stay distinguishable.
struct ParsedArguments: Equatable {
    var command: HelperCommand
    var listName: String?
    var includeCompleted = false
    var includeNotes = false
    var id: String?
    var title: String?
    var due: Date?
    var clearDue = false
    var notes: String?
    var clearNotes = false
    var priority: Int?
    var completed: Bool?
}

// MARK: - Argument parsing

func parseArguments(_ argv: [String]) throws -> ParsedArguments {
    guard let rawCommand = argv.first else {
        throw HelperError.invalid(helperUsage)
    }
    guard let command = HelperCommand(rawValue: rawCommand) else {
        throw HelperError.invalid("unknown subcommand '\(rawCommand)'\n\(helperUsage)")
    }

    let allowed: Set<String>
    switch command {
    case .list:
        allowed = ["--list", "--include-completed", "--include-notes"]
    case .lists:
        allowed = []
    case .add:
        allowed = ["--title", "--list", "--due", "--notes"]
    case .update:
        allowed = [
            "--id", "--title", "--due", "--clear-due", "--notes",
            "--clear-notes", "--priority", "--completed", "--list",
        ]
    case .delete:
        allowed = ["--id"]
    }

    var parsed = ParsedArguments(command: command)
    var index = 1

    func takeValue(_ flag: String, allowEmpty: Bool = false) throws -> String {
        index += 1
        guard index < argv.count else {
            throw HelperError.invalid("\(flag) requires a value")
        }
        let value = argv[index]
        if value.isEmpty && !allowEmpty {
            throw HelperError.invalid("\(flag) requires a non-empty value")
        }
        return value
    }

    while index < argv.count {
        let argument = argv[index]
        guard allowed.contains(argument) else {
            throw HelperError.invalid("unknown argument for \(command.rawValue): \(argument)\n\(helperUsage)")
        }
        switch argument {
        case "--list":
            parsed.listName = try takeValue(argument)
        case "--include-completed":
            parsed.includeCompleted = true
        case "--include-notes":
            parsed.includeNotes = true
        case "--id":
            parsed.id = try takeValue(argument)
        case "--title":
            parsed.title = try takeValue(argument)
        case "--due":
            let value = try takeValue(argument)
            guard let date = RFC3339().date(from: value) else {
                throw HelperError.invalid(
                    "invalid --due value '\(value)': expected an RFC3339 timestamp (e.g. 2026-09-09T12:00:00Z)"
                )
            }
            parsed.due = date
        case "--clear-due":
            parsed.clearDue = true
        case "--notes":
            parsed.notes = try takeValue(argument, allowEmpty: true)
        case "--clear-notes":
            parsed.clearNotes = true
        case "--priority":
            let value = try takeValue(argument)
            guard let priority = Int(value) else {
                throw HelperError.invalid("invalid --priority value '\(value)': expected an integer")
            }
            parsed.priority = priority
        case "--completed":
            let value = try takeValue(argument)
            switch value {
            case "true":
                parsed.completed = true
            case "false":
                parsed.completed = false
            default:
                throw HelperError.invalid("invalid --completed value '\(value)': expected true or false")
            }
        default:
            throw HelperError.invalid("unknown argument for \(command.rawValue): \(argument)")
        }
        index += 1
    }

    if parsed.due != nil && parsed.clearDue {
        throw HelperError.invalid("--due and --clear-due are mutually exclusive")
    }
    if parsed.notes != nil && parsed.clearNotes {
        throw HelperError.invalid("--notes and --clear-notes are mutually exclusive")
    }

    switch command {
    case .add:
        guard parsed.title != nil else {
            throw HelperError.invalid("add requires --title\n\(helperUsage)")
        }
    case .update, .delete:
        guard parsed.id != nil else {
            throw HelperError.invalid("\(command.rawValue) requires --id\n\(helperUsage)")
        }
    case .list, .lists:
        break
    }

    return parsed
}

// MARK: - RFC3339

// RFC3339 formats and parses internet timestamps without fractional seconds,
// accepting fractional seconds on input. Kept as an instance (not a global) so
// the pure helpers stay concurrency-clean.
final class RFC3339 {
    private let basic: ISO8601DateFormatter
    private let fractional: ISO8601DateFormatter

    init() {
        let basic = ISO8601DateFormatter()
        basic.formatOptions = [.withInternetDateTime]
        self.basic = basic

        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        self.fractional = fractional
    }

    func string(from date: Date) -> String {
        basic.string(from: date)
    }

    func date(from string: String) -> Date? {
        basic.date(from: string) ?? fractional.date(from: string)
    }
}

// MARK: - JSON shapes

// encodeJSON is the single JSON encoder used for every subcommand, so stdout is
// always compact JSON and nothing else.
func encodeJSON<T: Encodable>(_ value: T) throws -> Data {
    let encoder = JSONEncoder()
    encoder.outputFormatting = []
    return try encoder.encode(value)
}

// ReminderRow is the enriched reminder shape emitted by `list`, `add`, and
// `update`. `name`/`due_date` keep their existing spelling; `id`, `list`, and
// `completed` are additive. `due_date` is always present (explicit null when
// absent). `notes` is emitted only when emitNotes is true.
struct ReminderRow: Encodable {
    let id: String
    let name: String
    let list: String
    let dueDate: String?
    let completed: Bool
    let notes: String?
    let emitNotes: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case list
        case dueDate = "due_date"
        case completed
        case notes
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(name, forKey: .name)
        try container.encode(list, forKey: .list)
        if let dueDate {
            try container.encode(dueDate, forKey: .dueDate)
        } else {
            try container.encodeNil(forKey: .dueDate)
        }
        try container.encode(completed, forKey: .completed)
        if emitNotes {
            if let notes {
                try container.encode(notes, forKey: .notes)
            } else {
                try container.encodeNil(forKey: .notes)
            }
        }
    }
}

// ListRow is emitted by the `lists` subcommand: [{"id","name","count"},...].
struct ListRow: Encodable {
    let id: String
    let name: String
    let count: Int
}

// DeletedResult is emitted by the `delete` subcommand: {"id","deleted":true}.
struct DeletedResult: Encodable {
    let id: String
    let deleted: Bool
}
