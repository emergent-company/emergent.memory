import AppKit
import SwiftUI

/// Permissions page (moved unchanged from the old monolith): per-service
/// Notes/Reminders Automation cards with probe, state badge, and deep link into
/// System Settings.
struct PermissionsPage: View {
    @State private var permissionStates: [PermissionCenter.Service: PermissionCenter.PermissionState] = [:]
    @State private var probingServices: Set<PermissionCenter.Service> = []
    @State private var alertMessage: String?

    var body: some View {
        Form {
            Section {
                ForEach(PermissionCenter.Service.allCases, id: \.self) { service in
                    permissionCard(service)
                }
            } header: {
                Text("Permissions")
            } footer: {
                Text("macOS asks once per app; grants are per-app and persist. Use Authorize to trigger the prompt, or open Automation settings directly.")
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Permissions")
        .alert("Memory", isPresented: Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(alertMessage ?? "")
        }
        .onAppear {
            loadPermissionStates()
        }
    }

    private func permissionCard(_ service: PermissionCenter.Service) -> some View {
        let state = permissionStates[service] ?? .unknown
        let isProbing = probingServices.contains(service)

        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: service.systemImage)
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(service.displayName)
                        .font(.body)
                    Text("Automation permission to control \(service.displayName).")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if isProbing {
                    ProgressView()
                        .controlSize(.small)
                } else {
                    Text(state.label)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(badgeColor(state))
                }
            }

            HStack(spacing: 10) {
                Button("Authorize") {
                    Task { await authorize(service) }
                }
                .disabled(isProbing || state == .granted)

                Button("Open Automation Settings…") {
                    openAutomationSettings()
                }
            }
        }
        .padding(.vertical, 2)
    }

    private func badgeColor(_ state: PermissionCenter.PermissionState) -> Color {
        switch state {
        case .granted: return .green
        case .denied: return .red
        case .requested: return .orange
        case .unknown: return .secondary
        }
    }

    private func loadPermissionStates() {
        for service in PermissionCenter.Service.allCases {
            permissionStates[service] = PermissionCenter.loadState(service)
        }
    }

    @MainActor
    private func authorize(_ service: PermissionCenter.Service) async {
        guard !probingServices.contains(service) else { return }
        probingServices.insert(service)
        defer { probingServices.remove(service) }

        // First probe after user action: the prompt may appear.
        PermissionCenter.markRequested(service)
        permissionStates[service] = .requested

        let outcome = await PermissionCenter.runProbe(service)
        let wasRequested = PermissionCenter.hasRequested(service)
        let state = PermissionCenter.state(
            fromProbe: outcome.exitCode,
            stderr: outcome.stderr,
            timedOut: outcome.timedOut,
            wasRequested: wasRequested
        )
        PermissionCenter.saveState(state, for: service)
        permissionStates[service] = state

        if state == .denied {
            alertMessage = "\(service.displayName) Automation permission was denied. Grant it in System Settings → Privacy & Security → Automation, then try again."
        }
    }

    private func openAutomationSettings() {
        if let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation") {
            NSWorkspace.shared.open(url)
        }
    }
}
