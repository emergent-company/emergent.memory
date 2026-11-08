import Foundation
@testable import Memory
import Testing

/// The turn-footer line: the web's compact duration, the end clock in an
/// injected zone and locale, and the rule that a partial footer is suppressed.
struct TurnFooterModelTests {
    private let gmt = TimeZone.gmt
    private let british = Locale(identifier: "en_GB")

    private func date(_ string: String) -> Date {
        ISO8601DateFormatter().date(from: string)!
    }

    private func footer(
        model: String? = "deepseek-v4-flash",
        start: String? = "2026-11-08T10:00:00Z",
        end: String? = "2026-11-08T10:02:03Z"
    ) -> TurnFooterModel {
        TurnFooterModel(
            run: TurnRunInfo(
                model: model,
                startedAt: start.map(date),
                endedAt: end.map(date)
            ),
            timeZone: gmt,
            locale: british
        )
    }

    @Test func durationUnderAnHourIsMinutesColonSeconds() {
        #expect(TurnFooterModel.format(duration: 0) == "0:00")
        #expect(TurnFooterModel.format(duration: 7) == "0:07")
        #expect(TurnFooterModel.format(duration: 152) == "2:32")
    }

    @Test func durationAtOneHourAndBeyondIsHoursColonMinutesColonSeconds() {
        // Exactly one hour is the boundary: under it the hours field is
        // omitted, at it the format gains one.
        #expect(TurnFooterModel.format(duration: 3599) == "59:59")
        #expect(TurnFooterModel.format(duration: 3600) == "1:00:00")
        #expect(TurnFooterModel.format(duration: 3723) == "1:02:03")
        #expect(TurnFooterModel.format(duration: 5400) == "1:30:00")
    }

    @Test func composedLineJoinsModelDurationAndEndClock() {
        let model = footer()
        #expect(model.duration == "2:03")
        #expect(model.endClock == "10:02")
        #expect(model.line == "deepseek-v4-flash · 2:03 · 10:02")
    }

    @Test func lineIsNilWhenAnyPartIsMissing() {
        #expect(footer(model: nil).line == nil)
        #expect(footer(model: "   ").line == nil)
        #expect(footer(start: nil).line == nil)
        #expect(footer(end: nil).line == nil)
    }

    @Test func anEndBeforeTheStartHasNoDurationAndNoLine() {
        let model = footer(start: "2026-11-08T10:02:03Z", end: "2026-11-08T10:00:00Z")
        #expect(model.duration == nil)
        #expect(model.line == nil)
    }

    @Test func endClockUsesTheInjectedTimeZoneAndLocale() throws {
        let shanghai = try #require(TimeZone(identifier: "Asia/Shanghai"))
        let start = try #require(ISO8601DateFormatter().date(from: "2026-11-08T10:00:00Z"))
        let end = try #require(ISO8601DateFormatter().date(from: "2026-11-08T10:02:30Z"))
        let model = TurnFooterModel(
            run: TurnRunInfo(model: "flash", startedAt: start, endedAt: end),
            timeZone: shanghai,
            locale: british
        )

        // 10:02 UTC is 18:02 in Shanghai; the host's own zone must not matter.
        #expect(model.endClock == "18:02")
        #expect(model.line == "flash · 2:30 · 18:02")
    }
}
