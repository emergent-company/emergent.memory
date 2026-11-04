import Foundation

/// Shared date formatting for the session list and object detail.
///
/// The backend returns timestamps in two string shapes: object `created_at` is
/// RFC 3339 (`"2026-09-01T10:00:00Z"`) and session `started_at` is a clock
/// string (`"HH:mm:ss.SSS"`). Each helper parses its shape and renders it with
/// the user's locale; an unparseable value is returned verbatim so the UI never
/// blanks out.
enum DateFormatting {
    /// RFC 3339 parser (whole-second) for object timestamps.
    private static let isoFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    /// RFC 3339 parser (fractional seconds) for object timestamps.
    private static let isoFractionalFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    /// Locale-aware medium date + short time, for object timestamps.
    private static let objectDisplay: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter
    }()

    /// Parses the backend's `"HH:mm:ss.SSS"` clock string.
    private static let sessionParser: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "HH:mm:ss.SSS"
        return formatter
    }()

    /// Locale-aware medium date + short time, for session timestamps (which are
    /// RFC 3339 now, legacy clock strings otherwise).
    private static let sessionDisplay: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter
    }()

    /// Formats an object `created_at` (RFC 3339) as a locale-aware medium date.
    /// Returns the raw string when it cannot be parsed.
    static func formatObjectDate(_ raw: String?) -> String? {
        guard let raw, !raw.isEmpty else { return nil }
        let parsed = isoFormatter.date(from: raw) ?? isoFractionalFormatter.date(from: raw)
        guard let parsed else { return raw }
        return objectDisplay.string(from: parsed)
    }

    /// Formats a session timestamp as a locale-aware date + time. Chat sessions
    /// use RFC 3339 (`"2026-09-01T10:00:00Z"`, optionally fractional); older
    /// records still carry the legacy `"HH:mm:ss.SSS"` clock string. Returns
    /// the raw string when neither shape parses.
    static func formatSessionDate(_ raw: String?) -> String? {
        guard let raw, !raw.isEmpty else { return nil }
        let parsed = isoFormatter.date(from: raw)
            ?? isoFractionalFormatter.date(from: raw)
            ?? sessionParser.date(from: raw)
        guard let parsed else { return raw }
        return sessionDisplay.string(from: parsed)
    }
}
