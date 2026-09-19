import Combine
import Foundation
// Sparkle's `SPUUpdater` is not `Sendable` and its KVO surface predates Swift 6
// concurrency annotations. `@preconcurrency` downgrades those Sendable checks
// to warnings; the delegate protocol itself *is* `NS_SWIFT_UI_ACTOR`, so its
// callbacks are already main-actor isolated and line up with this type.
@preconcurrency import Sparkle

/// Observable wrapper around Sparkle's `SPUStandardUpdaterController`.
///
/// The About page binds to this for its version line, "Check for Updates…"
/// button, automatic-check toggle, and last-outcome status. The controller is
/// only ever created and started on a **release build with a valid feed URL and
/// EdDSA public key**; every other build (Debug, unit tests, a keyless or
/// placeholder build) exposes `.unavailable` and never starts Sparkle.
///
/// Starting the updater is deliberately gated: `SPUStandardUpdaterController`
/// initialised with `startingUpdater: true` can surface a fatal configuration
/// error (older Sparkle releases called `abort()`) when the bundle is
/// misconfigured, so this type constructs with `startingUpdater: false` and
/// calls `startUpdater()` itself only after validating the bundle.
@MainActor
final class UpdaterModel: NSObject, ObservableObject {

    /// The last known outcome of an update check, used to drive the status line.
    ///
    /// `.unavailable` means the updater was never started (Debug build, no
    /// feed/public key, or placeholder key) — distinct from `.failed`, which is
    /// a real check that errored and must not read as "up to date".
    enum State: Equatable {
        case unavailable
        case idle
        case checking
        case upToDate
        case updateAvailable(version: String)
        case installing(version: String)
        case failed(message: String)
    }

    /// Human-readable outcome of the most recent update check.
    @Published private(set) var state: State = .unavailable
    /// Mirrors `SPUUpdater.canCheckForUpdates`; false while a session is running
    /// and on builds where the updater was not started.
    @Published private(set) var canCheckForUpdates = false
    /// Mirrors Sparkle's own persisted automatic-check preference. Writing it
    /// forwards to the framework, which persists it across launches.
    @Published var automaticallyChecksForUpdates = false {
        didSet {
            guard oldValue != automaticallyChecksForUpdates else { return }
            guard let updater = controller?.updater else { return }
            guard updater.automaticallyChecksForUpdates != automaticallyChecksForUpdates else { return }
            updater.automaticallyChecksForUpdates = automaticallyChecksForUpdates
        }
    }

    private(set) var controller: SPUStandardUpdaterController?
    private var cancellables = Set<AnyCancellable>()

    /// True when Sparkle was actually started, i.e. the manual/automatic
    /// controls have something to drive.
    var isUpdaterAvailable: Bool { controller != nil }

    /// Whether an update was found by the most recent check.
    var updateAvailable: Bool {
        if case .updateAvailable = state { return true }
        return false
    }

    /// The marketing version of the most recently found update, if any.
    var latestVersion: String? {
        switch state {
        case .updateAvailable(let version), .installing(let version): return version
        default: return nil
        }
    }

    /// Caption for the status line: up to date / checking / available / failed.
    var statusText: String {
        switch state {
        case .unavailable:
            return "Update checks are unavailable in this build."
        case .idle:
            return "Not checked yet."
        case .checking:
            return "Checking for updates…"
        case .upToDate:
            return "You're up to date."
        case .updateAvailable(let version):
            return "Update available: \(version)"
        case .installing(let version):
            return "Installing \(version)…"
        case .failed(let message):
            return message.isEmpty ? "Update check failed." : "Update check failed: \(message)"
        }
    }

    override init() {
        super.init()

        #if DEBUG
        // Development builds must never check: there is no trusted feed, and a
        // misconfigured bundle must not be able to start (or alert) Sparkle.
        state = .unavailable
        #else
        guard Self.configuredFeedURL() != nil, Self.configuredPublicKey() != nil else {
            // Missing or placeholder feed/key: do not start the updater. This is
            // the guard that keeps a bad build from aborting at launch.
            state = .unavailable
            return
        }

        let controller = SPUStandardUpdaterController(
            startingUpdater: false,
            updaterDelegate: self,
            userDriverDelegate: nil)
        self.controller = controller

        observe(controller.updater)

        // Spec floor: automatic checks run no more frequently than once per hour.
        if controller.updater.updateCheckInterval < Self.minimumAutomaticCheckInterval {
            controller.updater.updateCheckInterval = Self.minimumAutomaticCheckInterval
        }

        controller.startUpdater()
        canCheckForUpdates = controller.updater.canCheckForUpdates
        automaticallyChecksForUpdates = controller.updater.automaticallyChecksForUpdates
        state = .idle
        #endif
    }

    /// Starts a user-initiated check and a progress dialog. No-op until Sparkle
    /// reports it can check (mirrors the framework's menu-item validation).
    func checkForUpdates() {
        guard let updater = controller?.updater, updater.canCheckForUpdates else { return }
        state = .checking
        updater.checkForUpdates()
    }

    // MARK: - Bundle configuration

    /// The automatic-check floor required by `mac-connector-update`.
    private static let minimumAutomaticCheckInterval: TimeInterval = 60 * 60

    /// Reads and validates `SUFeedURL`; only an HTTPS feed is accepted.
    private static func configuredFeedURL() -> URL? {
        guard let raw = Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") as? String else {
            return nil
        }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              scheme == "https" else {
            return nil
        }
        return url
    }

    /// Reads and validates `SUPublicEDKey`. A missing, placeholder, or
    /// non-32-byte EdDSA key is treated as "not configured" so Sparkle is never
    /// started against a key that would make it fail.
    private static func configuredPublicKey() -> String? {
        guard let raw = Bundle.main.object(forInfoDictionaryKey: "SUPublicEDKey") as? String else {
            return nil
        }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }

        let lowered = trimmed.lowercased()
        let placeholders = ["placeholder", "replace", "changeme", "change_me", "yourkey", "todo", "<", ">"]
        guard !placeholders.contains(where: { lowered.contains($0) }) else { return nil }

        // Sparkle's EdDSA public key is a 32-byte value, base64-encoded.
        guard let data = Data(base64Encoded: trimmed), data.count == 32 else { return nil }
        return trimmed
    }

    // MARK: - KVO bridging

    /// Bridges Sparkle's KVO-backed properties into published state. The sink
    /// closures inherit this type's main-actor isolation (they are non-Sendable),
    /// which is exactly the thread Sparkle mutates on.
    private func observe(_ updater: SPUUpdater) {
        updater.publisher(for: \.canCheckForUpdates)
            .receive(on: RunLoop.main)
            .sink { [weak self] value in
                self?.canCheckForUpdates = value
            }
            .store(in: &cancellables)

        updater.publisher(for: \.automaticallyChecksForUpdates)
            .receive(on: RunLoop.main)
            .sink { [weak self] value in
                guard let self, self.automaticallyChecksForUpdates != value else { return }
                self.automaticallyChecksForUpdates = value
            }
            .store(in: &cancellables)
    }

    // MARK: - Outcome mapping

    /// Maps a completed update cycle onto the published state.
    ///
    /// Sparkle reports "no update found", "user cancelled the install", and
    /// "install deferred to quit" through the same `error` channel as real
    /// failures; those are *benign* completions and must not be shown as a
    /// failed check. A genuine error is always surfaced as `.failed` — the spec
    /// requires a failed check to be distinguishable from "up to date".
    private func applyCompletion(error: Error?) {
        // Keep the button's enabled state honest even if KVO lags.
        canCheckForUpdates = controller?.updater.canCheckForUpdates ?? false

        if let error, !Self.isBenignCompletion(error) {
            state = .failed(message: Self.message(for: error))
            return
        }

        // Benign (nil error, no update found, cancelled, or deferred). Never
        // overwrite a just-found update with "up to date".
        switch state {
        case .updateAvailable, .installing:
            break
        default:
            state = .upToDate
        }
    }

    private static func isBenignCompletion(_ error: Error) -> Bool {
        let nsError = error as NSError
        // "No update found" carries the reason in its userInfo.
        if nsError.userInfo[SPUNoUpdateFoundReasonKey] != nil { return true }
        guard nsError.domain == SUSparkleErrorDomain else { return false }
        switch nsError.code {
        case Int(SUError.noUpdateError.rawValue),
             Int(SUError.installationCanceledError.rawValue),
             Int(SUError.installationAuthorizeLaterError.rawValue):
            return true
        default:
            return false
        }
    }

    private static func message(for error: Error) -> String {
        let nsError = error as NSError
        if let description = nsError.userInfo[NSLocalizedDescriptionKey] as? String,
           !description.isEmpty {
            return description
        }
        let localized = nsError.localizedDescription
        return localized.isEmpty ? "Unknown error." : localized
    }

    private static func displayVersion(of item: SUAppcastItem) -> String {
        let display = item.displayVersionString
        return display.isEmpty ? item.versionString : display
    }
}

// MARK: - SPUUpdaterDelegate

extension UpdaterModel: SPUUpdaterDelegate {

    func updater(_ updater: SPUUpdater, didFindValidUpdate item: SUAppcastItem) {
        state = .updateAvailable(version: Self.displayVersion(of: item))
    }

    func updaterDidNotFindUpdate(_ updater: SPUUpdater, error: Error) {
        // Reached the feed successfully, but nothing newer/installable: a
        // completed check with no update, not a failure.
        state = .upToDate
    }

    func updater(_ updater: SPUUpdater, willInstallUpdate item: SUAppcastItem) {
        state = .installing(version: Self.displayVersion(of: item))
    }

    func updater(_ updater: SPUUpdater, didAbortWithError error: Error) {
        applyCompletion(error: error)
    }

    func updater(_ updater: SPUUpdater, didFinishUpdateCycleFor updateCheck: SPUUpdateCheck, error: Error?) {
        applyCompletion(error: error)
    }
}
