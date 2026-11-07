import SwiftUI

/// Project switcher at the leading edge of the window toolbar: a borderless
/// dropdown showing the active project with its organisation above it — the
/// same org-over-project hierarchy the web console's switcher uses, so a user
/// who works in both surfaces reads the same label in the same place.
///
/// The list itself lives in `ProjectPickerMenu` (shared with the Project page
/// card). This view only owns the trigger label and the loading/empty/error
/// states that have no list to show.
struct ProjectSwitcherView: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore

    /// Width the project name may occupy before it tail-truncates. The web
    /// console caps its trigger label the same way instead of letting a long
    /// project name widen the whole navbar.
    private static let labelMaxWidth: CGFloat = 200

    var body: some View {
        content
            .accessibilityIdentifier("toolbar.project")
            // Self-heal: if bootstrap ran before the session was restored (or
            // failed), load once when the picker appears with no projects.
            .task { await ensureProjectsLoaded() }
    }

    @ViewBuilder
    private var content: some View {
        if !accountStore.isEffectivelySignedIn {
            menu {
                statusLabel(icon: "person.crop.circle.badge.questionmark",
                            text: "Sign in to switch projects")
            }
        } else if !projectStore.projects.isEmpty {
            menu { triggerLabel }
        } else {
            switch projectStore.state {
            case .idle, .loading:
                loadingLabel
            case .signedOut:
                menu {
                    statusLabel(icon: "person.crop.circle.badge.questionmark",
                                text: "Signed out")
                }
            case .error:
                menu {
                    statusLabel(icon: "exclamationmark.triangle",
                                text: "Couldn't load projects", tint: .orange)
                }
            case .loaded:
                menu {
                    statusLabel(icon: "folder", text: "No projects")
                }
            }
        }
    }

    // MARK: - Menu chrome

    /// One presentation for every state: the same label rhythm, the same
    /// borderless menu chrome, and the same accessible name. The hand-rolled
    /// horizontal padding per state is what made the old control read as a
    /// loose glyph rather than a menu.
    private func menu(@ViewBuilder label: () -> some View) -> some View {
        Menu {
            ProjectPickerMenu()
        } label: {
            label()
        }
        .menuStyle(.button)
        .buttonStyle(.borderless)
        .menuIndicator(.hidden)
        .fixedSize()
        .accessibilityLabel(Text(AccessibleNames.project))
        .accessibilityValue(accessibilityValue)
        .help(switchHelp)
        // One switch at a time: the rows and the trigger both key off the
        // store's `.loading`, so a second click can never race the first.
        .disabled(projectStore.state == .loading)
    }

    // MARK: - Trigger label

    /// Active project: organisation caption over the project name, with the
    /// connected bolt kept only as a trailing status glyph. No leading icon —
    /// the identity is the two words, and dropping the glyph is what stops the
    /// symbol crowding the toolbar edge.
    private var triggerLabel: some View {
        switcherLabel {
            VStack(alignment: .leading, spacing: 0) {
                if let orgName = activeOrganizationName {
                    Text(verbatim: orgName)
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                HStack(spacing: 5) {
                    if hasError {
                        Image(systemName: "exclamationmark.triangle")
                            .foregroundStyle(.orange)
                    }
                    activeProjectTitle
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(1)
                        .truncationMode(.tail)
                    connectionGlyph
                    trailingSlot
                }
            }
            .frame(maxWidth: Self.labelMaxWidth, alignment: .leading)
        }
    }

    private func statusLabel(icon: String, text: LocalizedStringResource,
                             tint: Color = .secondary) -> some View
    {
        switcherLabel {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(tint)
                // The placeholder is a localization key, not runtime data.
                Text(text)
                    .foregroundStyle(tint)
                    .lineLimit(1)
                trailingSlot
            }
            .frame(maxWidth: Self.labelMaxWidth, alignment: .leading)
        }
    }

    private var loadingLabel: some View {
        switcherLabel {
            HStack(spacing: 6) {
                ProgressView().controlSize(.small)
                Text("Loading projects…")
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .frame(maxWidth: Self.labelMaxWidth, alignment: .leading)
        }
    }

    /// Single chrome entry point, so every label state shares one vertical and
    /// horizontal rhythm. The trailing chevron/spinner sits in its own fixed
    /// slot, so a switch in progress never changes the item's width.
    private func switcherLabel(@ViewBuilder content: () -> some View) -> some View {
        HStack(spacing: 5) {
            content()
        }
        .font(.subheadline)
        .padding(.horizontal, 9)
        .padding(.vertical, 2)
        .contentShape(Rectangle())
    }

    @ViewBuilder
    private var connectionGlyph: some View {
        if let id = projectStore.activeProjectID, projectStore.isConnected(id) {
            Image(systemName: "bolt.fill")
                .font(.caption2)
                .foregroundStyle(.green)
                .accessibilityLabel(Text("Connected"))
                .help(Text("Connected — serving local tools"))
        }
    }

    /// Chevron, or a spinner while a switch/list load is in flight, always in
    /// the same reserved width so the toolbar does not jitter.
    @ViewBuilder
    private var trailingSlot: some View {
        if projectStore.state == .loading, !projectStore.projects.isEmpty {
            ProgressView()
                .controlSize(.small)
                .frame(width: 12, height: 12)
        } else {
            Image(systemName: "chevron.down")
                .font(.caption2.weight(.semibold))
                .foregroundStyle(.secondary)
                .frame(width: 12, height: 12)
        }
    }

    // MARK: - Derived

    private var activeOrganizationName: String? {
        guard let project = projectStore.activeProject else { return nil }
        return projectStore.organizationName(for: project)
    }

    /// Active project name (runtime data), or a localized placeholder when none
    /// is selected — kept on the localizable path rather than folded into a
    /// verbatim string.
    private var activeProjectTitle: Text {
        if let name = projectStore.activeProjectName?
            .trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty
        {
            return Text(verbatim: name)
        }
        return Text("Select project")
    }

    /// VoiceOver value for the trigger. The accessible NAME stays the stable
    /// action ("Switch project"); the live context travels as the value, so
    /// unlike a replaced label it keeps the organisation and project name the
    /// previous control announced.
    private var accessibilityValue: Text {
        if !accountStore.isEffectivelySignedIn {
            return Text("Signed out")
        }
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
    /// the trigger carries a warning glyph (as it did before) rather than only
    /// a tooltip.
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
