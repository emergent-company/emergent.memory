package backups

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ErrInvalidArchive is returned when a backup archive fails structure,
// manifest, or checksum validation. Callers can errors.Is(err, ErrInvalidArchive)
// to distinguish a malformed upload from an infrastructure failure.
var ErrInvalidArchive = errors.New("invalid backup archive")

// Importer downloads and validates a backup archive so it can be applied by the
// Restorer. Validation mirrors the Creator's checksum contract exactly:
//
//   - checksums.database = sha256 over the concatenated database/*.ndjson bytes
//     in ZIP order
//   - checksums.files    = sha256 over the concatenated files/* payloads in ZIP
//     order
//   - checksums.manifest = sha256 over json.Marshal(manifest) with
//     Checksums.Manifest cleared
type Importer struct {
	db      *bun.DB
	storage *storage.Service
	log     *slog.Logger
}

// NewImporter creates a new archive importer.
func NewImporter(db *bun.DB, storage *storage.Service, log *slog.Logger) *Importer {
	return &Importer{
		db:      db,
		storage: storage,
		log:     log.With(slog.String("component", "backups.importer")),
	}
}

// FileItem is a re-uploadable file payload paired back with its real MinIO
// storage key (manifest map keys are the true keys, not the sanitized path).
type FileItem struct {
	StorageKey string
	Filename   string
	MimeType   *string
	Payload    []byte
}

// Archive is a validated, fully-loaded backup archive.
type Archive struct {
	manifest  Manifest
	project   map[string]any
	tables    []string          // present database/<name>.ndjson entries, in ZIP order
	tableData map[string][]byte // raw NDJSON bytes per table
	files     []FileItem
}

// Manifest returns the archive manifest.
func (a *Archive) Manifest() Manifest { return a.manifest }

// Tables returns the names of database tables present in the archive.
func (a *Archive) Tables() []string { return a.tables }

// HasTable reports whether a table's NDJSON is present in the archive.
func (a *Archive) HasTable(name string) bool {
	_, ok := a.tableData[name]
	return ok
}

// Rows parses a table's NDJSON into JSON objects keyed by real column name.
// Numbers are decoded as json.Number so integers stay integral through the
// generic INSERT builder.
func (a *Archive) Rows(name string) ([]map[string]any, error) {
	raw, ok := a.tableData[name]
	if !ok {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var rows []map[string]any
	for {
		var row map[string]any
		if err := dec.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("parse %s NDJSON: %w", name, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Files returns the re-uploadable file payloads.
func (a *Archive) Files() []FileItem { return a.files }

// ProjectConfig returns the exported kb.projects row for this archive.
func (a *Archive) ProjectConfig() map[string]any { return a.project }

// Load downloads an archive, validates its checksums, and materializes it.
func (i *Importer) Load(ctx context.Context, storageKey string) (*Archive, error) {
	rc, err := i.storage.Download(ctx, storageKey)
	if err != nil {
		return nil, fmt.Errorf("download backup archive: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read backup archive: %w", err)
	}

	return i.Parse(data)
}

// Parse validates a raw backup archive (ZIP bytes) and materializes it.
// Structure, manifest, and checksum validation failures are wrapped with
// ErrInvalidArchive so callers can errors.Is(err, ErrInvalidArchive).
func (i *Importer) Parse(data []byte) (*Archive, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: open backup archive: %v", ErrInvalidArchive, err)
	}

	archive, err := i.validate(zr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	return archive, nil
}

func (i *Importer) validate(zr *zip.Reader) (*Archive, error) {
	archive := &Archive{
		tableData: make(map[string][]byte),
	}

	dbHash := sha256.New()
	filesHash := sha256.New()

	var rawFiles []struct {
		name    string
		payload []byte
	}

	for _, f := range zr.File {
		name := f.Name
		switch {
		case name == "manifest.json":
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open manifest: %w", err)
			}
			err = json.NewDecoder(rc).Decode(&archive.manifest)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("parse manifest: %w", err)
			}

		case name == "project/config.json":
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open project config: %w", err)
			}
			payload, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read project config: %w", readErr)
			}
			var project map[string]any
			if err := json.Unmarshal(payload, &project); err != nil {
				return nil, fmt.Errorf("parse project config: %w", err)
			}
			archive.project = project

		case strings.HasPrefix(name, "database/"):
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s: %w", name, err)
			}
			payload, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read %s: %w", name, readErr)
			}
			_, _ = dbHash.Write(payload)
			tableName := strings.TrimSuffix(strings.TrimPrefix(name, "database/"), ".ndjson")
			if _, dup := archive.tableData[tableName]; !dup {
				archive.tables = append(archive.tables, tableName)
			}
			archive.tableData[tableName] = payload

		case strings.HasPrefix(name, "files/"):
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s: %w", name, err)
			}
			payload, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read %s: %w", name, readErr)
			}
			_, _ = filesHash.Write(payload)
			rawFiles = append(rawFiles, struct {
				name    string
				payload []byte
			}{name: name, payload: payload})
		}
	}

	if archive.manifest.Version == "" {
		return nil, fmt.Errorf("backup archive is missing manifest.json")
	}

	// Validate database + files checksums.
	if got := hex.EncodeToString(dbHash.Sum(nil)); got != archive.manifest.Checksums.Database {
		return nil, fmt.Errorf("backup archive database checksum mismatch: got %s want %s", got, archive.manifest.Checksums.Database)
	}
	if got := hex.EncodeToString(filesHash.Sum(nil)); got != archive.manifest.Checksums.Files {
		return nil, fmt.Errorf("backup archive files checksum mismatch: got %s want %s", got, archive.manifest.Checksums.Files)
	}

	// Validate the manifest self-checksum: hash of the compact marshaled
	// manifest with the manifest checksum field cleared.
	payload := archive.manifest
	payload.Checksums.Manifest = ""
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest for checksum: %w", err)
	}
	sum := sha256.Sum256(payloadBytes)
	if got := hex.EncodeToString(sum[:]); got != archive.manifest.Checksums.Manifest {
		return nil, fmt.Errorf("backup archive manifest checksum mismatch: got %s want %s", got, archive.manifest.Checksums.Manifest)
	}

	// Validate the source project identity: an imported archive must carry a
	// parseable source project id and a non-empty name, and the id must agree
	// with the exported project/config.json id when that config is present.
	if archive.manifest.Project.ID == "" {
		return nil, fmt.Errorf("backup archive manifest is missing project id")
	}
	if _, err := uuid.Parse(archive.manifest.Project.ID); err != nil {
		return nil, fmt.Errorf("backup archive manifest project id is not a valid UUID: %v", err)
	}
	if strings.TrimSpace(archive.manifest.Project.Name) == "" {
		return nil, fmt.Errorf("backup archive manifest is missing project name")
	}
	if cfgID, ok := archive.project["id"]; ok {
		id, ok := cfgID.(string)
		if !ok || id != archive.manifest.Project.ID {
			return nil, fmt.Errorf("backup archive manifest project id does not match project/config.json")
		}
	}

	// Pair files/* ZIP entries back to their real storage keys via the
	// manifest files map (keys are the true MinIO storage keys).
	if err := matchArchiveFiles(archive, rawFiles); err != nil {
		return nil, err
	}

	// Filter the manifest files map down to what we actually have.
	i.log.Info("backup archive validated",
		slog.Int("tables", len(archive.tables)),
		slog.Int("files", len(archive.files)),
	)

	return archive, nil
}

// matchArchiveFiles pairs the files/* entries with the manifest storage keys.
// Normally each entry name equals "files/" + sanitize(storageKey). When the
// exporter had to disambiguate two keys that sanitize identically it appends a
// "-<hash>" suffix, so leftovers are paired in sorted order; any residual
// mismatch is treated as an invalid archive.
func matchArchiveFiles(archive *Archive, rawFiles []struct {
	name    string
	payload []byte
}) error {
	type entry struct {
		name    string
		payload []byte
	}

	byName := make(map[string]entry, len(rawFiles))
	for _, rf := range rawFiles {
		e := entry{name: rf.name, payload: rf.payload}
		if _, exists := byName[e.name]; exists {
			return fmt.Errorf("backup archive contains duplicate files entry %q", e.name)
		}
		byName[e.name] = e
	}

	keys := make([]string, 0, len(archive.manifest.Files))
	for k := range archive.manifest.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unmatchedKeys []string
	for _, key := range keys {
		want := "files/" + sanitizeFileKey(key)
		e, ok := byName[want]
		if !ok {
			unmatchedKeys = append(unmatchedKeys, key)
			continue
		}
		delete(byName, want)
		archive.files = append(archive.files, fileItemFromEntry(archive, key, e.payload))
	}

	// Leftover entries must exactly balance leftover manifest keys.
	if len(byName) != len(unmatchedKeys) {
		return fmt.Errorf("backup archive file manifest mismatch: %d entries vs %d keys",
			len(byName), len(unmatchedKeys))
	}

	// Pair leftovers deterministically (sanitize collisions). This only ever
	// runs when the exporter disambiguated duplicate sanitized paths.
	var remainingEntries []entry
	for _, e := range byName {
		remainingEntries = append(remainingEntries, e)
	}
	sort.Slice(remainingEntries, func(i, j int) bool {
		return remainingEntries[i].name < remainingEntries[j].name
	})
	sort.Strings(unmatchedKeys)
	for i, e := range remainingEntries {
		key := unmatchedKeys[i]
		archive.files = append(archive.files, fileItemFromEntry(archive, key, e.payload))
	}

	return nil
}

func fileItemFromEntry(archive *Archive, storageKey string, payload []byte) FileItem {
	meta := archive.manifest.Files[storageKey]
	return FileItem{
		StorageKey: storageKey,
		Filename:   meta.Filename,
		MimeType:   meta.MimeType,
		Payload:    payload,
	}
}
