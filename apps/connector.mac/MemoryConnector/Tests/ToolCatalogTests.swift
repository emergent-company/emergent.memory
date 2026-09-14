import XCTest
@testable import MemoryConnector

final class ToolCatalogTests: XCTestCase {

    func testCatalogMatchesEngineToolIds() {
        // These ids must equal the engine's registered tool names — the app's
        // disabled set is written straight into the engine config.
        let expected = [
            "notes_search", "notes_create",
            "reminders_list", "reminders_lists", "reminders_add",
            "reminders_update", "reminders_delete",
        ]
        XCTAssertEqual(ToolCatalog.tools.map(\.id), expected)
    }

    func testIdsUnique() {
        let ids = ToolCatalog.tools.map(\.id)
        XCTAssertEqual(Set(ids).count, ids.count, "tool ids must be unique")
    }

    func testServiceMapping() {
        let notes = ToolCatalog.tools(for: .notes).map(\.id)
        XCTAssertEqual(notes, ["notes_search", "notes_create"])
        let reminders = ToolCatalog.tools(for: .reminders).map(\.id)
        XCTAssertEqual(reminders, [
            "reminders_list", "reminders_lists", "reminders_add",
            "reminders_update", "reminders_delete",
        ])
    }

    func testLookup() {
        XCTAssertEqual(ToolCatalog.tool(id: "reminders_list")?.service, .reminders)
        XCTAssertEqual(ToolCatalog.tool(id: "reminders_lists")?.service, .reminders)
        XCTAssertEqual(ToolCatalog.tool(id: "reminders_update")?.service, .reminders)
        XCTAssertEqual(ToolCatalog.tool(id: "reminders_delete")?.service, .reminders)
        XCTAssertNil(ToolCatalog.tool(id: "nope"))
    }

    func testDescriptiveFieldsPresent() {
        for tool in ToolCatalog.tools {
            XCTAssertFalse(tool.displayName.isEmpty)
            XCTAssertFalse(tool.summary.isEmpty)
        }
    }
}
