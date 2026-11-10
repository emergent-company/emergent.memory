import Foundation
@testable import Memory
import Testing

/// The Sources footer's count label follows the catalog's plural variations:
/// one cited object reads "1 source", several read "3 sources". Pinned in
/// English so the assertion does not depend on the simulator's language.
struct SourcesFooterLabelTests {
    private let english = Locale(identifier: "en_US")

    @Test func oneCitedObjectReadsAsOneSource() {
        #expect(SourcesCountLabel.text(for: 1, locale: english) == "1 source")
    }

    @Test func severalCitedObjectsReadAsPluralSources() {
        #expect(SourcesCountLabel.text(for: 3, locale: english) == "3 sources")
    }

    /// The catalog key must exist: a missing entry would render
    /// "chat.sources.count 3" instead of the count.
    @Test func theLabelIsNotTheRawKey() {
        #expect(!SourcesCountLabel.text(for: 3, locale: english).contains("chat.sources.count"))
    }
}
