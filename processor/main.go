package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sky-history/processor/config"
	"github.com/sky-history/processor/db"
	"github.com/sky-history/processor/github"
	"github.com/sky-history/processor/ingest"
	"github.com/sky-history/processor/parser"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Sky-History Processor starting...")

	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutdown signal received, finishing current work...")
		cancel()
	}()

	// Connect to database
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Run migrations
	migrationsDir := findMigrationsDir()
	if err := db.Migrate(ctx, pool, migrationsDir); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize components
	p := &Processor{
		cfg:          cfg,
		pool:         pool,
		ghClient:     github.NewClient(cfg.GitHubRepo, cfg.GitHubToken),
		downloader:   ingest.NewDownloader(cfg.TempDir, cfg.GitHubToken, cfg.KeepDownloads),
		releaseRepo:  db.NewReleaseRepo(pool),
		aircraftRepo: db.NewAircraftRepo(pool),
		flightRepo:   db.NewFlightRepo(pool),
		typeRepo:     db.NewAircraftTypeRepo(pool),
	}

	// Backfill if configured, otherwise do a single poll immediately
	if cfg.BackfillDays > 0 {
		log.Printf("Backfill mode: fetching releases from the last %d days", cfg.BackfillDays)
		p.checkAndProcess(ctx, true)
	} else {
		// Only run immediate poll when not backfilling
		p.checkAndProcess(ctx, false)
	}

	// Main polling loop
	log.Printf("Starting poll loop (interval: %s)", cfg.PollInterval)
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Processor shutting down.")
			return
		case <-ticker.C:
			p.checkAndProcess(ctx, false)
		}
	}
}

// Processor orchestrates the download-parse-store pipeline.
type Processor struct {
	cfg          config.Config
	pool         *pgxpool.Pool
	ghClient     *github.Client
	downloader   *ingest.Downloader
	releaseRepo  *db.ReleaseRepo
	aircraftRepo *db.AircraftRepo
	flightRepo   *db.FlightRepo
	typeRepo     *db.AircraftTypeRepo
}

func (p *Processor) checkAndProcess(ctx context.Context, backfill bool) {
	log.Println("Checking for new releases...")

	isProcessed := func(tag string) (bool, error) {
		// Skip permanently failed releases
		permFailed, err := p.releaseRepo.IsPermanentlyFailed(ctx, tag)
		if err != nil {
			return false, err
		}
		if permFailed {
			return true, nil // treat as already processed so we skip it
		}
		return p.releaseRepo.IsProcessed(ctx, tag)
	}

	var releases []github.ReleaseInfo
	var err error

	if backfill {
		// Backfill: fetch releases from the last N days (start of day, UTC)
		now := time.Now().UTC()
		since := time.Date(now.Year(), now.Month(), now.Day()-p.cfg.BackfillDays, 0, 0, 0, 0, time.UTC)
		log.Printf("Backfill cutoff date: %s", since.Format("2006-01-02"))
		releases, err = p.ghClient.FetchNewReleases(ctx, since, isProcessed)
	} else {
		releases, err = p.ghClient.FetchRecentReleases(ctx, 3, isProcessed)
	}

	if err != nil {
		if p.handleRateLimitError(ctx, err) {
			return
		}
		log.Printf("Error fetching releases: %v", err)
		return
	}

	if len(releases) == 0 {
		log.Println("No new releases to process.")
		return
	}

	// On first run without backfill, only process the latest release
	if !backfill && len(releases) > 1 {
		lastProcessed, _ := p.releaseRepo.GetLastProcessedDate(ctx)
		if lastProcessed == nil {
			releases = releases[len(releases)-1:]
			log.Println("First run without backfill; processing only the latest release")
		}
	}

	// When not keeping downloads, clean up any leftover temp files before starting
	if !p.cfg.KeepDownloads {
		p.downloader.CleanAll()
	}

	log.Printf("Found %d new release(s) to process", len(releases))

	for _, release := range releases {
		if ctx.Err() != nil {
			return
		}
		p.processRelease(ctx, release)
	}
}

func (p *Processor) processRelease(ctx context.Context, release github.ReleaseInfo) {
	log.Printf("Processing release: %s (date: %s)", release.Tag, release.Date.Format("2006-01-02"))
	startTime := time.Now()

	// Step 1: Download and extract
	extractDir, err := p.downloader.DownloadAndExtract(ctx, release.AssetURLs, release.Tag)
	if err != nil {
		if p.handleRateLimitError(ctx, err) {
			return
		}
		log.Printf("Error downloading/extracting %s: %v", release.Tag, err)
		p.recordFailure(ctx, release, err)
		return
	}
	defer p.downloader.Cleanup(release.Tag)

	downloadTime := time.Since(startTime)
	log.Printf("  Download + extract completed in %s", downloadTime.Round(time.Second))

	// Step 2: Parse trace files
	parseStart := time.Now()
	aircraft, err := parser.ParseDirectory(extractDir, release.Date, p.cfg.ParseWorkers)
	if err != nil {
		log.Printf("Error parsing %s: %v", release.Tag, err)
		p.recordFailure(ctx, release, err)
		return
	}

	totalFlights := 0
	for _, a := range aircraft {
		totalFlights += len(a.Flights)
	}

	parseTime := time.Since(parseStart)
	log.Printf("  Parsed %d aircraft with %d flights in %s", len(aircraft), totalFlights, parseTime.Round(time.Second))

	// Step 3: Write to database in a transaction
	dbStart := time.Now()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		log.Printf("Error starting transaction for %s: %v", release.Tag, err)
		return
	}
	defer tx.Rollback(ctx) // no-op after commit

	aircraftCount, err := p.aircraftRepo.UpsertBatch(ctx, tx, aircraft, p.typeRepo)
	if err != nil {
		log.Printf("Error upserting aircraft for %s: %v", release.Tag, err)
		return
	}

	flightCount, err := p.flightRepo.InsertBatch(ctx, tx, release.Date, aircraft)
	if err != nil {
		log.Printf("Error inserting flights for %s: %v", release.Tag, err)
		return
	}

	if err := p.releaseRepo.MarkProcessed(ctx, tx, release.Tag, release.Date, aircraftCount, flightCount); err != nil {
		log.Printf("Error marking release %s as processed: %v", release.Tag, err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("Error committing transaction for %s: %v", release.Tag, err)
		return
	}

	dbTime := time.Since(dbStart)
	elapsed := time.Since(startTime)
	log.Printf("  DB write completed in %s (%d aircraft, %d flights)", dbTime.Round(time.Second), aircraftCount, flightCount)
	log.Printf("  Release %s fully processed in %s", release.Tag, elapsed.Round(time.Second))
}

func findMigrationsDir() string {
	candidates := []string{
		"db/migrations",
		"processor/db/migrations",
		"/app/db/migrations",
	}

	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "db", "migrations")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	return "db/migrations"
}

func (p *Processor) handleRateLimitError(ctx context.Context, err error) bool {
	var rle *github.RateLimitError
	if !errors.As(err, &rle) {
		return false
	}
	log.Printf("Rate limited: %v", rle)
	log.Printf("Waiting %s before retrying...", rle.RetryAfter.Round(time.Second))
	select {
	case <-time.After(rle.RetryAfter):
	case <-ctx.Done():
	}
	return true
}

func (p *Processor) recordFailure(ctx context.Context, release github.ReleaseInfo, processingErr error) {
	permanent, err := p.releaseRepo.RecordFailure(ctx, release.Tag, release.Date, processingErr.Error())
	if err != nil {
		log.Printf("Error recording failure for %s: %v", release.Tag, err)
		return
	}
	if permanent {
		log.Printf("Release %s permanently marked as failed after repeated failures — will not retry", release.Tag)
	} else {
		log.Printf("Release %s recorded as failed (attempt 1) — will retry next cycle", release.Tag)
	}
}
