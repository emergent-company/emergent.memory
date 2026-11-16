package market

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/domain/blueprints"
)

type fakeClient struct {
	trusted     bool
	exchange    *ExchangeResponse
	claims      *LicenseClaims
	version     *Version
	versionErr  error
	exchangeErr error
	verifyErr   error
	bundle      []byte
	downloads   int
	exchanges   int
	reports     int
	reportReqs  []ReportRequest
	reportErr   error
}

func (f *fakeClient) IsTrusted(string) bool { return f.trusted }
func (f *fakeClient) Exchange(context.Context, string, string) (*ExchangeResponse, error) {
	f.exchanges++
	return f.exchange, f.exchangeErr
}
func (f *fakeClient) Version(context.Context, string, string, string) (*Version, error) {
	return f.version, f.versionErr
}
func (f *fakeClient) VerifyLicense(context.Context, string, string) (*LicenseClaims, error) {
	return f.claims, f.verifyErr
}
func (f *fakeClient) DownloadBundle(context.Context, string, string, string) ([]byte, error) {
	f.downloads++
	return f.bundle, nil
}
func (f *fakeClient) ReportInstall(_ context.Context, _ string, report ReportRequest, _ string) error {
	f.reports++
	f.reportReqs = append(f.reportReqs, report)
	return f.reportErr
}

type fakeImporter struct {
	calls int
	req   *blueprints.LocalImportRequest
}

func (f *fakeImporter) ImportFromLocalDir(_ context.Context, _ string, req *blueprints.LocalImportRequest) (*blueprints.Blueprint, error) {
	f.calls++
	f.req = req
	return &blueprints.Blueprint{ID: "bp-1", Name: "demo", Origin: blueprints.OriginMarket}, nil
}

type fakeStore struct {
	rows map[string]*Install
}

func (f *fakeStore) key(projectID, issuer, slug, digest string) string {
	return projectID + "|" + issuer + "|" + slug + "|" + CleanDigest(digest)
}
func (f *fakeStore) Get(_ context.Context, projectID, issuer, slug, digest string) (*Install, error) {
	if f.rows == nil {
		return nil, nil
	}
	return f.rows[f.key(projectID, issuer, slug, digest)], nil
}
func (f *fakeStore) Upsert(_ context.Context, install *Install) error {
	if f.rows == nil {
		f.rows = map[string]*Install{}
	}
	f.rows[f.key(install.ProjectID, install.MarketIssuer, install.ListingSlug, install.Digest)] = install
	return nil
}

func tarWithBlueprint(t *testing.T, manifest map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "blueprint.json", Mode: 0o644, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(raw); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

const testIssuer = "https://market.example"

func newTestService(t *testing.T, client ClientAPI, store InstallStore, importer BlueprintImporter) *Service {
	t.Helper()
	return &Service{client: client, blueprints: importer, store: store, version: "1.5.0", log: testLogger()}
}

func testListing(tools []string) Listing {
	return Listing{
		Slug:      "demo",
		Version:   "1.2.3",
		Kind:      "blueprint",
		Delivery:  "install",
		Compat:    Compat{Memory: ">=0.1 <99.0"},
		Publisher: Publisher{Slug: "acme", Name: "Acme"},
		Meta:      ListingDetails{Title: "Demo"},
		Capabilities: Capabilities{
			Reads:  []string{"documents"},
			Writes: []string{"graph"},
			Tools:  tools,
		},
	}
}

func happyClient(t *testing.T, bundle []byte) *fakeClient {
	t.Helper()
	sum := sha256.Sum256(bundle)
	digest := fmt.Sprintf("%x", sum)
	return &fakeClient{
		trusted: true,
		exchange: &ExchangeResponse{
			InstallID: "inst-1",
			Listing:   testListing(nil),
			License:   "license-token",
			BundleURL: testIssuer + "/bundle",
			Digest:    "sha256:" + digest,
		},
		claims: &LicenseClaims{
			Issuer:    testIssuer,
			Audience:  json.RawMessage(`"memory-install"`),
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			JTI:       "jti-1",
			Digest:    "sha256:" + digest,
		},
		bundle: bundle,
	}
}

func TestServiceResolveDoesNotDownloadOrApply(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0", "skills": []map[string]any{{"name": "demo-skill", "description": "d", "content": "c"}}})
	client := happyClient(t, bundle)
	importer := &fakeImporter{}
	store := &fakeStore{}
	svc := newTestService(t, client, store, importer)

	res, err := svc.Resolve(context.Background(), "proj-1", testIssuer, "code")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Listing.Title != "Demo" || res.Listing.Publisher != "Acme" || res.Listing.Semver != "1.2.3" {
		t.Fatalf("unexpected preview: %+v", res.Listing)
	}
	if res.AlreadyInstalled {
		t.Fatalf("should not be installed yet")
	}
	if client.downloads != 0 || importer.calls != 0 {
		t.Fatalf("resolve downloaded=%d imported=%d, want 0/0", client.downloads, importer.calls)
	}
}

func TestServiceResolveUsesVersionDocumentForCompatAndCapabilities(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0"})
	client := happyClient(t, bundle)
	// The exchange card carries only identity fields; the version document is
	// where compat, capabilities and the tool declarations live.
	client.version = &Version{
		Slug:    "demo",
		Version: "1.2.3",
		Kind:    "blueprint",
		Digest:  client.claims.Digest,
		Compat:  ">=1.0 <2.0",
		Manifest: Manifest{
			Capabilities: Capabilities{
				Reads:     []string{"documents"},
				Writes:    []string{"graph"},
				Egress:    []string{"api.acme.example"},
				MCPs:      []MCPCapability{{Slug: "search", Scopes: []string{"read"}}},
				Schedules: true,
				Model:     &CapabilityModel{Provider: "openai", Name: "gpt-5"},
			},
			Agents: []ManifestAgent{
				{Name: "writer", Model: &ManifestAgentModel{Name: "gpt-5", Provider: "openai"}},
				{Name: "reader"},
			},
		},
		Tools: []Tool{{Name: "search"}},
	}
	importer := &fakeImporter{}
	store := &fakeStore{}
	svc := newTestService(t, client, store, importer)

	res, err := svc.Resolve(context.Background(), "proj-1", testIssuer, "code")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Compat.Memory != ">=1.0 <2.0" {
		t.Fatalf("compat = %q, want the version document's range", res.Compat.Memory)
	}
	caps := res.Capabilities
	if len(caps.Reads) != 1 || caps.Reads[0] != "documents" {
		t.Fatalf("reads = %#v", caps.Reads)
	}
	if len(caps.Writes) != 1 || caps.Writes[0] != "graph" {
		t.Fatalf("writes = %#v", caps.Writes)
	}
	if len(caps.Egress) != 1 || caps.Egress[0] != "api.acme.example" {
		t.Fatalf("egress = %#v", caps.Egress)
	}
	if len(caps.MCPs) != 1 || caps.MCPs[0].Slug != "search" || len(caps.MCPs[0].Scopes) != 1 {
		t.Fatalf("mcps = %#v", caps.MCPs)
	}
	if !caps.Schedules {
		t.Fatal("schedules = false, want the version's declaration")
	}
	if caps.Model == nil || caps.Model.Provider != "openai" || caps.Model.Name != "gpt-5" {
		t.Fatalf("model = %#v", caps.Model)
	}
	if res.Listing.Semver != "1.2.3" || res.Listing.Title != "Demo" || res.Listing.Publisher != "Acme" {
		t.Fatalf("listing = %#v", res.Listing)
	}
	if len(res.Agents) != 2 || res.Agents[0].Name != "writer" || res.Agents[0].Model.Reference() != "openai/gpt-5" {
		t.Fatalf("agents = %#v", res.Agents)
	}
	if client.downloads != 0 || importer.calls != 0 {
		t.Fatalf("resolve downloaded=%d imported=%d, want 0/0", client.downloads, importer.calls)
	}
}

func TestServiceResolveRefusesVersionDigestMismatch(t *testing.T) {
	client := happyClient(t, nil)
	// A version document whose digest disagrees with the licence must be
	// refused even though the exchange's own digest matched.
	client.version = &Version{
		Slug:    "demo",
		Version: "1.2.3",
		Digest:  "sha256:" + strings.Repeat("a", 64),
		Compat:  ">=0.1 <99.0",
	}
	svc := newTestService(t, client, &fakeStore{}, &fakeImporter{})

	_, err := svc.Resolve(context.Background(), "proj-1", testIssuer, "code")
	expectCode(t, err, CodeLicenseInvalid)
}

func TestServiceApplyHappyPathRecordsProvenanceAndReports(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0", "skills": []map[string]any{{"name": "demo-skill", "description": "d", "content": "c"}}})
	client := happyClient(t, bundle)
	importer := &fakeImporter{}
	store := &fakeStore{}
	svc := newTestService(t, client, store, importer)

	res, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.BlueprintID != "bp-1" || res.InstallID != "inst-1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if importer.req == nil || importer.req.Origin != blueprints.OriginMarket {
		t.Fatalf("import did not run with origin=market: %+v", importer.req)
	}
	if importer.req.SourceURL != testIssuer+"/listings/demo" {
		t.Fatalf("source URL = %q", importer.req.SourceURL)
	}
	if importer.req.SkillProvenance == nil || importer.req.SkillProvenance.Source != "marketplace" {
		t.Fatalf("skill provenance = %+v", importer.req.SkillProvenance)
	}
	if importer.req.ModelOverrides["a"] != "b" {
		t.Fatalf("model overrides not forwarded: %+v", importer.req.ModelOverrides)
	}
	row := store.rows[store.key("proj-1", testIssuer, "demo", client.claims.Digest)]
	if row == nil || row.State != InstallStateApplied || row.BlueprintID != "bp-1" {
		t.Fatalf("install row = %+v", row)
	}
	if client.reports != 1 {
		t.Fatalf("reports = %d, want 1", client.reports)
	}
	if len(client.reportReqs) != 1 || client.reportReqs[0].State != ReportStateInstalled || client.reportReqs[0].ErrorCode != "" {
		t.Fatalf("report requests = %+v", client.reportReqs)
	}
}

func TestServiceApplyIsIdempotentForSameDigest(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0"})
	client := happyClient(t, bundle)
	importer := &fakeImporter{}
	store := &fakeStore{}
	store.rows = map[string]*Install{
		store.key("proj-1", testIssuer, "demo", client.claims.Digest): {
			ProjectID: "proj-1", BlueprintID: "bp-existing", MarketIssuer: testIssuer,
			ListingSlug: "demo", Digest: CleanDigest(client.claims.Digest), State: InstallStateApplied,
		},
	}
	svc := newTestService(t, client, store, importer)
	res, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.BlueprintID != "bp-existing" {
		t.Fatalf("result = %+v", res)
	}
	if client.downloads != 0 || importer.calls != 0 {
		t.Fatalf("repeat install downloaded=%d imported=%d, want 0/0", client.downloads, importer.calls)
	}
}

func TestServiceApplyRefusesDigestMismatch(t *testing.T) {
	good := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0"})
	client := happyClient(t, good)
	client.bundle = []byte("tampered bytes")
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)
	_, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil)
	expectCode(t, err, CodeDigestMismatch)
	if importer.calls != 0 {
		t.Fatalf("tampered bundle reached apply")
	}
}

func TestServiceRefusesUntrustedIssuerBeforeExchange(t *testing.T) {
	client := happyClient(t, nil)
	client.trusted = false
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)

	_, err := svc.Resolve(context.Background(), "proj-1", "https://evil.example", "code")
	expectCode(t, err, CodeIssuerUntrusted)
	_, err = svc.Apply(context.Background(), "proj-1", "user-1", "https://evil.example", "code", nil)
	expectCode(t, err, CodeIssuerUntrusted)
	if client.exchanges != 0 || importer.calls != 0 {
		t.Fatalf("untrusted issuer made %d exchanges / %d imports", client.exchanges, importer.calls)
	}
}

func TestServiceRefusesIncompatibleListing(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0"})
	client := happyClient(t, bundle)
	client.exchange.Listing.Compat.Memory = ">=2.0 <3.0"
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)

	_, err := svc.Resolve(context.Background(), "proj-1", testIssuer, "code")
	expectCode(t, err, CodeCompat)
	_, err = svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil)
	expectCode(t, err, CodeCompat)
	if importer.calls != 0 {
		t.Fatalf("incompatible listing reached apply")
	}
}

func TestServiceRefusesUndeclaredTools(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{
		"name":    "demo",
		"version": "1.0.0",
		"agents": []map[string]any{{
			"name":  "agent",
			"tools": []string{"search", "danger"},
		}},
	})
	client := happyClient(t, bundle)
	// The licensed set is the version manifest's tools (plus capabilities.tools).
	// mcp/agent installs enforce it; blueprint/bundle payloads stay exempt.
	client.version = &Version{
		Slug:    "demo",
		Version: "1.2.3",
		Kind:    "mcp",
		Digest:  client.claims.Digest,
		Compat:  ">=0.1 <99.0",
		Manifest: Manifest{
			Tools: []string{"search"},
		},
	}
	client.exchange.Listing = testListing(nil)
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)

	_, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil)
	expectCode(t, err, CodeUndeclaredTools)
	if importer.calls != 0 {
		t.Fatalf("undeclared tools reached apply")
	}
	// The failure is reported as `failed` with the stable contract code.
	if len(client.reportReqs) != 1 || client.reportReqs[0].State != ReportStateFailed || client.reportReqs[0].ErrorCode != CodeUndeclaredTools {
		t.Fatalf("failure report = %+v", client.reportReqs)
	}
}

func TestServiceAllowsDeclaredTools(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{
		"name":    "demo",
		"version": "1.0.0",
		"agents": []map[string]any{{
			"name":  "agent",
			"tools": []string{"search"},
		}},
	})
	client := happyClient(t, bundle)
	client.version = &Version{
		Slug:    "demo",
		Version: "1.2.3",
		Kind:    "agent",
		Digest:  client.claims.Digest,
		Compat:  ">=0.1 <99.0",
		Manifest: Manifest{
			Tools: []string{"search"},
		},
	}
	client.exchange.Listing = testListing(nil)
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)
	if _, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil); err != nil {
		t.Fatalf("declared tool should be allowed: %v", err)
	}
	if importer.calls != 1 {
		t.Fatalf("import calls = %d, want 1", importer.calls)
	}
}

func TestServiceBlueprintKindStaysExemptFromToolLicence(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{
		"name":    "demo",
		"version": "1.0.0",
		"agents": []map[string]any{{
			"name":  "agent",
			"tools": []string{"search", "danger"},
		}},
	})
	client := happyClient(t, bundle)
	client.version = &Version{
		Slug:    "demo",
		Version: "1.2.3",
		Kind:    "blueprint",
		Digest:  client.claims.Digest,
		Compat:  ">=0.1 <99.0",
		Manifest: Manifest{
			Tools: []string{"search"},
		},
	}
	client.exchange.Listing = testListing(nil)
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)
	if _, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil); err != nil {
		t.Fatalf("blueprint payload must stay exempt: %v", err)
	}
	if importer.calls != 1 {
		t.Fatalf("import calls = %d, want 1", importer.calls)
	}
}

func TestServiceReportFailureIsLoggedAndDoesNotUnapply(t *testing.T) {
	bundle := tarWithBlueprint(t, map[string]any{"name": "demo", "version": "1.0.0", "skills": []map[string]any{{"name": "demo-skill", "description": "d", "content": "c"}}})
	client := happyClient(t, bundle)
	client.reportErr = fmt.Errorf("market callback unavailable")
	importer := &fakeImporter{}
	store := &fakeStore{}
	var logs strings.Builder
	svc := &Service{client: client, blueprints: importer, store: store, version: "1.5.0",
		log: slog.New(slog.NewTextHandler(&logs, nil))}

	res, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil)
	if err != nil {
		t.Fatalf("report failure must not fail the install: %v", err)
	}
	if res.BlueprintID != "bp-1" {
		t.Fatalf("result = %+v", res)
	}
	row := store.rows[store.key("proj-1", testIssuer, "demo", client.claims.Digest)]
	if row == nil || row.State != InstallStateApplied {
		t.Fatalf("install row = %+v, want applied", row)
	}
	if importer.calls != 1 {
		t.Fatalf("import calls = %d, want 1", importer.calls)
	}
	if !strings.Contains(logs.String(), "market install report failed") {
		t.Fatalf("report failure was not logged: %s", logs.String())
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
