## 1. Config + guard

- [x] 1.1 Add `dialAllowlist` (hosts + CIDRs) and `newDialAllowlist` to `apps/web-ui/gateway/netguard.go`
- [x] 1.2 Apply the allowlist in `ssrfProtectedDialContext`; keep the default strict
- [x] 1.3 Add `Config.ProviderProbeAllow` from `PROVIDER_PROBE_ALLOW` (`apps/web-ui/gateway/config.go`)
- [x] 1.4 Build the probe client from config in `apps/web-ui/gateway/main.go`

## 2. Docs + tests

- [x] 2.1 Document `PROVIDER_PROBE_ALLOW` in `apps/web-ui/.env.example`
- [x] 2.2 Unit tests: allowlisted host, allowlisted CIDR, unlisted private blocked, metadata still blocked, invalid entries dropped
- [x] 2.3 `go build ./... && go test ./... && task lint` in `apps/web-ui/gateway`
