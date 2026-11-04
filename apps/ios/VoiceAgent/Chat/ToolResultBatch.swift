import Foundation

/// Reads a batch create's counts out of the payload.
///
/// `entity-create` answers with `data.results` plus a `meta` tally beside the
/// envelope; the readable list keeps only created records, so this is the one
/// place the failure count can come from (#2277).
enum ToolResultBatch {
    /// The counts a batch payload carries, or nil when the payload is not a
    /// batch. Per-item flags are the fallback when `meta` is absent.
    static func tally(from json: Any) -> ToolResultPresentation.BatchTally? {
        guard let record = json as? [String: Any] else { return nil }
        // The MCP envelope uses `data`; the agent's wrapper uses `result`.
        let payload = (record["data"] as? [String: Any])
            ?? (record["result"] as? [String: Any])
            ?? record
        guard let results = payload["results"] as? [Any] else { return nil }

        let meta = (record["meta"] as? [String: Any]) ?? (payload["meta"] as? [String: Any])
        let failedItems = results.filter { result in
            (result as? [String: Any])?["ok"] as? Bool == false
        }.count
        let similar = meta.flatMap { integer($0["similar_count"]) } ?? 0

        if let created = meta.flatMap({ integer($0["created"]) }),
           let failed = meta.flatMap({ integer($0["failed"]) })
        {
            return ToolResultPresentation.BatchTally(created: created, failed: failed, similar: similar)
        }

        // No tally: only a batch-shaped result may be counted from its items, so
        // a web-search `results` array is never reported as "0 created".
        let looksLikeBatch = results.contains { result in
            guard let item = result as? [String: Any] else { return false }
            return item["entity"] != nil || item["index"] != nil || (item["ok"] as? Bool) != nil
        }
        guard looksLikeBatch else { return nil }
        return ToolResultPresentation.BatchTally(
            created: max(0, results.count - failedItems),
            failed: failedItems,
            similar: similar
        )
    }

    private static func integer(_ value: Any?) -> Int? {
        (value as? NSNumber)?.intValue
    }
}
