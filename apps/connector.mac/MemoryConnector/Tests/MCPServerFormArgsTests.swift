@testable import MemoryConnector
import XCTest

/// Row identity for editable MCP argument rows. Each row carries a stable
/// `UUID`, so adding or removing a row preserves the other rows' values and
/// state (index identity would reset them on mutation).
final class MCPServerFormArgsTests: XCTestCase {
    func testAddingAnArgumentPreservesExistingValues() {
        var form = MCPServerForm()
        form.args = [
            MCPServerForm.Arg(value: "--root"),
            MCPServerForm.Arg(value: "/tmp"),
        ]

        form.args.append(MCPServerForm.Arg(value: "--verbose"))

        XCTAssertEqual(form.args.map(\.value), ["--root", "/tmp", "--verbose"])
    }

    func testRemovingAMiddleArgumentKeepsTheOthers() {
        var form = MCPServerForm()
        form.args = [
            MCPServerForm.Arg(value: "a"),
            MCPServerForm.Arg(value: "b"),
            MCPServerForm.Arg(value: "c"),
        ]
        let ids = form.args.map(\.id)

        form.args.removeAll { $0.id == ids[1] }

        XCTAssertEqual(form.args.map(\.value), ["a", "c"])
        XCTAssertEqual(form.args.map(\.id), [ids[0], ids[2]],
                       "the surviving rows keep their own identity")
    }

    func testArgumentIdentityIsStableAcrossAppends() {
        var form = MCPServerForm()
        form.args = [MCPServerForm.Arg(value: "one")]
        let first = form.args[0].id

        form.args.append(MCPServerForm.Arg(value: "two"))

        XCTAssertEqual(form.args.first?.id, first)
        XCTAssertEqual(form.args.first?.value, "one")
    }

    func testDecodingAConfigSeedsIdentifiedArgs() {
        let config = HostedMCPServerConfig(name: "fs",
                                           transport: .stdio,
                                           command: "mcp",
                                           args: ["--a", "--b"])
        let form = MCPServerForm(config: config)

        XCTAssertEqual(form.args.map(\.value), ["--a", "--b"])
        XCTAssertEqual(Set(form.args.map(\.id)).count, 2)
        XCTAssertEqual(form.config().args, ["--a", "--b"])
    }
}
