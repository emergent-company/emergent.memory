// Package main is the entrypoint for the huma-test-suite CLI tool.
// It orchestrates document extraction testing against the Emergent platform
// using a real-world dataset from Google Drive.
//
// Usage:
//
//	huma-test --phase download [flags]
//	huma-test --phase upload [flags]
//	huma-test --phase verify [flags]
//	huma-test --phase all [flags]
//
// Environment variables (can be set in .env file):
//
//	EMERGENT_API_KEY             Emergent API key (required for upload/verify)
//	EMERGENT_SERVER_URL          Emergent server URL (default: http://mcj-emergent:3002)
//	EMERGENT_ORG_ID              Emergent org ID
//	EMERGENT_PROJECT_ID          Emergent project ID for the huma test project
//	GOOGLE_SERVICE_ACCOUNT_JSON  Path to Google service account key file (optional)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/config"
	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/drive"
	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/uploader"
	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/verifier"
)

func main() {
	// Load .env file if present (ignore error if not found)
	_ = godotenv.Load()

	// CLI flags
	phase := flag.String("phase", "download", "Phase to run: download, upload, verify, all")
	folderID := flag.String("folder-id", "16qesqkUSHJTdKZCMoZtMe9GtesDxGE0A", "Google Drive folder ID to sync from")
	cacheDir := flag.String("cache-dir", "/root/data", "Local directory to cache downloaded files")
	concurrency := flag.Int("concurrency", 3, "Number of concurrent workers for upload/verify phases")
	cleanup := flag.Bool("cleanup", false, "Remove local cache after run completes")
	flag.Parse()

	cfg := &config.Config{
		Phase:       *phase,
		FolderID:    *folderID,
		CacheDir:    *cacheDir,
		Concurrency: *concurrency,
		Cleanup:     *cleanup,

		// Emergent SDK config
		EmergentServerURL: getEnv("EMERGENT_SERVER_URL", "http://mcj-emergent:3002"),
		EmergentAPIKey:    os.Getenv("EMERGENT_API_KEY"),
		EmergentOrgID:     os.Getenv("EMERGENT_ORG_ID"),
		EmergentProjectID: os.Getenv("EMERGENT_PROJECT_ID"),

		// Google Drive config
		GoogleServiceAccountJSON: os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON"),
	}

	ctx := context.Background()

	switch cfg.Phase {
	case "download":
		if err := runDownload(ctx, cfg); err != nil {
			log.Fatalf("Download phase failed: %v", err)
		}
	case "upload":
		uploadStats, err := runUpload(ctx, cfg)
		if err != nil {
			log.Fatalf("Upload phase failed: %v", err)
		}
		printUploadReport(uploadStats)
	case "verify":
		log.Fatal("--phase verify requires prior upload stats. Use --phase all or --phase upload first.")
	case "all":
		if err := runDownload(ctx, cfg); err != nil {
			log.Fatalf("Download phase failed: %v", err)
		}
		uploadStats, err := runUpload(ctx, cfg)
		if err != nil {
			log.Fatalf("Upload phase failed: %v", err)
		}
		printUploadReport(uploadStats)

		report, err := runVerify(ctx, cfg, uploadStats)
		if err != nil {
			log.Fatalf("Verify phase failed: %v", err)
		}
		printVerifyReport(report)

		if cfg.Cleanup {
			runCleanup(cfg)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown phase %q. Must be one of: download, upload, verify, all\n", cfg.Phase)
		os.Exit(1)
	}
}

// runDownload syncs files from Google Drive to the local cache.
func runDownload(ctx context.Context, cfg *config.Config) error {
	fmt.Printf("=== Huma Test Suite — Download Phase ===\n")
	fmt.Printf("Folder ID : %s\n", cfg.FolderID)
	fmt.Printf("Cache Dir : %s\n", cfg.CacheDir)
	fmt.Println()

	syncer, err := drive.NewSyncer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialise Drive syncer: %w", err)
	}

	stats, err := syncer.Sync(ctx)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	fmt.Printf("\n=== Download Complete ===\n")
	fmt.Printf("Total files  : %d\n", stats.Total)
	fmt.Printf("Downloaded   : %d\n", stats.Downloaded)
	fmt.Printf("Skipped      : %d (already cached)\n", stats.Skipped)
	fmt.Printf("Failed       : %d\n", stats.Failed)

	if stats.Failed > 0 {
		fmt.Println("\nFailed files:")
		for _, f := range stats.FailedFiles {
			fmt.Printf("  - %s: %s\n", f.Name, f.Err)
		}
	}
	fmt.Println()
	return nil
}

// runUpload uploads cached files to the Emergent project.
func runUpload(ctx context.Context, cfg *config.Config) (*uploader.Stats, error) {
	fmt.Printf("=== Huma Test Suite — Upload Phase ===\n")
	fmt.Printf("Server     : %s\n", cfg.EmergentServerURL)
	fmt.Printf("Project ID : %s\n", cfg.EmergentProjectID)
	fmt.Printf("Cache Dir  : %s\n", cfg.CacheDir)
	fmt.Printf("Workers    : %d\n", cfg.Concurrency)
	fmt.Println()

	u, err := uploader.New(cfg)
	if err != nil {
		return nil, err
	}

	stats, err := u.Upload(ctx)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// runVerify polls extraction status for uploaded documents.
func runVerify(ctx context.Context, cfg *config.Config, uploadStats *uploader.Stats) (*verifier.Report, error) {
	fmt.Printf("\n=== Huma Test Suite — Verify Phase ===\n")

	v, err := verifier.New(cfg)
	if err != nil {
		return nil, err
	}

	return v.Verify(ctx, uploadStats)
}

// printUploadReport prints the upload phase summary.
func printUploadReport(stats *uploader.Stats) {
	fmt.Println()
	fmt.Println("=== Upload Summary ===")
	fmt.Printf("Total      : %d\n", stats.Total)
	fmt.Printf("Uploaded   : %d\n", stats.Successful)
	fmt.Printf("Duplicates : %d\n", stats.Duplicates)
	fmt.Printf("Failed     : %d\n", stats.Failed)

	if stats.Failed > 0 {
		fmt.Println("\nFailed uploads:")
		for _, r := range stats.Records {
			if r.Err != nil {
				fmt.Printf("  - %s: %v\n", r.Filename, r.Err)
			}
		}
	}
}

// printVerifyReport prints the final extraction verification report.
func printVerifyReport(r *verifier.Report) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("         EXTRACTION VERIFICATION REPORT")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  Total documents   : %d\n", r.Total)
	fmt.Printf("  Successful        : %d\n", r.Successful)
	fmt.Printf("  Failed            : %d\n", r.Failed)
	fmt.Printf("  Timed out         : %d\n", r.TimedOut)
	fmt.Printf("  Skipped (upload)  : %d\n", r.Skipped)

	successRate := 0.0
	denominator := r.Successful + r.Failed + r.TimedOut
	if denominator > 0 {
		successRate = float64(r.Successful) / float64(denominator) * 100
	}
	fmt.Printf("  Success rate      : %.1f%%\n", successRate)
	fmt.Println()
	fmt.Println("  Extraction timing (successful docs):")
	fmt.Printf("    Average : %s\n", r.AvgExtractionTime.Round(time.Second))
	fmt.Printf("    Min     : %s\n", r.MinExtractionTime.Round(time.Second))
	fmt.Printf("    Max     : %s\n", r.MaxExtractionTime.Round(time.Second))

	if len(r.ErrorBreakdown) > 0 {
		fmt.Println()
		fmt.Println("  Error breakdown:")
		for errMsg, count := range r.ErrorBreakdown {
			fmt.Printf("    [%d] %s\n", count, errMsg)
		}
	}
	fmt.Println(strings.Repeat("=", 60))
}

// runCleanup removes the local cache directory.
func runCleanup(cfg *config.Config) {
	fmt.Printf("\nCleaning up cache directory: %s\n", cfg.CacheDir)
	if err := os.RemoveAll(cfg.CacheDir); err != nil {
		fmt.Printf("Warning: cleanup failed: %v\n", err)
	} else {
		fmt.Println("Cache removed.")
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
