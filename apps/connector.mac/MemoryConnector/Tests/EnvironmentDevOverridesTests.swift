import Foundation
@testable import MemoryConnector
import Testing

/// Dev overrides read from the launch environment.
///
/// Absent, empty, and whitespace-only values all mean "not set" (a shell or an
/// Xcode scheme exporting `KEY=` must not defeat the fallback), and an
/// unparseable URL falls back instead of trapping while `Environment.dev`
/// initialises. Every case goes through `Environment.makeDev(environment:)`, so
/// the process environment is never mutated.
struct EnvironmentDevOverridesTests {
    private let serverFallback = "https://api.example.com"
    private let issuerFallback = "https://zitadel.example.com"
    private let clientIDFallback = "390138928478289930"

    // MARK: Unset

    @Test func unsetValuesUseFallbacks() {
        let dev = Environment.makeDev(environment: [:])

        #expect(dev.id == "dev")
        #expect(dev.name == "Memory Dev")
        #expect(dev.serverURL.absoluteString == serverFallback)
        #expect(dev.issuer.absoluteString == issuerFallback)
        #expect(dev.clientID == clientIDFallback)
    }

    // MARK: Empty

    @Test func emptyValuesUseFallbacks() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "",
            "MEMORY_DEV_ISSUER_URL": "",
            "MEMORY_DEV_CLIENT_ID": "",
        ])

        #expect(dev.serverURL.absoluteString == serverFallback)
        #expect(dev.issuer.absoluteString == issuerFallback)
        #expect(dev.clientID == clientIDFallback)
    }

    // MARK: Whitespace-only

    @Test(arguments: [" ", "\t", "\n", "   \t \n  "])
    func whitespaceOnlyServerURLUsesFallback(value: String) {
        let dev = Environment.makeDev(environment: ["MEMORY_DEV_SERVER_URL": value])

        #expect(dev.serverURL.absoluteString == serverFallback)
    }

    @Test(arguments: [" ", "\t", "\n", "   \t \n  "])
    func whitespaceOnlyIssuerURLUsesFallback(value: String) {
        let dev = Environment.makeDev(environment: ["MEMORY_DEV_ISSUER_URL": value])

        #expect(dev.issuer.absoluteString == issuerFallback)
    }

    @Test(arguments: [" ", "\t", "\n", "   \t \n  "])
    func whitespaceOnlyClientIDUsesFallback(value: String) {
        let dev = Environment.makeDev(environment: ["MEMORY_DEV_CLIENT_ID": value])

        #expect(dev.clientID == clientIDFallback)
    }

    // MARK: Configured

    @Test func configuredValuesAreUsed() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "https://memory.dev.example.test",
            "MEMORY_DEV_ISSUER_URL": "https://auth.dev.example.test",
            "MEMORY_DEV_CLIENT_ID": "12345",
        ])

        #expect(dev.serverURL.absoluteString == "https://memory.dev.example.test")
        #expect(dev.issuer.absoluteString == "https://auth.dev.example.test")
        #expect(dev.clientID == "12345")
    }

    @Test func surroundingWhitespaceIsTrimmed() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "  https://memory.dev.example.test\n",
            "MEMORY_DEV_ISSUER_URL": "\thttps://auth.dev.example.test  ",
            "MEMORY_DEV_CLIENT_ID": " 12345 ",
        ])

        #expect(dev.serverURL.absoluteString == "https://memory.dev.example.test")
        #expect(dev.issuer.absoluteString == "https://auth.dev.example.test")
        #expect(dev.clientID == "12345")
    }

    // MARK: Malformed URLs

    /// Each malformed shape must fall back rather than trap. The list covers
    /// both failure modes: `http://[` is an unclosed IPv6 literal, so
    /// `URL(string:)` returns nil — the exact input that used to hit the
    /// `URL(string:)!` fatal error — while the others parse as relative or
    /// host-less URLs (`api.example.com`, `https://`) and are caught only by
    /// the stricter scheme + host check.
    @Test(arguments: [
        "http://[",
        "not a url",
        "://missing-scheme",
        "api.example.com",
        "ftp://api.example.com",
        "https://",
    ])
    func malformedServerURLUsesFallback(value: String) {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": value,
            "MEMORY_DEV_ISSUER_URL": "https://auth.dev.example.test",
        ])

        #expect(dev.serverURL.absoluteString == serverFallback)
        // A bad server URL must not disturb a good issuer.
        #expect(dev.issuer.absoluteString == "https://auth.dev.example.test")
    }

    @Test(arguments: [
        "http://[",
        "not a url",
        "://missing-scheme",
        "auth.example.com",
        "ftp://auth.example.com",
        "https://",
    ])
    func malformedIssuerURLUsesFallback(value: String) {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "https://memory.dev.example.test",
            "MEMORY_DEV_ISSUER_URL": value,
        ])

        #expect(dev.issuer.absoluteString == issuerFallback)
        // A bad issuer URL must not disturb a good server URL.
        #expect(dev.serverURL.absoluteString == "https://memory.dev.example.test")
    }

    /// A malformed value is as inert as an unset one: the built-in still
    /// initialises, and `Environment.all` keeps both entries.
    @Test func fullyMalformedEnvironmentStillYieldsDevEntry() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "%%%",
            "MEMORY_DEV_ISSUER_URL": "???",
            "MEMORY_DEV_CLIENT_ID": " ",
        ])

        #expect(dev.serverURL.absoluteString == serverFallback)
        #expect(dev.issuer.absoluteString == issuerFallback)
        #expect(dev.clientID == clientIDFallback)
    }

    // MARK: Built-in

    /// Whatever the launching shell exported, accessing the real built-in must
    /// not trap and must expose usable URLs.
    @Test func builtInDevAlwaysHasUsableURLs() {
        #expect(Environment.dev.serverURL.host?.isEmpty == false)
        #expect(Environment.dev.issuer.host?.isEmpty == false)
        #expect(!Environment.dev.clientID.isEmpty)
        #expect(Environment.all.map(\.id) == ["prod", "dev"])
    }
}
