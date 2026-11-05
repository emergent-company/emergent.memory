import Foundation
import LiveKit
@testable import Memory
import Testing

/// Guards the app's audio-session contract: nothing acquires audio until the
/// user actually starts using voice.
struct LocalMediaModelTests {
    /// LiveKit's own `LocalMedia` enables "recording always prepared" mode as
    /// soon as it is constructed, which starts the audio engine (muted) and
    /// configures `AVAudioSession` as `.playAndRecord` — interrupting music
    /// from other apps merely because Memory launched. The app-owned model
    /// must leave the device alone until the microphone is enabled.
    @MainActor
    @Test func creatingTheModelDoesNotPrepareRecording() async throws {
        let room = Room()
        let model = LocalMediaModel(room: room)

        // Deferred audio setup would run on a task; give it a chance to.
        try await Task.sleep(for: .milliseconds(100))

        #expect(!AudioManager.shared.isRecordingAlwaysPreparedMode)
        #expect(!model.isMicrophoneEnabled)
        withExtendedLifetime(model) {}
    }
}
