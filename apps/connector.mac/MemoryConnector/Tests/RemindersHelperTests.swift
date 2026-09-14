import Foundation
import XCTest

// Tests for the pure helpers compiled from
// RemindersHelper/RemindersHelperCore.swift. These run without an EventKit
// store: they pin the CLI argument contract and the JSON shapes the Go
// connector depends on.
final class RemindersHelperTests: XCTestCase {

    // MARK: - Argument parsing

    func testListDefaults() throws {
        let parsed = try parseArguments(["list"])
        XCTAssertEqual(parsed.command, .list)
        XCTAssertNil(parsed.listName)
        XCTAssertFalse(parsed.includeCompleted)
        XCTAssertFalse(parsed.includeNotes)
    }

    func testListFlags() throws {
        let parsed = try parseArguments([
            "list", "--list", "Errands", "--include-completed", "--include-notes",
        ])
        XCTAssertEqual(parsed.listName, "Errands")
        XCTAssertTrue(parsed.includeCompleted)
        XCTAssertTrue(parsed.includeNotes)
    }

    func testListsTakesNoArguments() throws {
        let parsed = try parseArguments(["lists"])
        XCTAssertEqual(parsed.command, .lists)
    }

    func testListsRejectsArguments() {
        XCTAssertThrowsError(try parseArguments(["lists", "--list", "x"]))
    }

    func testAddParsesAllOptions() throws {
        let parsed = try parseArguments([
            "add", "--title", "Buy milk", "--list", "Errands",
            "--due", "2026-09-09T12:00:00Z", "--notes", "semi skimmed",
        ])
        XCTAssertEqual(parsed.command, .add)
        XCTAssertEqual(parsed.title, "Buy milk")
        XCTAssertEqual(parsed.listName, "Errands")
        XCTAssertEqual(parsed.notes, "semi skimmed")
        XCTAssertEqual(parsed.due, RFC3339().date(from: "2026-09-09T12:00:00Z"))
    }

    func testAddAllowsEmptyNotes() throws {
        let parsed = try parseArguments(["add", "--title", "T", "--notes", ""])
        XCTAssertEqual(parsed.notes, "")
    }

    func testAddRequiresTitle() {
        XCTAssertThrowsError(try parseArguments(["add", "--list", "Errands"]))
    }

    func testAddRejectsEmptyTitle() {
        XCTAssertThrowsError(try parseArguments(["add", "--title", "", "--list", "Errands"]))
    }

    func testUpdateParsesAllOptions() throws {
        let parsed = try parseArguments([
            "update", "--id", "ABC", "--title", "New",
            "--due", "2026-09-09T12:00:00Z", "--notes", "note",
            "--priority", "5", "--completed", "true", "--list", "Work",
        ])
        XCTAssertEqual(parsed.command, .update)
        XCTAssertEqual(parsed.id, "ABC")
        XCTAssertEqual(parsed.title, "New")
        XCTAssertEqual(parsed.notes, "note")
        XCTAssertEqual(parsed.priority, 5)
        XCTAssertEqual(parsed.completed, true)
        XCTAssertEqual(parsed.listName, "Work")
        XCTAssertEqual(parsed.due, RFC3339().date(from: "2026-09-09T12:00:00Z"))
    }

    func testUpdateParsesCompletedFalse() throws {
        let parsed = try parseArguments(["update", "--id", "ABC", "--completed", "false"])
        XCTAssertEqual(parsed.completed, false)
    }

    func testUpdateClearFlags() throws {
        let parsed = try parseArguments(["update", "--id", "ABC", "--clear-due", "--clear-notes"])
        XCTAssertTrue(parsed.clearDue)
        XCTAssertTrue(parsed.clearNotes)
        XCTAssertNil(parsed.due)
        XCTAssertNil(parsed.notes)
    }

    func testUpdateRejectsDueAndClearDue() {
        XCTAssertThrowsError(try parseArguments([
            "update", "--id", "ABC", "--due", "2026-09-09T12:00:00Z", "--clear-due",
        ]))
    }

    func testUpdateRejectsNotesAndClearNotes() {
        XCTAssertThrowsError(try parseArguments([
            "update", "--id", "ABC", "--notes", "x", "--clear-notes",
        ]))
    }

    func testUpdateRejectsInvalidDue() {
        XCTAssertThrowsError(try parseArguments(["update", "--id", "ABC", "--due", "not-a-date"]))
    }

    func testUpdateRejectsInvalidCompleted() {
        XCTAssertThrowsError(try parseArguments(["update", "--id", "ABC", "--completed", "yes"]))
    }

    func testUpdateRejectsInvalidPriority() {
        XCTAssertThrowsError(try parseArguments(["update", "--id", "ABC", "--priority", "high"]))
    }

    func testUpdateRequiresId() {
        XCTAssertThrowsError(try parseArguments(["update", "--title", "x"]))
    }

    func testDeleteRejectsNonIdArgument() {
        XCTAssertThrowsError(try parseArguments(["delete", "--title", "x"]))
    }

    func testDeleteParsesId() throws {
        let parsed = try parseArguments(["delete", "--id", "ABC"])
        XCTAssertEqual(parsed.command, .delete)
        XCTAssertEqual(parsed.id, "ABC")
    }

    func testDeleteRequiresId() {
        XCTAssertThrowsError(try parseArguments(["delete"]))
    }

    func testNoArgumentsIsUsageError() {
        XCTAssertThrowsError(try parseArguments([]))
    }

    func testUnknownSubcommand() {
        XCTAssertThrowsError(try parseArguments(["frobnicate"]))
    }

    func testUnknownArgument() {
        XCTAssertThrowsError(try parseArguments(["list", "--bogus"]))
    }

    func testEmptyFlagValueRejected() {
        XCTAssertThrowsError(try parseArguments(["list", "--list", ""]))
    }

    // MARK: - Row encoding

    func testListRowOmitsNotesByDefault() throws {
        let row = ReminderRow(
            id: "R1", name: "Buy milk", list: "Errands",
            dueDate: nil, completed: false, notes: "secret", emitNotes: false
        )
        let object = try jsonObject(row)
        XCTAssertEqual(object["id"] as? String, "R1")
        XCTAssertEqual(object["name"] as? String, "Buy milk")
        XCTAssertEqual(object["list"] as? String, "Errands")
        XCTAssertEqual(object["completed"] as? Bool, false)
        XCTAssertTrue(object.keys.contains("due_date"))
        XCTAssertTrue(object["due_date"] is NSNull)
        XCTAssertNil(object["notes"])
    }

    func testListRowIncludesNullNotesWhenRequested() throws {
        let row = ReminderRow(
            id: "R1", name: "n", list: "l",
            dueDate: nil, completed: true, notes: nil, emitNotes: true
        )
        let object = try jsonObject(row)
        XCTAssertTrue(object.keys.contains("notes"))
        XCTAssertTrue(object["notes"] is NSNull)
    }

    func testListRowIncludesNotesValue() throws {
        let row = ReminderRow(
            id: "R1", name: "n", list: "l",
            dueDate: "2026-09-09T12:00:00Z", completed: false,
            notes: "hello", emitNotes: true
        )
        let object = try jsonObject(row)
        XCTAssertEqual(object["notes"] as? String, "hello")
        XCTAssertEqual(object["due_date"] as? String, "2026-09-09T12:00:00Z")
    }

    func testReminderRowsEncodeAsArray() throws {
        let rows = [
            ReminderRow(id: "R1", name: "a", list: "l", dueDate: nil, completed: false, notes: nil, emitNotes: false),
            ReminderRow(id: "R2", name: "b", list: "l", dueDate: nil, completed: true, notes: nil, emitNotes: false),
        ]
        let data = try encodeJSON(rows)
        let array = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [[String: Any]])
        XCTAssertEqual(array.count, 2)
        XCTAssertEqual(array.compactMap { $0["id"] as? String }, ["R1", "R2"])
    }

    func testListRowEncoding() throws {
        let object = try jsonObject(ListRow(id: "CAL1", name: "Errands", count: 3))
        XCTAssertEqual(object["id"] as? String, "CAL1")
        XCTAssertEqual(object["name"] as? String, "Errands")
        XCTAssertEqual(object["count"] as? Int, 3)
    }

    func testDeletedResultEncoding() throws {
        let object = try jsonObject(DeletedResult(id: "R1", deleted: true))
        XCTAssertEqual(object["id"] as? String, "R1")
        XCTAssertEqual(object["deleted"] as? Bool, true)
    }

    // MARK: - RFC3339

    func testRFC3339RoundTrip() throws {
        let formatter = RFC3339()
        let date = try XCTUnwrap(formatter.date(from: "2026-09-09T12:00:00Z"))
        XCTAssertEqual(formatter.string(from: date), "2026-09-09T12:00:00Z")
    }

    func testRFC3339AcceptsOffset() throws {
        let formatter = RFC3339()
        let date = try XCTUnwrap(formatter.date(from: "2026-09-09T14:00:00+02:00"))
        XCTAssertEqual(formatter.string(from: date), "2026-09-09T12:00:00Z")
    }

    func testRFC3339AcceptsFractionalSeconds() throws {
        let formatter = RFC3339()
        let date = try XCTUnwrap(formatter.date(from: "2026-09-09T12:00:00.500Z"))
        XCTAssertEqual(formatter.string(from: date), "2026-09-09T12:00:00Z")
    }

    func testRFC3339RejectsGarbage() {
        XCTAssertNil(RFC3339().date(from: "yesterday"))
    }

    // MARK: - Support

    private func jsonObject<T: Encodable>(_ value: T) throws -> [String: Any] {
        let data = try encodeJSON(value)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }
}
