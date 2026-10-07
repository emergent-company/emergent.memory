import SwiftUI

/// Settings screen for the Memory connection.
///
/// Edits a local copy of ``MemoryConfig`` and persists it with `save()`.
/// `MemoryConfig` is read once when `MemorySessionController` is created, so
/// saved values apply on the next launch — the screen states that instead of
/// attempting a runtime reconfiguration. Saving also refreshes the agent
/// store, so a changed control-plane URL or API key is picked up by the
/// picker right away.
struct MemorySettingsView: View {
    @EnvironmentObject private var agentStore: AgentStore
    @EnvironmentObject private var projectStore: MemoryProjectStore
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    /// Presents the setup-QR scanner (owned by `AppShellView`).
    var onAddProject: () -> Void = {}

    /// Editable copy of the current configuration. Loaded once from
    /// `UserDefaults`; `save()` writes the values back.
    @State private var draft = MemoryConfig()
    /// Set after a successful save so the user sees a confirmation.
    @State private var savedMessage: LocalizedStringKey?
    /// Bumped on a successful save to drive the save haptic.
    @State private var saveFeedback = 0
    @FocusState private var focusedField: Field?

    private enum Field: Hashable {
        case serverURL
        case tokenEndpoint
        case apiKey
        case apiBaseURL
        case exitKeywords
        case agentConnectTimeout
        case chimeSoundID
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 5 * .grid) {
                header()
                MemoryProjectsSection(onAddProject: onAddProject)
                connectionSection()
                sessionSection()
                advancedSection()
                if let savedMessage {
                    savedNote(savedMessage)
                }
            }
            .padding(4 * .grid)
            .padding(.bottom, 2 * .grid)
            // Constrain the form to a comfortable reading width and centre it
            // on regular-width devices (iPad).
            .frame(maxWidth: horizontalSizeClass == .regular ? 128 * .grid : .infinity)
            .frame(maxWidth: .infinity)
        }
        .background(.bg1)
        .animation(.default, value: savedMessage)
        .onTapGesture {
            focusedField = nil
        }
        .onReceive(NotificationCenter.default.publisher(for: .memoryConfigChanged)) { _ in
            draft = MemoryConfig()
        }
        #if os(iOS)
        // A "Done" key so number-pad fields (no return key) can be dismissed.
        .toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button("settings.done") { focusedField = nil }
            }
        }
        #endif
        .safeAreaInset(edge: .bottom, content: saveBar)
        #if os(iOS)
            .sensoryFeedback(.success, trigger: saveFeedback)
        #endif
    }

    // MARK: - Header

    /// This screen is the Settings tab root, so it has no dismiss affordance.
    private func header() -> some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Text("settings.title")
                .font(MemoryFont.screenTitle)
                .foregroundStyle(.fg0)
            Text("settings.intro")
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
        }
    }

    // MARK: - Sections

    private func connectionSection() -> some View {
        sectionCard {
            sectionTitle("settings.section.connection")
            inputField("settings.serverURL", text: $draft.serverURL, field: .serverURL)
            inputField("settings.tokenEndpoint", text: $draft.tokenEndpoint, field: .tokenEndpoint)
            inputField("settings.apiKey", text: $draft.apiKey, field: .apiKey, isSecure: true)
            fieldFooter("settings.apiKey.footer")
            inputField("settings.apiBaseURL", text: $draft.apiBaseURL, field: .apiBaseURL)
            fieldFooter("settings.apiBaseURL.footer")
            fieldFooter("settings.section.connection.footer")
        }
    }

    private func sessionSection() -> some View {
        sectionCard {
            sectionTitle("settings.section.session")
            inputField("settings.exitKeywords", text: exitKeywordsText, field: .exitKeywords)
            fieldFooter("settings.exitKeywords.footer")
        }
    }

    private func advancedSection() -> some View {
        sectionCard {
            sectionTitle("settings.section.advanced")
            numberField("settings.agentConnectTimeout", value: connectTimeoutSeconds, field: .agentConnectTimeout)
            fieldFooter("settings.agentConnectTimeout.footer")
            numberField("settings.chimeSoundID", value: chimeSoundValue, field: .chimeSoundID)
            fieldFooter("settings.chimeSoundID.footer")
            fieldFooter("settings.advanced.footer")
        }
    }

    /// One inset card grouping a form section.
    private func sectionCard(@ViewBuilder _ content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 3 * .grid) {
            content()
        }
        .padding(4 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusMedium))
    }

    private func sectionTitle(_ title: LocalizedStringKey) -> some View {
        Text(title)
            .font(MemoryFont.metaEmphasis)
            .foregroundStyle(.fg3)
    }

    private func fieldFooter(_ text: LocalizedStringKey) -> some View {
        Text(text)
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
    }

    // MARK: - Fields

    private func inputField(
        _ title: LocalizedStringKey,
        text: Binding<String>,
        field: Field,
        isSecure: Bool = false
    ) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(title)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
            Group {
                if isSecure {
                    SecureField(title, text: text)
                } else {
                    TextField(title, text: text)
                }
            }
            .font(MemoryFont.bodySmall)
            .foregroundStyle(.fg1)
            .focused($focusedField, equals: field)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            #if os(visionOS)
                .textFieldStyle(.roundedBorder)
            #else
                .textFieldStyle(.plain)
                .padding(3 * .grid)
                .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
            #endif
        }
    }

    private func numberField(
        _ title: LocalizedStringKey,
        value: Binding<Int>,
        field: Field
    ) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(title)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
            TextField(title, value: value, format: .number)
                .font(MemoryFont.bodySmall)
                .foregroundStyle(.fg1)
                .focused($focusedField, equals: field)
            #if os(iOS)
                .keyboardType(.numberPad)
            #endif
            #if os(visionOS)
            .textFieldStyle(.roundedBorder)
            #else
            .textFieldStyle(.plain)
            .padding(3 * .grid)
            .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
            #endif
        }
    }

    // MARK: - Bindings

    /// `exitKeywords` is stored as an array; the screen edits it as one
    /// comma-separated string.
    private var exitKeywordsText: Binding<String> {
        Binding(
            get: { draft.exitKeywords.joined(separator: ", ") },
            set: { newValue in
                draft.exitKeywords = newValue
                    .split(separator: ",")
                    .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
                    .filter { !$0.isEmpty }
            }
        )
    }

    private var connectTimeoutSeconds: Binding<Int> {
        Binding(
            get: { Int(draft.agentConnectTimeout) },
            set: { draft.agentConnectTimeout = TimeInterval(max(0, $0)) }
        )
    }

    private var chimeSoundValue: Binding<Int> {
        Binding(
            get: { Int(draft.chimeSoundID) },
            set: { draft.chimeSoundID = UInt32(clamping: max(0, $0)) }
        )
    }

    // MARK: - Save

    private func saveBar() -> some View {
        Button {
            save()
        } label: {
            HStack {
                Spacer()
                Text("settings.save")
                Spacer()
            }
            .frame(height: 11 * .grid)
        }
        #if os(visionOS)
        .buttonStyle(.borderedProminent)
        #else
        .buttonStyle(ProminentButtonStyle())
        #endif
        .padding(.horizontal, 4 * .grid)
        .padding(.vertical, 2 * .grid)
        .background(.bg1)
    }

    private func savedNote(_ message: LocalizedStringKey) -> some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.fgAccent)
            Text(message)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
        }
        .padding(3 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        .transition(.opacity)
    }
}

extension MemorySettingsView {
    private func save() {
        focusedField = nil
        // Session-only fields (exit keywords, timeout, chime) live only in
        // MemoryConfig, so persist the full draft first.
        draft.save()
        // Then fold the connection fields into the active project so the
        // Projects list and switcher stay in step with what was saved.
        persistConnectionFromDraft()
        // The agent picker and the session log read config once per call, so
        // reload the agent list against the newly saved control-plane URL and
        // API key.
        Task { @MainActor in
            await agentStore.load()
        }
        savedMessage = "settings.saved"
        saveFeedback += 1
    }

    /// Writes the draft's connection fields onto the active project (creating
    /// a legacy identity-less one when nothing is connected yet).
    private func persistConnectionFromDraft() {
        var connection = projectStore.activeConnection ?? MemoryProjectConnection(
            projectID: "",
            orgID: "",
            projectName: "",
            serverURL: draft.serverURL,
            tokenEndpoint: draft.tokenEndpoint,
            apiBaseURL: draft.apiBaseURL,
            apiKey: draft.apiKey,
            ttsStrategy: draft.ttsStrategy
        )
        connection.serverURL = draft.serverURL
        connection.tokenEndpoint = draft.tokenEndpoint
        connection.apiBaseURL = draft.apiBaseURL
        connection.apiKey = draft.apiKey
        connection.ttsStrategy = draft.ttsStrategy
        projectStore.upsert(connection)
        projectStore.setActive(projectID: connection.projectID)
    }
}

#Preview {
    MemorySettingsView()
        .environmentObject(AgentStore())
        .environmentObject(MemoryProjectStore())
}
