@testable import MemoryConnector
import XCTest

/// Pins the spec requirement "Engine and app log writes are serialized and
/// lossless": the `LogSink` actor writes whole, non-interleaved lines, keeps
/// stdout/stderr framing separate, and stays distinguishable for lifecycle
/// lines — without any unsynchronized shared mutable state.
final class LogSinkTests: XCTestCase {
    private var tempDir: URL!
    private var logURL: URL!

    override func setUpWithError() throws {
        tempDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("log-sink-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: tempDir, withIntermediateDirectories: true)
        logURL = tempDir.appendingPathComponent("sink.log")
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempDir)
    }

    private func makeSink(suppress: Bool = false) -> LogSink {
        LogSink(destination: logURL, shouldSuppress: { suppress })
    }

    private func readLog() throws -> String {
        try String(contentsOf: logURL, encoding: .utf8)
    }

    // MARK: - Line atomicity under concurrency

    func testConcurrentWritersProduceWholeNonInterleavedLines() async throws {
        let sink = makeSink()
        let workers = 50
        let linesPerWorker = 20
        let payload = String(repeating: "z", count: 256)

        await withTaskGroup(of: Void.self) { group in
            for worker in 0 ..< workers {
                group.addTask {
                    for lineIndex in 0 ..< linesPerWorker {
                        let line = "[worker-\(worker)] line=\(lineIndex) \(payload)\n"
                        await sink.append(line: line)
                    }
                }
            }
        }

        let lines = try readLog()
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map(String.init)
            .filter { !$0.isEmpty }

        XCTAssertEqual(lines.count, workers * linesPerWorker,
                       "every line must be written exactly once and whole")
        for line in lines {
            XCTAssertTrue(line.hasPrefix("[worker-") && line.hasSuffix(payload),
                          "interleaved/truncated line: \(line.prefix(80))")
        }
    }

    // MARK: - Line framing across chunk boundaries

    func testLineFramingReassemblesLinesSplitAcrossChunks() async throws {
        let sink = makeSink()
        await sink.append(stdout: Data("hello ".utf8))
        await sink.append(stdout: Data("world\nsecond line\n".utf8))
        await sink.flush()

        let content = try readLog()
        XCTAssertTrue(content.contains("hello world\n"),
                      "line split across chunks must be written whole: \(content)")
        XCTAssertTrue(content.contains("second line\n"))
    }

    func testFlushWritesTrailingUnterminatedLine() async throws {
        let sink = makeSink()
        await sink.append(stdout: Data("no trailing newline".utf8))
        await sink.flush()

        XCTAssertEqual(try readLog(), "no trailing newline")
    }

    // MARK: - Stream separation

    func testStdoutAndStderrLinesDoNotMerge() async throws {
        let sink = makeSink()
        await sink.append(stdout: Data("out-".utf8)) // unterminated stdout chunk
        let lines = await sink.append(stderr: Data("err-line\n".utf8))
        await sink.flush()

        XCTAssertEqual(lines, ["err-line"], "stderr framing returns only complete stderr lines")
        let content = try readLog()
        XCTAssertTrue(content.contains("err-line\n"))
        XCTAssertTrue(content.contains("out-"), "trailing stdout bytes must be flushed")
        XCTAssertFalse(content.contains("out-err-line"), "stdout and stderr must not merge into one line")
    }

    // MARK: - Lifecycle distinguishability

    func testLifecycleLineRemainsDistinguishableFromEngineOutput() async throws {
        let sink = makeSink()
        await sink.append(stdout: Data("engine output one\ntwo\n".utf8))
        await sink.append(line: "[lifecycle] activeAccountDidChange source=signIn account=dev\n")
        await sink.flush()

        let lines = try readLog()
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map(String.init)
        XCTAssertTrue(lines.contains("[lifecycle] activeAccountDidChange source=signIn account=dev"),
                      "lifecycle line must be whole and distinguishable")
        XCTAssertTrue(lines.contains("engine output one"))
        XCTAssertTrue(lines.contains("two"))
    }

    // MARK: - Suppression

    func testSuppressionDropsLifecycleLines() async throws {
        try "sentinel\n".write(to: logURL, atomically: true, encoding: .utf8)
        let sink = makeSink(suppress: true)
        await sink.append(line: "[lifecycle] should-not-appear\n")

        XCTAssertEqual(try readLog(), "sentinel\n")
    }

    func testEngineOutputIsNotSuppressed() async throws {
        // Engine stdout/stderr always writes, even when lifecycle lines are
        // suppressed (the split mirrors the pre-sink behavior).
        let sink = makeSink(suppress: true)
        await sink.append(stdout: Data("engine line\n".utf8))
        await sink.flush()

        XCTAssertTrue(try readLog().contains("engine line\n"))
    }
}
