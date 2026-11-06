import Foundation
@testable import Memory
import Testing

/// The response size is reported the way the web console reports it, which
/// measures `JSON.stringify(value, null, 2)` — not `displayText` and not
/// Foundation's pretty printer (`"key" : value`, 17-digit doubles).
struct ChatUIWireTextTests {
    @Test func aStringMeasuresAsItsOwnText() {
        #expect(ChatUIPropValue.string("https://example.com/a/b").wireText == "https://example.com/a/b")
    }

    @Test func anObjectIsTheWebsPrettyJSON() {
        let value = ChatUIPropValue.object([
            "url": .string("https://example.com/a/b"),
            "n": .number(1),
            "ratio": .number(1.5),
            "ok": .bool(true),
            "missing": .null,
            "list": .array([.number(1), .number(2)]),
        ])
        #expect(
            value.wireText == """
            {
              "list": [
                1,
                2
              ],
              "missing": null,
              "n": 1,
              "ok": true,
              "ratio": 1.5,
              "url": "https://example.com/a/b"
            }
            """
        )
    }

    @Test func anArrayUsesTwoSpaceIndent() {
        #expect(
            ChatUIPropValue.array([.string("a"), .array([.bool(false)])]).wireText == """
            [
              "a",
              [
                false
              ]
            ]
            """
        )
    }

    @Test func emptyContainersStayOnOneLine() {
        #expect(ChatUIPropValue.array([]).wireText == "[]")
        #expect(ChatUIPropValue.object([:]).wireText == "{}")
    }

    /// The numbers `JSON.stringify` would write. Foundation's encoder prints
    /// many of these with 17 digits (`0.10000000000000001`) or a padded
    /// exponent (`1e-07`), which shifts the reported size.
    @Test func numbersFollowECMAScript() {
        let cases: [(Double, String)] = [
            (1, "1"),
            (1.5, "1.5"),
            (0, "0"),
            (-0.5, "-0.5"),
            (0.1, "0.1"),
            (0.000001, "0.000001"),
            (0.0000012, "0.0000012"),
            (0.0000001, "1e-7"),
            (1e20, "100000000000000000000"),
            (1e21, "1e+21"),
            (12_345_678_901_234_567_890.0, "12345678901234567000"),
        ]
        for (value, expected) in cases {
            #expect(ChatUIPropValue.number(value).wireText == expected, "number \(value)")
        }
    }

    @Test func quotesAndControlsAreEscapedButSlashesAreNot() {
        let text = ChatUIPropValue.array([.string("a/b \"q\"\n\t\u{1}")])
        #expect(text.wireText == "[\n  \"a/b \\\"q\\\"\\n\\t\\u0001\"\n]")
    }

    /// A tool result's size is the bytes of that text, so the fix reaches the
    /// number the opened step shows.
    @Test func theSizeCountsTheSerializedBytes() {
        let value = ChatUIPropValue.object(["url": .string("https://example.com/a/b")])
        #expect(ToolResultSize.byteCount(of: value.wireText) == 38)
    }
}
