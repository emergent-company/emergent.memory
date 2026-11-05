import Combine
import Foundation
import LiveKit

/// App-owned microphone controls, replacing LiveKit's `LocalMedia`.
///
/// LiveKit's `LocalMedia` enables "recording always prepared" mode as soon as
/// it is created (`AudioManager.setRecordingAlwaysPreparedMode(true)`): the
/// audio engine starts in a muted state and the `AVAudioSession` is configured
/// as `.playAndRecord`. `MemorySessionController` is built at launch for the
/// remembered agent, so that pre-warm interrupted audio from other apps —
/// music or a podcast stopped the moment Memory opened, long before the user
/// joined a call.
///
/// This type keeps the microphone surface the app needs (state, track, toggle,
/// input device) but never touches the audio device by itself: the engine
/// starts only when the user actually enables the microphone in a voice
/// session, and text conversations acquire no audio at all.
@MainActor
final class LocalMediaModel: ObservableObject {
    /// A microphone (or input device) failure surfaced in the UI.
    enum MediaError: LocalizedError {
        case device(Swift.Error)

        var errorDescription: String? {
            switch self {
            case let .device(error):
                "Media device error: \(error.localizedDescription)"
            }
        }
    }

    /// The last error that occurred.
    @Published private(set) var error: MediaError?
    /// The local microphone track, once the room has published one.
    @Published private(set) var microphoneTrack: (any AudioTrack)?
    /// Whether the local microphone is currently enabled.
    @Published private(set) var isMicrophoneEnabled = false
    /// The available audio input devices.
    @Published private(set) var audioDevices: [AudioDevice] = AudioManager.shared.inputDevices
    /// The ID of the selected audio input device.
    @Published private(set) var selectedAudioDeviceID: String = AudioManager.shared.inputDevice.deviceId

    private let localParticipant: LocalParticipant
    private var cancellables = Set<AnyCancellable>()

    init(room: Room) {
        localParticipant = room.localParticipant
        refresh()
        observe()
        observeDevices()
    }

    convenience init(session: Session) {
        self.init(room: session.room)
    }

    /// Resets the last error.
    func dismissError() {
        error = nil
    }

    /// Toggles the microphone on or off.
    ///
    /// Enabling it here is what starts the audio engine (and asks for the
    /// microphone permission) — not loading the app.
    func toggleMicrophone() async {
        do {
            try await localParticipant.setMicrophone(enabled: !isMicrophoneEnabled)
        } catch {
            self.error = .device(error)
        }
    }

    /// Selects an audio input device.
    func select(audioDevice: AudioDevice) {
        selectedAudioDeviceID = audioDevice.deviceId
        let device = AudioManager.shared.inputDevices.first { $0.deviceId == selectedAudioDeviceID }
            ?? AudioManager.shared.inputDevice
        AudioManager.shared.inputDevice = device
    }

    // MARK: - Observation

    private func observe() {
        localParticipant.objectWillChange
            // `objectWillChange` fires before the new track/state is stored.
            .sink { [weak self] _ in
                Task { @MainActor in
                    await Task.yield()
                    self?.refresh()
                }
            }
            .store(in: &cancellables)
    }

    private func refresh() {
        // The SDK's `firstAudioTrack` helper is module-internal, so mirror it
        // over the public publication list: the first unmuted audio track.
        microphoneTrack = localParticipant.trackPublications.values
            .first { $0.kind == .audio && !$0.isMuted }?
            .track as? AudioTrack
        isMicrophoneEnabled = localParticipant.isMicrophoneEnabled()
    }

    private func observeDevices() {
        AudioManager.shared.onDeviceUpdate = { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self else { return }
                audioDevices = AudioManager.shared.inputDevices
                selectedAudioDeviceID = AudioManager.shared.defaultInputDevice.deviceId
            }
        }
    }
}
