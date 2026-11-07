import SwiftUI

/// Project switcher at the leading edge of the window toolbar.
///
/// The trigger is one compact line — active project name, connection dot,
/// disclosure chevron — the shape a Mac toolbar expects. The organisation used
/// to be stacked above the name in a two-line label, which is a browser-navbar
/// pattern: it was the only two-line item in the titlebar and it crowded the
/// toolbar's baseline. The organisation now lives in the panel header, where
/// there is room for it.
///
/// `MainWindowView` installs this control only while the account is effectively
/// signed in — signed out there is nothing to switch, and the trigger's own
/// "Sign in" label would sit beside the account control's button, one action
/// twice. So this view has no signed-out label of its own: losing the session
/// removes the control rather than relettering it, and the labels below describe
/// the project store's own states should it report a signed-out session under a
/// window the account store still calls signed in.
///
/// The panel itself is `ProjectSwitcherPanel`, a real view in a `.popover`
/// rather than a `Menu` (see that type for why).
struct ProjectSwitcherView: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore

    @State private var showsPanel = false

    /// Width the project name may occupy before it tail-truncates, so a long
    /// name cannot widen the toolbar.
    private static let labelMaxWidth: CGFloat = 220

    var body: some View {
        trigger
            .accessibilityIdentifier("toolbar.project")
            // Self-heal: if bootstrap ran before the session was restored (or
            // failed), load once when the picker appears with no projects.
            .task { await ensureProjectsLoaded() }
            .popover(isPresented: $showsPanel, arrowEdge: .bottom) {
                ProjectSwitcherPanel(onDismiss: { showsPanel = false })
                    .padding(10)
                    .frame(width: 320)
            }
    }

    // MARK: - Trigger

    private var trigger: some View {
        Button {
            showsPanel.toggle()
        } label: {
            HStack(spacing: 5) {
                if hasError {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
                triggerTitle
                    .font(.subheadline.weight(.medium))
                    .lineLimit(1)
                    .truncationMode(.tail)
                connectionGlyph
                trailingSlot
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .frame(maxWidth: Self.labelMaxWidth, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(Text(AccessibleNames.project))
        .accessibilityValue(accessibilityValue)
        .help(switchHelp)
        // One switch at a time: the first load has no list to show yet.
        .disabled(projectStore.state == .loading && projectStore.projects.isEmpty)
    }

    @ViewBuilder
    private var connectionGlyph: some View {
        if let id = projectStore.activeProjectID, projectStore.isConnected(id) {
            Image(systemName: "bolt.fill")
                .font(.system(size: 9))
                .foregroundStyle(.green)
                .accessibilityLabel(Text("Connected"))
                .help(Text("Connected — serving local tools"))
        }
    }

    /// Chevron, or a spinner while a list load is in flight, always in the same
    /// reserved width so the toolbar does not jitter.
    @ViewBuilder
    private var trailingSlot: some View {
        if projectStore.state == .loading {
            ProgressView()
                .controlSize(.small)
                .frame(width: 11, height: 11)
        } else {
            Image(systemName: "chevron.down")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.secondary)
                .frame(width: 11, height: 11)
        }
    }

    // MARK: - Derived

    /// Active project name (runtime data), or the state that stands in for it —
    /// all on the localizable path.
    private var triggerTitle: Text {
        if let name = projectStore.activeProjectName?
            .trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty
        {
            return Text(verbatim: name)
        }
        switch projectStore.state {
        case .idle, .loading:
            return Text("Loading projects…")
        case .signedOut:
            return Text("Signed out")
        case .error:
            return Text("Couldn't load projects")
        case .loaded:
            return Text("No projects")
        }
    }

    private var activeOrganizationName: String? {
        guard let project = projectStore.activeProject else { return nil }
        return projectStore.organizationName(for: project)
    }

    /// VoiceOver value for the trigger. The accessible NAME stays the stable
    /// action ("Switch project"); the live context travels as the value.
    private var accessibilityValue: Text {
        if projectStore.projects.isEmpty {
            switch projectStore.state {
            case .idle, .loading:
                return Text("Loading projects…")
            case .signedOut:
                return Text("Signed out")
            case .error:
                return Text("Couldn't load projects")
            case .loaded:
                return Text("No projects")
            }
        }
        guard let summary = contextSummary else {
            return Text("Select project")
        }
        return Text(verbatim: summary)
    }

    /// "Org, Project" for the active context, or the project alone when its
    /// organisation is unresolvable. Both parts are runtime data.
    private var contextSummary: String? {
        guard let name = projectStore.activeProjectName?
            .trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty
        else {
            return nil
        }
        guard let org = activeOrganizationName else { return name }
        return "\(org), \(name)"
    }

    /// Tooltip for the trigger: the server's error text when the load failed
    /// (data, shown verbatim), otherwise the localized action label.
    private var switchHelp: Text {
        if case let .error(message) = projectStore.state {
            return Text(verbatim: message)
        }
        return Text("Switch project")
    }

    /// A failed switch keeps the loaded list usable but must stay visible, so
    /// the trigger carries a warning glyph rather than only a tooltip.
    private var hasError: Bool {
        if case .error = projectStore.state {
            return true
        }
        return false
    }

    // MARK: - Loading

    private func ensureProjectsLoaded() async {
        guard accountStore.isEffectivelySignedIn,
              projectStore.projects.isEmpty,
              projectStore.state != .loading else { return }
        switch projectStore.state {
        case .idle, .error, .signedOut:
            // `.signedOut` retries through `loadProjects`, which itself attempts
            // the session repair hook before falling back to the signed-out UI.
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        case .loading, .loaded:
            return
        }
    }
}
