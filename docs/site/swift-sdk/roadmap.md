# Roadmap

## Current state

The Swift integration is currently implemented as an embedded HTTP client inside the **Emergent Mac app** (`emergent-company/emergent.memory.mac`):

| File | Role |
|------|------|
| `Emergent/Core/EmergentAPIClient.swift` | `@MainActor ObservableObject` wrapping the REST API |
| `Emergent/Core/Models.swift` | Codable model types |

This layer is production-quality and sufficient for the Mac app. It is not distributed as a reusable Swift Package.

---

## Planned: `EmergentSwiftSDK` Swift Package

A formal Swift Package Manager library is planned. It is tracked at **[emergent-company/emergent#49](https://github.com/emergent-company/emergent/issues/49)**.

### Design

The package will live at `EmergentSwiftSDK/` in the `emergent-company/emergent.memory.mac` repo:

```
EmergentSwiftSDK/
├── Package.swift
├── Sources/
│   └── EmergentSwiftSDK/
│       ├── EmergentClient.swift       # Primary entry point
│       ├── Auth/
│       ├── Services/
│       │   ├── ProjectsService.swift
│       │   ├── GraphService.swift
│       │   ├── DocumentsService.swift
│       │   └── ...
│       └── Models/
└── Tests/
    └── EmergentSwiftSDKTests/
```

### Distribution

The package will be available via Swift Package Manager:

```swift
// Package.swift
.package(
    url: "https://github.com/emergent-company/emergent.memory.mac",
    from: "1.0.0"
)
```

### CGO bridge option

For scenarios requiring access to the full Go SDK feature set (e.g., streaming, complex graph traversals), the roadmap includes an optional CGO bridge layer that links against a pre-built `libemergent.a` static library. This is strictly optional — the pure-Swift HTTP layer will cover the vast majority of use cases.

```
libemergent.a  ←  cross-compiled from Go SDK via CGO
      ↑
EmergentSwiftSDK/Sources/EmergentBridge/  (optional)
      ↑
EmergentSwiftSDK/Sources/EmergentSwiftSDK/  (public API)
```

### Status

| Component | Status |
|-----------|--------|
| `EmergentAPIClient` (Mac app embedded) | Stable, production |
| `EmergentSwiftSDK/` Swift Package stub | Not yet created |
| CGO bridge (`libemergent.a`) | Design only |

Until the Swift Package is available, use `EmergentAPIClient` directly by copying or linking `Emergent/Core/EmergentAPIClient.swift` and `Emergent/Core/Models.swift` into your project.
