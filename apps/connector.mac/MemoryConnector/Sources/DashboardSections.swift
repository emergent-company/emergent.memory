import SwiftUI

// MARK: - Project card

/// The dashboard's project identity card: name, organisation, refresh button,
/// and the expandable project-info ("About") block.
struct DashboardProjectCard: View {
    let snapshot: DashboardSnapshot
    let projectName: String
    let organizationName: String?
    let isRefreshing: Bool
    /// The active project id, used only to reset the expand toggle on switch.
    let projectID: String?
    let onRefresh: () -> Void

    @State private var infoExpanded = false

    var body: some View {
        ConnectorCard {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 14) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(Color.memoryPrimary.opacity(0.18))
                            .frame(width: 44, height: 44)
                        Image(systemName: "shippingbox")
                            .font(.title3.weight(.medium))
                            .foregroundStyle(Color.memoryPrimary)
                    }

                    VStack(alignment: .leading, spacing: 4) {
                        Text(projectName)
                            .font(.title2.weight(.semibold))
                            .lineLimit(1)
                        // Organisation line is shown only when a name resolves;
                        // never a placeholder like "Unknown organisation".
                        if let organizationName {
                            HStack(alignment: .firstTextBaseline, spacing: 5) {
                                Image(systemName: "building.2")
                                    .font(.caption)
                                // Never tail-truncate the organisation: wrap to
                                // as many lines as needed.
                                Text(organizationName)
                                    .font(.subheadline)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            .foregroundStyle(.secondary)
                        }
                    }

                    Spacer(minLength: 0)

                    if isRefreshing {
                        ProgressView().controlSize(.small)
                    } else {
                        Button(action: onRefresh) {
                            Label("Refresh", systemImage: "arrow.clockwise")
                        }
                        .buttonStyle(.borderless)
                    }
                }

                if let info = snapshot.projectInfo, !info.isEmpty {
                    Divider()
                    projectInfoView(info)
                }
            }
        }
        .onChange(of: projectID) { _, _ in
            infoExpanded = false
        }
    }

    private func projectInfoView(_ info: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("About")
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(info)
                .font(.callout)
                .lineLimit(infoExpanded ? nil : 3)
                .fixedSize(horizontal: false, vertical: true)
            if info.count > 180 {
                Button(infoExpanded ? "Show less" : "Show more") {
                    withAnimation(.easeInOut(duration: 0.15)) { infoExpanded.toggle() }
                }
                .buttonStyle(.link)
                .font(.caption)
            }
        }
    }
}

// MARK: - Stats

/// One stat box: label, value, and an always-reserved caption line so the value
/// baseline stays aligned across tiles that do and don't have a caption.
struct DashboardStatTile: View {
    let title: LocalizedStringResource
    let systemImage: String
    let value: String
    let caption: LocalizedStringResource?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: systemImage)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(title)
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            Spacer(minLength: 0)
            Text(value)
                .font(.title.weight(.semibold))
                .fontDesign(.rounded)
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            Text(caption ?? " ")
                .font(.caption)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .frame(maxWidth: .infinity, minHeight: Self.tileHeight, maxHeight: Self.tileHeight,
               alignment: .topLeading)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(.quaternary.opacity(0.45))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(.quaternary.opacity(0.6))
        )
    }

    /// Fixed tile height so every stat box is identical regardless of content
    /// or window width.
    static let tileHeight: CGFloat = 96
}

/// The dashboard's stat grid: object/relationship/schema/document/job counts.
struct DashboardStatsGrid: View {
    let snapshot: DashboardSnapshot

    /// Fixed column count (not `.adaptive`), so boxes never reflow into a
    /// different number of columns as the window resizes.
    private static let columns = 3

    private struct StatTile: Identifiable {
        let id: String
        let title: LocalizedStringResource
        let systemImage: String
        let value: String
        let caption: LocalizedStringResource?
    }

    var body: some View {
        let tiles = statTiles
        if !tiles.isEmpty {
            LazyVGrid(
                columns: Array(repeating: GridItem(.flexible(), spacing: 12),
                               count: Self.columns),
                alignment: .leading,
                spacing: 12
            ) {
                ForEach(tiles) { tile in
                    DashboardStatTile(title: tile.title,
                                      systemImage: tile.systemImage,
                                      value: tile.value,
                                      caption: tile.caption)
                }
            }
        }
    }

    private var statTiles: [StatTile] {
        var tiles: [StatTile] = []

        if let objects = snapshot.resolvedObjectCount {
            tiles.append(StatTile(id: "objects",
                                  title: "OBJECTS",
                                  systemImage: "cube",
                                  value: objects.formatted(),
                                  caption: nil))
        }
        if let relationships = snapshot.resolvedRelationshipCount {
            tiles.append(StatTile(id: "relationships",
                                  title: "RELATIONSHIPS",
                                  systemImage: "link",
                                  value: relationships.formatted(),
                                  caption: nil))
        }
        if let schema = snapshot.schemaStats {
            tiles.append(StatTile(id: "schema",
                                  title: "SCHEMA TYPES",
                                  systemImage: "square.stack.3d.up",
                                  value: schema.totalTypes.formatted(),
                                  caption: "\(schema.totalObjects.formatted()) objects"))
        }
        if let stats = snapshot.projectStats {
            tiles.append(StatTile(id: "documents",
                                  title: "DOCUMENTS",
                                  systemImage: "doc.text",
                                  value: stats.documentCount.formatted(),
                                  caption: nil))
            tiles.append(StatTile(id: "jobs",
                                  title: "JOBS",
                                  systemImage: "gearshape.2",
                                  value: stats.runningJobs.formatted(),
                                  caption: "\(stats.queuedJobs) queued · \(stats.totalJobs) total"))
        }

        return tiles
    }
}

// MARK: - Agents

/// The dashboard's "Available Agents" card: a clickable list of the project's
/// agent definitions, or an empty-state hint.
struct DashboardAgentsCard: View {
    let agents: [AgentDefinitionSummary]
    let onSelect: (AgentDefinitionSummary) -> Void

    var body: some View {
        ConnectorCard(title: "Available Agents", systemImage: "person.2.wave.2") {
            if agents.isEmpty {
                HStack(spacing: 12) {
                    Image(systemName: "wand.and.stars")
                        .foregroundStyle(.secondary)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("No agents yet")
                            .font(.callout)
                        Text("Agents created for this project will appear here.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                }
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(agents) { agent in
                        if agent.id != agents.first?.id {
                            Divider()
                        }
                        Button {
                            onSelect(agent)
                        } label: {
                            AgentSummaryRow(agent: agent)
                        }
                        .buttonStyle(.plain)
                        .help("View agent details")
                    }
                }
            }
        }
    }
}

/// One clickable row in the Dashboard "Available Agents" card: the agent's
/// name, an enabled/disabled badge, a one-line description, and a tool hint.
/// Clicking the row opens ``AgentDetailView``. Hover paints a subtle highlight
/// so the whole row reads as a control.
struct AgentSummaryRow: View {
    let agent: AgentDefinitionSummary

    @State private var isHovering = false

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: "wand.and.stars")
                .foregroundStyle(.secondary)
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    displayName
                        .font(.callout.weight(.medium))
                        .lineLimit(1)
                    if let enabled = agent.enabled {
                        if enabled {
                            AgentStatusBadge(text: "Enabled", color: .green)
                        } else {
                            AgentStatusBadge(text: "Disabled", color: .secondary)
                        }
                    }
                    if agent.isDefault == true {
                        AgentStatusBadge(text: "Default", color: .memoryPrimary)
                    }
                }
                if let description = agent.description, !description.isEmpty {
                    Text(description)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer(minLength: 12)

            if let toolHint {
                Text(toolHint)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }

            Image(systemName: "chevron.right")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tertiary)
        }
        .frame(minHeight: 40)
        .padding(.vertical, 8)
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(.quaternary.opacity(isHovering ? 0.5 : 0))
        )
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.1)) { isHovering = hovering }
        }
    }

    private var displayName: Text {
        agent.name.isEmpty ? Text("Untitled agent") : Text(agent.name)
    }

    /// "3 tools" / "No tools" from the list endpoint's `toolCount`. The summary
    /// carries no whitelist, so nil means "not reported" and we show nothing
    /// rather than a misleading label.
    private var toolHint: String? {
        guard let count = agent.toolCount else { return nil }
        return count == 0 ? "No tools" : "\(count) \(count == 1 ? "tool" : "tools")"
    }
}
