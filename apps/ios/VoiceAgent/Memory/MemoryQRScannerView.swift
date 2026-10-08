import AVFoundation
import SwiftUI

/// QR-code onboarding scanner — backend authentication, not agent config.
///
/// Scans the one-time setup QR, POSTs its token to the setup endpoint, and
/// records the returned per-device connection as a project. It never creates
/// or changes an agent — agents are added explicitly through the add flow.
struct MemoryQRScannerView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AgentStore.self) private var agentStore
    @EnvironmentObject private var projectStore: MemoryProjectStore

    /// Payload of the one-time setup QR code. Required fields: `setupURL`
    /// (the setup endpoint) and `token` (the one-time setup token). Both must
    /// be non-empty; anything else is malformed and must not touch the
    /// previous configuration. The project identity is optional so a QR minted
    /// by an older gateway still onboards.
    private struct QRSetupPayload: Decodable {
        let setupURL: String
        let token: String
        let orgID: String?
        let projectID: String?
        let projectName: String?
    }

    private enum ScanPhase {
        case requesting // camera permission not granted yet
        case denied // permission denied, or no camera available
        case scanning
    }

    @State private var phase: ScanPhase = .requesting
    @State private var session: AVCaptureSession?
    @State var hasDecodeError = false
    /// User-presentable message for the most recent setup failure (shown in
    /// place of the generic decode-failure text while scanning resumes).
    @State var setupErrorMessage: String?
    @State var configured = false
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
        #if os(iOS)
            .sensoryFeedback(.success, trigger: configured)
        #endif
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
                        setupSession()
                    } else {
                        showOpenSettings = true
                        phase = .denied
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

    // MARK: - States

    private func requestingView() -> some View {
        VStack(spacing: 3 * .grid) {
            Spinner()
            Text("qr.requesting")
                .font(MemoryFont.meta)
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
                .font(.title)
                .foregroundStyle(.fgModerate)
            Text(denialMessage)
                .font(.footnote)
                .foregroundStyle(.fg3)
                .multilineTextAlignment(.center)
            if showOpenSettings {
                Button(action: openSettings) {
                    Text("qr.openSettings")
                        .font(MemoryFont.labelEmphasis)
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
                .font(.footnote.weight(.medium))
                .foregroundStyle(.fg3)
                .padding(2 * .grid)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("qr.close")
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }

    // MARK: - Camera preview (AVFoundation)

    private struct CameraScannerView: UIViewRepresentable {
        let session: AVCaptureSession
        let onFound: (String) -> Void

        final class Coordinator: NSObject, AVCaptureMetadataOutputObjectsDelegate, @unchecked Sendable {
            let onFound: (String) -> Void

            init(onFound: @escaping (String) -> Void) {
                self.onFound = onFound
            }

            /// Metadata callbacks arrive on the delegate queue (a background
            /// thread), so SwiftUI state is only touched via the main actor.
            nonisolated func metadataOutput(
                _: AVCaptureMetadataOutput,
                didOutput metadataObjects: [AVMetadataObject],
                from _: AVCaptureConnection
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

        func updateUIView(_ uiView: PreviewView, context _: Context) {
            uiView.videoPreviewLayer.session = session
        }
    }

    private final class PreviewView: UIView {
        override class var layerClass: AnyClass {
            AVCaptureVideoPreviewLayer.self
        }

        var videoPreviewLayer: AVCaptureVideoPreviewLayer {
            layer as! AVCaptureVideoPreviewLayer
        }
    }
}

// MARK: - Decode + apply

extension MemoryQRScannerView {
    /// Debounces and validates a scanned value, then hands it to the exchange.
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
        exchangeSetupToken(url: url, token: token, payload: payload)
    }

    /// Exchanges the one-time token for the per-device configuration, records
    /// (or updates) the project connection, and refreshes the agent list.
    @MainActor
    private func exchangeSetupToken(url: URL, token: String, payload: QRSetupPayload) {
        Task { @MainActor in
            do {
                let response = try await MemorySetupClient().setup(
                    url: url,
                    token: token,
                    device: DeviceInfo().manifest
                )
                Log.qr.info("setup ok apiBaseURL=\(response.apiBaseURL)")

                // Each QR is one project: record (or update) that connection and
                // make it active. The store mirrors the active connection into
                // MemoryConfig and posts .memoryConfigChanged, so the session
                // controller rebuilds from the new credential.
                let connection = MemoryProjectConnection(
                    projectID: response.projectID ?? payload.projectID ?? "",
                    orgID: response.orgID ?? payload.orgID ?? "",
                    projectName: response.projectName ?? payload.projectName ?? "",
                    serverURL: response.serverURL,
                    tokenEndpoint: response.tokenEndpoint,
                    apiBaseURL: response.apiBaseURL,
                    apiKey: response.apiKey,
                    ttsStrategy: response.ttsStrategy ?? MemoryConfig.defaultTTSStrategy,
                    addedAt: Date()
                )
                projectStore.upsert(connection)
                projectStore.setActive(projectID: connection.projectID)
                agentStore.setActiveProject(projectStore.activeProjectID)
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
}

#Preview {
    MemoryQRScannerView()
        .environment(AgentStore())
        .environmentObject(MemoryProjectStore())
}
