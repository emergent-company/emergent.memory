import AVFoundation
import SwiftUI

/// QR-code onboarding scanner — backend authentication, not agent config.
///
/// Scans the one-time setup QR code (see the gateway's `GET /api/qr-config`).
/// The payload is JSON carrying the setup endpoint URL and a one-time token;
/// the token is POSTed to the setup endpoint, which returns the per-device
/// server URL, token endpoint, API base URL, and API key. Those are stored in
/// `MemoryConfig` so the app can reach the backend. It never creates or
/// changes an agent — agents are added explicitly through the add-agent flow.
struct MemoryQRScannerView: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var agentStore: AgentStore

    /// Payload of the one-time setup QR code. Required fields: `setupURL`
    /// (the setup endpoint) and `token` (the one-time setup token). Both must
    /// be non-empty; anything else is malformed and must not touch the
    /// previous configuration.
    private struct QRSetupPayload: Decodable {
        let setupURL: String
        let token: String
    }

    private enum ScanPhase {
        case requesting  // camera permission not granted yet
        case denied      // permission denied, or no camera available
        case scanning
    }

    @State private var phase: ScanPhase = .requesting
    @State private var session: AVCaptureSession?
    @State private var hasDecodeError = false
    /// User-presentable message for the most recent setup failure (shown in
    /// place of the generic decode-failure text while scanning resumes).
    @State private var setupErrorMessage: String?
    @State private var configured = false
    @State private var showOpenSettings = false
    @State private var denialMessage: LocalizedStringKey = "qr.permission.denied"
    /// Timestamp of the last decode attempt. The metadata output re-reports
    /// the same code while it stays in view, so repeated reads are ignored
    /// for a short window (debounce, see `handleFound`).
    @State private var lastDecodeAt: Date?

    var body: some View {
        ZStack {
            switch phase {
            case .requesting:
                requestingView()
            case .denied:
                deniedView()
            case .scanning:
                scannerContent()
            }
        }
        .task { await startScanning() }
        .onDisappear { session?.stopRunning() }
    }

    // MARK: - Camera permission + session setup

    @MainActor
    private func startScanning() async {
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            setupSession()
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { granted in
                Task { @MainActor in
                    if granted {
                        self.setupSession()
                    } else {
                        self.showOpenSettings = true
                        self.phase = .denied
                    }
                }
            }
        default:
            showOpenSettings = true
            phase = .denied
        }
    }

    @MainActor
    private func setupSession() {
        let session = AVCaptureSession()
        guard
            let device = AVCaptureDevice.default(for: .video),
            let input = try? AVCaptureDeviceInput(device: device),
            session.canAddInput(input)
        else {
            denialMessage = "qr.cameraUnavailable"
            showOpenSettings = false
            phase = .denied
            return
        }
        session.addInput(input)
        self.session = session
        phase = .scanning
        // Blocks briefly; acceptable for the one-time setup scan.
        session.startRunning()
    }

    // MARK: - Decode + apply

    @MainActor
    private func handleFound(_ value: String) {
        guard phase == .scanning, !configured else { return }
        // Debounce: the same code is re-reported while it is in view. Ignore
        // repeats inside the window so a failed decode can be retried by
        // re-pointing the camera and a successful one is applied only once.
        if let last = lastDecodeAt, Date().timeIntervalSince(last) < 1.0 {
            return
        }
        lastDecodeAt = Date()

        guard
            let data = value.data(using: .utf8),
            let payload = try? JSONDecoder().decode(QRSetupPayload.self, from: data)
        else {
            // Not an Memory setup code: show the error inline, keep scanning.
            hasDecodeError = true
            return
        }

        // A valid code must carry a non-empty setup URL and token; anything
        // else is malformed and must not touch the previous configuration.
        let setupURL = payload.setupURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let token = payload.token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !setupURL.isEmpty, !token.isEmpty, let url = URL(string: setupURL), url.scheme != nil else {
            hasDecodeError = true
            return
        }

        // Stop the camera while the exchange runs so the same code is not
        // re-reported mid-request; it is restarted if the exchange fails.
        session?.stopRunning()

        // Exchange the one-time token for the per-device configuration,
        // introducing the device so the gateway can label the registration.
        Task { @MainActor in
            do {
                let response = try await MemorySetupClient().setup(
                    url: url,
                    token: token,
                    device: DeviceInfo().manifest
                )
                Log.qr.info("setup ok apiBaseURL=\(response.apiBaseURL)")

                // Overwrite the connection settings; keep everything else
                // (agentName, exit keywords, ...) from the current config.
                var config = MemoryConfig()
                config.serverURL = response.serverURL
                config.tokenEndpoint = response.tokenEndpoint
                config.apiBaseURL = response.apiBaseURL
                config.apiKey = response.apiKey
                config.ttsStrategy = response.ttsStrategy ?? "server"
                config.save()
                NotificationCenter.default.post(name: .memoryConfigChanged, object: nil)

                // The whole point of authenticating is reaching the backend —
                // refresh the agent list against the newly configured control
                // plane.
                await agentStore.load()

                configured = true
                hasDecodeError = false
                setupErrorMessage = nil

                // Show the confirmation, then close.
                try? await Task.sleep(for: .seconds(1.5))
                dismiss()
            } catch {
                // Non-2xx, unreachable, or undecodable: show the message,
                // keep scanning so the code can be tried again.
                Log.qr.error("setup failed: \(error.localizedDescription)")
                hasDecodeError = true
                setupErrorMessage = error.localizedDescription
                session?.startRunning()
            }
        }
    }

    // MARK: - States

    private func requestingView() -> some View {
        VStack(spacing: 3 * .grid) {
            Spinner()
            Text("qr.requesting")
                .font(.system(size: 12))
                .foregroundStyle(.fg3)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.bg1)
    }

    private func deniedView() -> some View {
        VStack(spacing: 3 * .grid) {
            HStack {
                Spacer()
                closeButton()
            }
            Spacer()
            Image(systemName: "video.slash")
                .font(.system(size: 28))
                .foregroundStyle(.fgModerate)
            Text(denialMessage)
                .font(.system(size: 13))
                .foregroundStyle(.fg3)
                .multilineTextAlignment(.center)
            if showOpenSettings {
                Button(action: openSettings) {
                    Text("qr.openSettings")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.fgAccent)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .padding(.top, 2 * .grid)
            }
            Spacer()
            Spacer()
        }
        .padding(4 * .grid)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.bg1)
    }

    private func scannerContent() -> some View {
        ZStack {
            if let session {
                CameraScannerView(session: session, onFound: handleFound)
                    .ignoresSafeArea()
            }
            VStack {
                HStack {
                    Spacer()
                    closeButton()
                }
                Spacer()
            }
            .padding(3 * .grid)
        }
        .overlay(alignment: .bottom) {
            statusNote()
                .padding(4 * .grid)
                .padding(.bottom, 3 * .grid)
        }
    }

    // MARK: - Chrome

    private func closeButton() -> some View {
        Button {
            dismiss()
        } label: {
            Image(systemName: "xmark")
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(.fg3)
                .padding(2 * .grid)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("qr.close")
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }

    @ViewBuilder
    private func statusNote() -> some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: statusIcon)
                .foregroundStyle(statusColor)
            Text(statusText)
                .font(.system(size: 12))
                .foregroundStyle(.fg3)
        }
        .padding(3 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }

    private var statusIcon: String {
        if configured { return "checkmark.circle.fill" }
        if hasDecodeError { return "exclamationmark.triangle.fill" }
        return "qrcode.viewfinder"
    }

    private var statusColor: Color {
        if configured { return .fgSuccess }
        if hasDecodeError { return .fgSerious }
        return .fg3
    }

    private var statusText: String {
        if configured { return NSLocalizedString("qr.configured", comment: "") }
        if hasDecodeError {
            return setupErrorMessage ?? NSLocalizedString("qr.decode.failure", comment: "")
        }
        return NSLocalizedString("qr.scan.hint", comment: "")
    }

    // MARK: - Camera preview (AVFoundation)

    private struct CameraScannerView: UIViewRepresentable {
        let session: AVCaptureSession
        let onFound: (String) -> Void

        final class Coordinator: NSObject, AVCaptureMetadataOutputObjectsDelegate {
            let onFound: (String) -> Void

            init(onFound: @escaping (String) -> Void) {
                self.onFound = onFound
            }

            /// Metadata callbacks arrive on the delegate queue (a background
            /// thread), so SwiftUI state is only touched via the main actor.
            nonisolated func metadataOutput(
                _ output: AVCaptureMetadataOutput,
                didOutput metadataObjects: [AVMetadataObject],
                from connection: AVCaptureConnection
            ) {
                guard
                    let code = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
                    let value = code.stringValue
                else { return }
                Task { @MainActor [weak self] in
                    self?.onFound(value)
                }
            }
        }

        func makeCoordinator() -> Coordinator {
            Coordinator(onFound: onFound)
        }

        func makeUIView(context: Context) -> PreviewView {
            let view = PreviewView()
            view.videoPreviewLayer.session = session
            view.videoPreviewLayer.videoGravity = .resizeAspectFill

            let output = AVCaptureMetadataOutput()
            if session.canAddOutput(output) {
                session.addOutput(output)
                output.setMetadataObjectsDelegate(context.coordinator, queue: .main)
                output.metadataObjectTypes = [.qr]
            }
            return view
        }

        func updateUIView(_ uiView: PreviewView, context: Context) {
            uiView.videoPreviewLayer.session = session
        }
    }

    private final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var videoPreviewLayer: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }
}

#Preview {
    MemoryQRScannerView()
        .environmentObject(AgentStore())
}
