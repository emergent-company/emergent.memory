import XCTest
@testable import MemoryConnector

final class EngineResourceTests: XCTestCase {

    /// The run-script build phase embeds the engine at
    /// Contents/Resources/memory-connector; hosted tests run inside the built
    /// app bundle, so the resource must be visible via Bundle.main. This only
    /// checks presence/executability — it never spawns the engine.
    func testEngineBinaryEmbeddedAndExecutable() throws {
        let url = try XCTUnwrap(
            Bundle.main.url(forResource: "memory-connector", withExtension: nil),
            "memory-connector resource missing from app bundle"
        )
        XCTAssertTrue(
            FileManager.default.isExecutableFile(atPath: url.path),
            "embedded engine not executable at \(url.path)"
        )
    }

    func testDefaultConfigPathPointsAtEngineConfig() {
        XCTAssertEqual(
            EngineManager.defaultConfigPath,
            NSHomeDirectory() + "/.config/memory-connector.yml"
        )
    }
}
