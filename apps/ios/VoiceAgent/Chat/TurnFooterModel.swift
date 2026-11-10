import Foundation

/// What a finished turn publishes about its run: the model that answered, when
/// it started, and when it ended.
///
/// The recorded history supplies all three (`run_start` names the model,
/// `run_end` carries `completed_at`), and the timeline's ``RunWindow`` keeps
/// them. A live run has only its two boundaries: the stream forwards neither a
/// model nor a server duration, so its footer shows the duration and end clock
/// until the record reloads and adds the model.
struct TurnRunInfo: Equatable, Sendable {
    var model: String?
    var startedAt: Date?
    var endedAt: Date?

    init(model: String? = nil, startedAt: Date? = nil, endedAt: Date? = nil) {
        self.model = model
        self.startedAt = startedAt
        self.endedAt = endedAt
    }
}

/// The line under a finished turn: `model · m:ss · 14:05`, matching the web's
/// `turnFooter` (`chat-components.js`) and its duration format (`formatDuration`,
/// `chat-host.js`).
///
/// A pure value: the time zone and locale are stored rather than read from the
/// process, so a test asserts one fixed line whatever the host's settings.
struct TurnFooterModel {
    let run: TurnRunInfo
    var timeZone: TimeZone = .current
    var locale: Locale = .current

    /// "0:07", "1:32", "1:02:03" — minutes unpadded under an hour, hours
    /// unpadded and minutes padded from one hour, seconds always two digits.
    /// Nil when the span is unknown or the end precedes the start.
    var duration: String? {
        guard let startedAt = run.startedAt,
              let endedAt = run.endedAt,
              endedAt >= startedAt
        else { return nil }
        return Self.format(duration: endedAt.timeIntervalSince(startedAt))
    }

    /// The local clock time the turn ended, in the injected zone and locale.
    var endClock: String? {
        guard let endedAt = run.endedAt else { return nil }
        return Self.format(clock: endedAt, timeZone: timeZone, locale: locale)
    }

    /// Every part that exists, joined with the web footer's separator: the
    /// model when the run named one, then the duration and the end clock.
    ///
    /// Duration and end time are required — a run with no end boundary has no
    /// footer — while the model is not: a live run has none until the recorded
    /// history reloads, and the web's `turnFooter` omits it the same way rather
    /// than dropping the whole line.
    var line: String? {
        guard let duration, let endClock else { return nil }
        let name = run.model?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let parts: [String?] = [name.isEmpty ? nil : name, duration, endClock]
        return parts
            .compactMap(\.self)
            .joined(separator: " · ")
    }

    /// A span in the web's compact form: `m:ss`, and `h:mm:ss` from one hour.
    ///
    /// Rounds to the nearest second, as the web's `Math.round(ms / 1000)` does.
    static func format(duration: TimeInterval) -> String? {
        guard duration.isFinite, duration >= 0 else { return nil }
        let total = Int(duration.rounded())
        let hours = total / 3600
        let minutes = (total % 3600) / 60
        let seconds = total % 60
        if hours > 0 {
            return "\(hours):" + twoDigits(minutes) + ":" + twoDigits(seconds)
        }
        return "\(minutes):" + twoDigits(seconds)
    }

    /// A short clock time ("14:05", "2:05 PM") for the current locale.
    static func format(clock: Date, timeZone: TimeZone, locale: Locale) -> String {
        clock.formatted(
            Date.FormatStyle(date: .omitted, time: .shortened, locale: locale, timeZone: timeZone)
                .hour(.twoDigits(amPM: .abbreviated))
                .minute(.twoDigits)
        )
    }

    private static func twoDigits(_ value: Int) -> String {
        value < 10 ? "0\(value)" : String(value)
    }
}
