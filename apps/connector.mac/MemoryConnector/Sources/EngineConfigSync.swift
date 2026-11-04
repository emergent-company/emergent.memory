import Foundation

/// The shared engine config location.
///
/// Engine config is materialized by the connector CLI (`projects use`), which
/// mints/reuses the connected project's token. This type only exposes the path
/// the app and the lifecycle policy read from.
@MainActor
enum EngineConfigSync {
    nonisolated static let configURL = URL(fileURLWithPath: EngineManager.defaultConfigPath)
}
