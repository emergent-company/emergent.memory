import Foundation
import ServiceManagement

/// Seam over `SMAppService.mainApp`, so the toggle's logic is testable without
/// touching the real login-item registry of whatever machine runs the tests.
protocol LaunchAtLoginService: Sendable {
    /// True when the app is currently registered to open at login.
    var isEnabled: Bool { get }
    /// True when the user must approve the registration in System Settings
    /// (Login Items) before it takes effect.
    var requiresApproval: Bool { get }
    /// Registers or unregisters the app. Throws when the system refuses.
    func setEnabled(_ enabled: Bool) throws
}

/// The real service: `SMAppService.mainApp` (macOS 13+; this app targets 15).
///
/// The system owns the state — it is read back after every change instead of
/// being remembered in preferences, so the toggle can never drift from what
/// macOS will actually do at the next login.
struct SystemLaunchAtLoginService: LaunchAtLoginService {
    var isEnabled: Bool {
        SMAppService.mainApp.status == .enabled
    }

    var requiresApproval: Bool {
        SMAppService.mainApp.status == .requiresApproval
    }

    func setEnabled(_ enabled: Bool) throws {
        if enabled {
            try SMAppService.mainApp.register()
        } else {
            try SMAppService.mainApp.unregister()
        }
    }
}

/// The user-visible "launch at login" setting.
///
/// Small on purpose: no preferences subsystem, no cached copy of the flag. It
/// reads the system's own status, applies a change, re-reads, and reports a
/// failure rather than leaving a toggle that looks on while nothing is
/// registered.
@MainActor
@Observable
final class LaunchAtLogin {
    /// What to tell the user while macOS is holding the registration for
    /// approval. Shared with the tests so the copy stays pinned.
    static let approvalInstruction =
        "Approve Memory in System Settings › General › Login Items to finish."

    private let service: LaunchAtLoginService

    /// True when the app is registered to open at login.
    private(set) var isEnabled: Bool
    /// True when macOS is waiting for the user to approve the registration.
    private(set) var requiresApproval: Bool
    /// User-facing failure text from the last attempt, if any.
    private(set) var lastError: String?
    /// Informational text while the registration is pending approval.
    ///
    /// Deliberately separate from `lastError`: nothing failed, the user still
    /// has an action to take. It is derived on every `refresh()` so a relaunch
    /// with an unapproved registration explains the off toggle instead of
    /// leaving the user to toggle it again to find out why.
    private(set) var approvalNotice: String?

    init(service: LaunchAtLoginService = SystemLaunchAtLoginService()) {
        self.service = service
        isEnabled = service.isEnabled
        requiresApproval = service.requiresApproval
        approvalNotice = requiresApproval && !isEnabled ? Self.approvalInstruction : nil
    }

    /// Re-reads the system status (on appear, when the app becomes active, and
    /// after every change), re-deriving the pending-approval notice with it.
    func refresh() {
        isEnabled = service.isEnabled
        requiresApproval = service.requiresApproval
        approvalNotice = requiresApproval && !isEnabled ? Self.approvalInstruction : nil
    }

    /// Applies `enabled`, then reflects what the system actually reports.
    ///
    /// A refusal (the system said no, or the app is not in a location macOS
    /// will register) surfaces as `lastError`; a registration that lands in
    /// `.requiresApproval` surfaces as the informational `approvalNotice`
    /// instead, because the toggle staying off with no explanation is the
    /// failure mode this exists to avoid.
    func setEnabled(_ enabled: Bool) {
        lastError = nil
        do {
            try service.setEnabled(enabled)
        } catch {
            lastError = error.localizedDescription
            ConnectorLog.lifecycle("launchAtLogin failed enabled=\(enabled) error=\(error.localizedDescription)")
        }
        refresh()
        if lastError == nil, enabled, !isEnabled, !requiresApproval {
            lastError = "macOS did not register Memory to open at login."
        }
    }

    /// Opens the Login Items pane so the user can approve the app.
    func openLoginItemsSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }
}
