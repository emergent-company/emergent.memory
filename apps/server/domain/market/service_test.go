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
func (f *fakeClient) ReportInstall(context.Context, string, ReportRequest, string) error {
	f.reports++
	return nil
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
	client.exchange.Listing = testListing([]string{"search"})
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)

	_, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil)
	expectCode(t, err, CodeUndeclaredTools)
	if importer.calls != 0 {
		t.Fatalf("undeclared tools reached apply")
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
	client.exchange.Listing = testListing([]string{"search"})
	importer := &fakeImporter{}
	svc := newTestService(t, client, &fakeStore{}, importer)
	if _, err := svc.Apply(context.Background(), "proj-1", "user-1", testIssuer, "code", nil); err != nil {
		t.Fatalf("declared tool should be allowed: %v", err)
	}
	if importer.calls != 1 {
		t.Fatalf("import calls = %d, want 1", importer.calls)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
