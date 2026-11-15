package market

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/blueprints"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/internal/version"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ClientAPI is the market transport surface used by the install service.
// *Client implements it; tests substitute a fake.
type ClientAPI interface {
	IsTrusted(issuer string) bool
	Exchange(ctx context.Context, issuer, code string) (*ExchangeResponse, error)
	Version(ctx context.Context, issuer, slug, semver string) (*Version, error)
	VerifyLicense(ctx context.Context, issuer, token string) (*LicenseClaims, error)
	DownloadBundle(ctx context.Context, issuer, bundleURL, license string) ([]byte, error)
	ReportInstall(ctx context.Context, issuer string, report ReportRequest, license string) error
}

// InstallStore persists kb.market_installs provenance.
type InstallStore interface {
	Get(ctx context.Context, projectID, issuer, slug, digest string) (*Install, error)
	Upsert(ctx context.Context, install *Install) error
}

// BlueprintImporter runs the shared blueprint create/publish/apply pipeline
// against an extracted local directory. *blueprints.Service implements it.
type BlueprintImporter interface {
	ImportFromLocalDir(ctx context.Context, projectID string, req *blueprints.LocalImportRequest) (*blueprints.Blueprint, error)
}

// Service implements resolve/apply for marketplace installs.
type Service struct {
	client     ClientAPI
	blueprints BlueprintImporter
	store      InstallStore
	version    string
	log        *slog.Logger
}

// NewService wires the production dependencies.
func NewService(client *Client, bp *blueprints.Service, store *Repository, log *slog.Logger) *Service {
	return &Service{
		client:     client,
		blueprints: bp,
		store:      store,
		version:    version.Version,
		log:        log.With(logger.Scope("market.install")),
	}
}

// ListingPreview is the review payload the UI renders before applying.
type ListingPreview struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Publisher string `json:"publisher"`
	Semver    string `json:"semver"`
	Digest    string `json:"digest"`
}

// ResolveResult is the preview shown before confirmation. Resolve performs no
// download and no apply. Agents carries the version manifest's agents so the
// gateway can render the model-map review before anything is applied.
type ResolveResult struct {
	Listing          ListingPreview  `json:"listing"`
	Capabilities     Capabilities    `json:"capabilities"`
	Compat           Compat          `json:"compat"`
	Agents           []ManifestAgent `json:"agents,omitempty"`
	AlreadyInstalled bool            `json:"already_installed"`
}

// ApplyResult is the confirmed-install response.
type ApplyResult struct {
	BlueprintID string `json:"blueprint_id"`
	InstallID   string `json:"install_id"`
}

// Resolve exchanges the code, verifies the signed license and returns the
// authoritative listing facts for review. It does not download the bundle.
func (s *Service) Resolve(ctx context.Context, projectID, marketIssuer, code string) (*ResolveResult, error) {
	issuer, exchange, claims, err := s.exchange(ctx, marketIssuer, code)
	if err != nil {
		return nil, err
	}
	digest := strings.TrimSpace(CleanDigest(claims.Digest))
	listing := exchange.Listing
	if !Compatible(s.version, listing.Compat.Memory) {
		return nil, ErrCompatUnsatisfied(listing.Compat.Memory, s.version)
	}
	already, err := s.store.Get(ctx, projectID, issuer, listing.Slug, digest)
	if err != nil {
		return nil, err
	}
	var agents []ManifestAgent
	if exchange.Version != nil {
		agents = exchange.Version.Manifest.Agents
	}
	return &ResolveResult{
		Listing: ListingPreview{
			Slug:      listing.Slug,
			Title:     listing.DisplayTitle(),
			Publisher: listing.Publisher.DisplayName(),
			Semver:    listing.VersionString(),
			Digest:    digest,
		},
		Capabilities:     listing.Capabilities,
		Compat:           listing.Compat,
		Agents:           agents,
		AlreadyInstalled: already != nil && already.State == InstallStateApplied,
	}, nil
}

// Apply performs the full install: exchange, licence verify, download, digest
// verify, extraction, tool-licence check, blueprint apply with market
// provenance, install row, and a best-effort market report. A repeat install of
// the same licensed digest into the same project is idempotent. Once the
// exchange has handed out a licence, any later failure is reported to the
// market as `failed` with the stable app error code (still best-effort: a
// reporting failure never changes the install outcome).
func (s *Service) Apply(ctx context.Context, projectID, installedBy, marketIssuer, code string, overrides map[string]string) (result *ApplyResult, applyErr error) {
	issuer, exchange, claims, err := s.exchange(ctx, marketIssuer, code)
	if err != nil {
		return nil, err
	}
	defer func() {
		if applyErr == nil {
			return
		}
		report := ReportRequest{State: ReportStateFailed, ErrorCode: reportErrorCode(applyErr)}
		if rerr := s.client.ReportInstall(ctx, issuer, report, exchange.License); rerr != nil {
			s.log.Warn("market install failure report failed", logger.Error(rerr))
		}
	}()

	listing := exchange.Listing
	if !Compatible(s.version, listing.Compat.Memory) {
		return nil, ErrCompatUnsatisfied(listing.Compat.Memory, s.version)
	}
	digest := CleanDigest(claims.Digest)

	existing, err := s.store.Get(ctx, projectID, issuer, listing.Slug, digest)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.State == InstallStateApplied {
		return &ApplyResult{BlueprintID: existing.BlueprintID, InstallID: existing.InstallID}, nil
	}

	data, err := s.client.DownloadBundle(ctx, issuer, exchange.BundleURL, exchange.License)
	if err != nil {
		return nil, err
	}
	if err := VerifyDigest(digest, data); err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "memory-market-bundle-*")
	if err != nil {
		return nil, ErrMarketUnreachable("failed to create extraction directory")
	}
	defer os.RemoveAll(dir)

	if err := blueprints.ExtractCanonicalTar(bytes.NewReader(data), dir); err != nil {
		return nil, err
	}
	local, err := blueprints.LoadLocalBlueprint(dir)
	if err != nil {
		return nil, err
	}
	kind := ""
	var manifestTools []string
	if exchange.Version != nil {
		kind = exchange.Version.Kind
		manifestTools = exchange.Version.Manifest.Tools
	}
	if err := checkLicensedTools(kind, &listing, manifestTools, local); err != nil {
		return nil, err
	}

	sourceURL := blueprints.MarketSourceURL(issuer, listing.Slug)
	bp, err := s.blueprints.ImportFromLocalDir(ctx, projectID, &blueprints.LocalImportRequest{
		Root:            dir,
		Apply:           true,
		ModelOverrides:  overrides,
		Origin:          blueprints.OriginMarket,
		SourceKind:      "market",
		SourceURL:       sourceURL,
		SkillProvenance: &blueprints.SkillProvenance{Source: skills.SourceMarketplace, SourceURL: sourceURL},
	})
	if err != nil {
		return nil, err
	}

	var installedByPtr *string
	if installedBy != "" {
		installedByPtr = &installedBy
	}
	installID := exchange.InstallID
	if installID == "" {
		installID = listing.Slug + "@" + listing.VersionString()
	}
	row := &Install{
		ProjectID:    projectID,
		BlueprintID:  bp.ID,
		MarketIssuer: issuer,
		ListingSlug:  listing.Slug,
		Semver:       listing.VersionString(),
		Digest:       digest,
		InstallID:    installID,
		LicenseJTI:   claims.JTI,
		InstalledBy:  installedByPtr,
		State:        InstallStateApplied,
	}
	if err := s.store.Upsert(ctx, row); err != nil {
		return nil, err
	}

	// Reporting is best-effort: the install is already applied, and failing it
	// because a usage callback failed would be worse than a missing report. Use
	// the market's terminal state vocabulary (`installed`), never the internal
	// install-row state (`applied`).
	if rerr := s.client.ReportInstall(ctx, issuer, ReportRequest{State: ReportStateInstalled}, exchange.License); rerr != nil {
		s.log.Warn("market install report failed", logger.Error(rerr))
	}

	return &ApplyResult{BlueprintID: bp.ID, InstallID: installID}, nil
}

// reportErrorCode maps an apply failure to the stable error_code the market
// records. App errors carry the contract code; anything else is a generic
// install failure.
func reportErrorCode(err error) string {
	if code := ErrorCode(err); code != "" {
		return code
	}
	return "install_failed"
}

// exchange normalizes the issuer, redeems the code at that issuer, and verifies
// the signed licence against the same issuer's key set. The allowlist is
// checked before the outbound exchange.
func (s *Service) exchange(ctx context.Context, marketIssuer, code string) (string, *ExchangeResponse, *LicenseClaims, error) {
	issuer, err := NormalizeIssuer(marketIssuer)
	if err != nil {
		return "", nil, nil, ErrIssuerUntrusted(marketIssuer)
	}
	if !s.client.IsTrusted(issuer) {
		return "", nil, nil, ErrIssuerUntrusted(issuer)
	}
	exchange, err := s.client.Exchange(ctx, issuer, code)
	if err != nil {
		return "", nil, nil, err
	}
	if exchange == nil {
		return "", nil, nil, ErrInstallCodeInvalid("")
	}
	if strings.TrimSpace(exchange.License) == "" {
		return "", nil, nil, ErrLicenseInvalid("market did not return an install license")
	}
	claims, err := s.client.VerifyLicense(ctx, issuer, exchange.License)
	if err != nil {
		return "", nil, nil, err
	}
	licensed := CleanDigest(claims.Digest)
	if licensed == "" {
		return "", nil, nil, ErrLicenseInvalid("install license carries no archive digest")
	}
	if claimed := CleanDigest(exchange.BundleDigest()); claimed != "" && claimed != licensed {
		return "", nil, nil, ErrLicenseInvalid("install license digest does not match the exchange")
	}

	// The exchange carries card fields only. Compatibility, capabilities and the
	// tool declarations live on the immutable version document; fetch it and
	// require it to describe the same version and digest the license did.
	slug := claims.ListingSlug
	if slug == "" {
		slug = exchange.Listing.Slug
	}
	semver := claims.Semver
	if semver == "" {
		semver = exchange.Listing.VersionString()
	}
	if slug == "" || semver == "" {
		return "", nil, nil, ErrLicenseInvalid("install license names no listing or version")
	}
	version, err := s.client.Version(ctx, issuer, slug, semver)
	if err != nil {
		return "", nil, nil, err
	}
	if version != nil {
		if version.Slug != "" && version.Slug != slug {
			return "", nil, nil, ErrLicenseInvalid("market version document names a different listing")
		}
		if version.Version != "" && semver != "" && version.Version != semver {
			return "", nil, nil, ErrLicenseInvalid("market version document names a different version")
		}
		if vd := CleanDigest(version.Digest); vd != "" && vd != licensed {
			return "", nil, nil, ErrLicenseInvalid("market version document names a different digest")
		}
		exchange.Listing.Slug = slug
		exchange.Listing.Semver = semver
		exchange.Listing.Compat.Memory = version.Compat
		exchange.Listing.Capabilities = version.Manifest.Capabilities
		exchange.Listing.Tools = version.Tools
		exchange.Version = version
	}
	return issuer, exchange, claims, nil
}

// checkLicensedTools enforces the hosted rule: an mcp/agent listing may only
// deliver agents whose tools the version manifest licences (manifest tools plus
// capabilities.tools). Blueprint and bundle installs are the payload itself and
// bring their own tool graph, so they stay exempt. A kind that is neither
// recognised nor hosted stays exempt too, so an unknown future kind cannot
// start refusing real installs.
func checkLicensedTools(kind string, listing *Listing, manifestTools []string, local *blueprints.LocalBlueprint) error {
	if kind != "mcp" && kind != "agent" {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, name := range listing.LicencedTools() {
		allowed[name] = struct{}{}
	}
	for _, name := range manifestTools {
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	// Nothing declared means nothing to enforce (P0): a listing that declares no
	// tool set cannot have an agent tool refused.
	if len(allowed) == 0 {
		return nil
	}
	var undeclared []string
	seen := map[string]struct{}{}
	for _, agent := range local.Manifest.Agents {
		for _, tool := range agent.Tools {
			if tool == "" {
				continue
			}
			if _, ok := allowed[tool]; ok {
				continue
			}
			if _, dup := seen[tool]; dup {
				continue
			}
			seen[tool] = struct{}{}
			undeclared = append(undeclared, tool)
		}
	}
	if len(undeclared) > 0 {
		return ErrUndeclaredTools(undeclared)
	}
	return nil
}
