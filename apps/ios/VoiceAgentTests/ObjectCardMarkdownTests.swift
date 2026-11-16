import Foundation
@testable import Memory
import Testing

/// The gateway's markdown flag: a property the schema declares
/// `format: markdown` (or the long-form heuristics flag, #814) arrives as
/// `markdown: true`, so the card renders it through ``MarkdownRenderer``; every
/// other row stays literal text.
struct ObjectCardMarkdownFieldTests {
    @Test func markdownFlagDecodes() throws {
        let json = #"""
        {"id":"o1","title":"Release notes","type":{"name":"changelog"},
         "sections":[{"title":"Changelog Properties","fields":[
           {"label":"Release notes","name":"release_notes","value":"## v2\n\n- **faster** startup","markdown":true},
           {"label":"Version","name":"version","value":"v2"}
         ]}],"labels":[]}
        """#
        let card = try JSONDecoder().decode(ObjectCard.self, from: Data(json.utf8))
        let fields = try #require(card.sections.first?.fields)
        let notes = try #require(fields.first { $0.name == "release_notes" })
        #expect(notes.isMarkdown)
        let version = try #require(fields.first { $0.name == "version" })
        #expect(!version.isMarkdown)
        // The markdown source is carried verbatim; rendering happens in the view.
        #expect(notes.value.contains("## v2"))
    }

    @Test func markdownDefaultsToFalse() throws {
        let json = #"""
        {"id":"o1","title":"T","type":{"name":"t"},
         "sections":[{"title":"T Properties","fields":[
           {"label":"A","name":"a","value":"plain"}
         ]}],"labels":[]}
        """#
        let card = try JSONDecoder().decode(ObjectCard.self, from: Data(json.utf8))
        let field = try #require(card.sections.first?.fields.first)
        #expect(!field.isMarkdown)
    }
}
