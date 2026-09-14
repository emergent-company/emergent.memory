import Foundation

extension Notification.Name {
    /// Posted after the connection config is replaced (e.g. QR setup) so
    /// long-lived owners (the session controller, the settings draft) rebuild
    /// from fresh UserDefaults values.
    static let memoryConfigChanged = Notification.Name("memoryConfigChanged")
}

/// Runtime configuration for the Memory iOS client.
///
/// All values are backed by `UserDefaults` (with build-time defaults) so the
/// connection settings can be changed without recompiling. The app reads the
/// configuration once, when `MemorySessionController` is created; after
/// changing a value the settings screen must recreate the controller (or
/// relaunch the app) for the new value to take effect.
///
/// # Stable API for the settings screen (OpenSpec task 3.2)
///
/// Bind the settings UI to these properties and their corresponding
/// `UserDefaults` keys (`*Key` static constants). Property names are stable;
/// do not rename them.
///
/// | Property                 | UserDefaults key                 | Default                            |
/// |--------------------------|----------------------------------|------------------------------------|
/// | `serverURL`              | `alfred.serverURL`               | `ws://100.69.175.118:7880`         |
/// | `agentName`              | `alfred.agentName`               | `memory-google-rt`                 |
/// | `tokenEndpoint`          | `alfred.tokenEndpoint`           | `http://100.69.175.118:8080/api/token` |
/// | `apiKey`                 | `alfred.apiKey`                  | `""`                               |
/// | `apiBaseURL`             | `alfred.apiBaseURL`              | `http://100.69.175.118:8080`       |
/// | `exitKeywords`           | `alfred.exitKeywords`            | `["goodbye"]`                      |
/// | `agentConnectTimeout`    | `alfred.agentConnectTimeout`     | `20` (seconds)                     |
/// | `chimeSoundID`           | `alfred.chimeSoundID`            | `1057` (system sound "Tink")       |
///
/// `participantIdentity` is generated once per install and persisted; it is
/// not meant for the settings UI.
struct MemoryConfig: Sendable, Equatable {
    // MARK: - UserDefaults keys (stable, public)

    /// `ws://` URL of the self-hosted LiveKit server.
    static let serverURLKey = "alfred.serverURL"
    /// Name of the agent worker to dispatch (`memory-google-rt`).
    static let agentNameKey = "alfred.agentName"
    /// HTTP(S) URL of the Memory token endpoint (`POST /api/token`).
    static let tokenEndpointKey = "alfred.tokenEndpoint"
    /// Shared secret sent as the `X-API-Key` header to the token endpoint.
    static let apiKeyKey = "alfred.apiKey"
    /// Base URL of the Memory gateway (`/api/agents` CRUD, port 8080).
    static let apiBaseURLKey = "alfred.apiBaseURL"
    /// User-transcript phrases that end the session (case-insensitive substring match).
    static let exitKeywordsKey = "alfred.exitKeywords"
    /// Seconds to wait for the agent to join before failing.
    static let agentConnectTimeoutKey = "alfred.agentConnectTimeout"
    /// System sound ID played on `lk.agent.ready`.
    static let chimeSoundIDKey = "alfred.chimeSoundID"
    /// `"client"` = speak replies with AVSpeechSynthesizer; `"server"` = worker streams its own TTS audio.
    static let ttsStrategyKey = "alfred.ttsStrategy"
    /// Stable per-install participant identity sent to the token endpoint.
    static let participantIdentityKey = "alfred.participantIdentity"

    // MARK: - Build-time defaults

    static let defaultServerURL = "ws://100.69.175.118:7880"
    static let defaultAgentName = "memory-google-rt"
    static let defaultTokenEndpoint = "http://100.69.175.118:8080/api/token"
    static let defaultAPIKey = ""
    static let defaultAPIBaseURL = "http://100.69.175.118:8080"
    static let defaultExitKeywords = ["goodbye"]
    static let defaultAgentConnectTimeout: TimeInterval = 20
    static let defaultChimeSoundID: UInt32 = 1057
    static let defaultTTSStrategy = "server"

    // MARK: - Values

    var serverURL: String
    var agentName: String
    var tokenEndpoint: String
    var apiKey: String
    var apiBaseURL: String
    var exitKeywords: [String]
    var agentConnectTimeout: TimeInterval
    var chimeSoundID: UInt32
    /// `"client"` = app speaks replies locally (AVSpeechSynthesizer);
    /// `"server"` = worker streams its own TTS audio.
    var ttsStrategy: String
    /// Stable per-install participant identity (generated once, persisted).
    var participantIdentity: String

    /// Loads the configuration from `UserDefaults`, falling back to the
    /// build-time defaults for unset keys.
    init(defaults: UserDefaults = .standard) {
        serverURL = defaults.string(forKey: Self.serverURLKey) ?? Self.defaultServerURL
        agentName = defaults.string(forKey: Self.agentNameKey) ?? Self.defaultAgentName
        tokenEndpoint = defaults.string(forKey: Self.tokenEndpointKey) ?? Self.defaultTokenEndpoint
        apiKey = defaults.string(forKey: Self.apiKeyKey) ?? Self.defaultAPIKey
        apiBaseURL = defaults.string(forKey: Self.apiBaseURLKey) ?? Self.defaultAPIBaseURL
        exitKeywords = defaults.stringArray(forKey: Self.exitKeywordsKey) ?? Self.defaultExitKeywords
        let storedTimeout = defaults.double(forKey: Self.agentConnectTimeoutKey)
        agentConnectTimeout = storedTimeout > 0 ? storedTimeout : Self.defaultAgentConnectTimeout
        let storedChime = defaults.integer(forKey: Self.chimeSoundIDKey)
        chimeSoundID = storedChime > 0 ? UInt32(storedChime) : Self.defaultChimeSoundID
        ttsStrategy = defaults.string(forKey: Self.ttsStrategyKey) ?? Self.defaultTTSStrategy

        if let storedIdentity = defaults.string(forKey: Self.participantIdentityKey), !storedIdentity.isEmpty {
            participantIdentity = storedIdentity
        } else {
            participantIdentity = "memory-ios-\(UUID().uuidString)"
            defaults.set(participantIdentity, forKey: Self.participantIdentityKey)
        }
    }

    /// Persists the current values to `UserDefaults`.
    func save(defaults: UserDefaults = .standard) {
        defaults.set(serverURL, forKey: Self.serverURLKey)
        defaults.set(agentName, forKey: Self.agentNameKey)
        defaults.set(tokenEndpoint, forKey: Self.tokenEndpointKey)
        defaults.set(apiKey, forKey: Self.apiKeyKey)
        defaults.set(apiBaseURL, forKey: Self.apiBaseURLKey)
        defaults.set(exitKeywords, forKey: Self.exitKeywordsKey)
        defaults.set(agentConnectTimeout, forKey: Self.agentConnectTimeoutKey)
        defaults.set(Int(chimeSoundID), forKey: Self.chimeSoundIDKey)
        defaults.set(ttsStrategy, forKey: Self.ttsStrategyKey)
        defaults.set(participantIdentity, forKey: Self.participantIdentityKey)
    }

    /// Restores every key to its build-time default and persists the result.
    static func reset(defaults: UserDefaults = .standard) {
        defaults.removeObject(forKey: serverURLKey)
        defaults.removeObject(forKey: agentNameKey)
        defaults.removeObject(forKey: tokenEndpointKey)
        defaults.removeObject(forKey: apiKeyKey)
        defaults.removeObject(forKey: apiBaseURLKey)
        defaults.removeObject(forKey: exitKeywordsKey)
        defaults.removeObject(forKey: agentConnectTimeoutKey)
        defaults.removeObject(forKey: chimeSoundIDKey)
        defaults.removeObject(forKey: ttsStrategyKey)
        defaults.removeObject(forKey: participantIdentityKey)
    }
}
