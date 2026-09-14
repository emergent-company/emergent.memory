import Foundation

/// A memory stored by an agent (Diane has memory; Memory does not).
///
/// Mirrors the `GET /api/memories` response item:
/// `{ "id": "<uuid>", "content": "<text>", "category": "...", "confidence": <number> }`.
///
/// `category` is a free-form string: a note category (`preference`, `fact`,
/// `pattern`, `correction`, `instruction`, `convention`) for note observations,
/// or the entity type (`person`, `task`, `project`, `calendar_event`,
/// `financial_transaction`, `contact`, `place`, `note`, `habit`, `NoteCluster`)
/// for typed graph entities.
struct Memory: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let content: String
    let category: String
    let confidence: Double

    /// Localization key for the category/type label. Known values map to a
    /// localized name under `memory.category.<value>`.
    var categoryDisplayNameKey: String { "memory.category.\(category)" }
}
