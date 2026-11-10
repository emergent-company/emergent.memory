import SwiftUI

// MARK: - Timeline rows

extension ToolActivityGroup {
    /// A run drawn from the shared chat timeline: its identity is the first
    /// call's id hashed into the UUID the disclosure store keys on, so a reload
    /// (which rebuilds every row) cannot reset the reader's open/closed choice.
    init(tools: [ToolCallInfo], turnKey: String) {
        self.init(tools: tools, turnID: StableTurnID.uuid(for: turnKey))
    }
}

/// A stable UUID for a string identity, so a drawn row can be remembered by its
/// server id (``ToolActivityDisclosure``) instead of by position.
enum StableTurnID {
    static func uuid(for key: String) -> UUID {
        // FNV-1a over the key: deterministic across launches, so the same run
        // keeps its row identity after a reload. The high half is mixed again
        // so the 128 bits are not one repeated word.
        var hash: UInt64 = 0xCBF2_9CE4_8422_2325
        for byte in key.utf8 {
            hash ^= UInt64(byte)
            hash = hash &* 0x100_0000_01B3
        }
        var second = hash &* 0x9E37_79B9_7F4A_7C15
        var bytes: [UInt8] = []
        for _ in 0 ..< 8 {
            bytes.append(UInt8(truncatingIfNeeded: hash))
            hash >>= 8
        }
        for _ in 0 ..< 8 {
            bytes.append(UInt8(truncatingIfNeeded: second))
            second >>= 8
        }
        // A well-formed RFC 4122 value, so it round-trips like any other id.
        bytes[6] = (bytes[6] & 0x0F) | 0x50
        bytes[8] = (bytes[8] & 0x3F) | 0x80
        return UUID(uuid: (
            bytes[0], bytes[1], bytes[2], bytes[3],
            bytes[4], bytes[5], bytes[6], bytes[7],
            bytes[8], bytes[9], bytes[10], bytes[11],
            bytes[12], bytes[13], bytes[14], bytes[15]
        ))
    }
}
