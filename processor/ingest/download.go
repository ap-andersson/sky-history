package ingest

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sky-history/processor/github"
)

// Downloader handles downloading and extracting GitHub release assets.
type Downloader struct {
	httpClient    *http.Client
	token         string
	tempDir       string
	keepDownloads bool
}

// NewDownloader creates a new Downloader.
func NewDownloader(tempDir, token string, keepDownloads bool) *Downloader {
	return &Downloader{
		httpClient:    &http.Client{Timeout: 60 * time.Minute}, // Large files need long timeout
		token:         token,
		tempDir:       tempDir,
		keepDownloads: keepDownloads,
	}
}

// DownloadAndExtract downloads the split tar files, concatenates them, and extracts the archive.
// Returns the path to the extracted directory.
// If keepDownloads is enabled, extracted data is cached on disk. When a new release
// is downloaded, all previously cached releases are deleted so only the latest remains.
func (d *Downloader) DownloadAndExtract(ctx context.Context, assetURLs []string, tag string) (string, error) {
	outputDir := filepath.Join(d.tempDir, tag)
	extractDir := filepath.Join(outputDir, "extracted")

	// Check if we already have cached extracted data
	if d.keepDownloads {
		if entries, err := os.ReadDir(extractDir); err == nil && len(entries) > 0 {
			log.Printf("  Using cached data for %s (%d entries)", tag, len(entries))
			return extractDir, nil
		}
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	// Download all parts to temp files
	var partFiles []string
	for i, url := range assetURLs {
		log.Printf("  Downloading part %d/%d: %s", i+1, len(assetURLs), filepath.Base(url))
		partPath, err := d.downloadFile(ctx, url, outputDir, i)
		if err != nil {
			d.cleanupFiles(partFiles)
			return "", fmt.Errorf("download part %d: %w", i+1, err)
		}
		partFiles = append(partFiles, partPath)
	}

	// Extract the concatenated tar archive
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		d.cleanupFiles(partFiles)
		return "", fmt.Errorf("create extract dir: %w", err)
	}

	log.Printf("  Extracting tar archive...")
	if err := d.extractConcatenatedTar(partFiles, extractDir); err != nil {
		d.cleanupFiles(partFiles)
		return "", fmt.Errorf("extract tar: %w", err)
	}

	// Always remove raw part files after extraction; the extracted dir is what we cache
	d.cleanupFiles(partFiles)

	return extractDir, nil
}

// Cleanup removes the entire temp directory for a release.
// When keepDownloads is enabled, files are preserved for reuse.
func (d *Downloader) Cleanup(tag string) {
	if d.keepDownloads {
		log.Printf("  Keeping cached data for %s", tag)
		return
	}
	dir := filepath.Join(d.tempDir, tag)
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("Warning: failed to cleanup %s: %v", dir, err)
	}
}

func (d *Downloader) downloadFile(ctx context.Context, url, dir string, index int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting / throttling from GitHub CDN
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		wait := 5 * time.Minute // conservative default
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				wait = time.Duration(secs) * time.Second
			}
		}
		return "", &github.RateLimitError{
			Status:     resp.StatusCode,
			Message:    fmt.Sprintf("download throttled for %s", filepath.Base(url)),
			RetryAfter: wait,
			ResetAt:    time.Now().UTC().Add(wait),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	partPath := filepath.Join(dir, fmt.Sprintf("part_%d", index))
	f, err := os.Create(partPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	start := time.Now()
	written, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(partPath)
		return "", fmt.Errorf("write file: %w", err)
	}

	elapsed := time.Since(start).Seconds()
	mbWritten := float64(written) / (1024 * 1024)
	speed := mbWritten / elapsed
	log.Printf("    Downloaded %.0f MB in %.0fs (%.1f MB/s)", mbWritten, elapsed, speed)
	return partPath, nil
}

func (d *Downloader) extractConcatenatedTar(partFiles []string, extractDir string) error {
	// Create readers for all parts
	readers := make([]io.Reader, 0, len(partFiles))
	files := make([]*os.File, 0, len(partFiles))

	for _, path := range partFiles {
		f, err := os.Open(path)
		if err != nil {
			// Close already opened files
			for _, of := range files {
				of.Close()
			}
			return fmt.Errorf("open part %s: %w", path, err)
		}
		files = append(files, f)
		readers = append(readers, f)
	}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()

	// Concatenate all parts and read as a single tar archive
	combined := io.MultiReader(readers...)
	tr := tar.NewReader(combined)

	fileCount := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header (after %d files): %w", fileCount, err)
		}

		targetPath := filepath.Join(extractDir, header.Name)

		// Security: prevent path traversal
		if !isSubPath(extractDir, targetPath) {
			log.Printf("    Skipping suspicious path: %s", header.Name)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("create dir %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", header.Name, err)
			}
			f, err := os.Create(targetPath)
			if err != nil {
				return fmt.Errorf("create file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", header.Name, err)
			}
			f.Close()
			fileCount++
		}
	}

	log.Printf("    Extracted %d files", fileCount)
	return nil
}

func (d *Downloader) cleanupFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// CleanAll removes all contents of the temp directory.
// Called before processing when KEEP_DOWNLOADS=false to ensure
// no leftover files from previous runs remain.
func (d *Downloader) CleanAll() {
	entries, err := os.ReadDir(d.tempDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(d.tempDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			log.Printf("Warning: failed to remove %s: %v", path, err)
		} else {
			log.Printf("  Cleaned up old temp data: %s", e.Name())
		}
	}
}

// isSubPath checks if target is a child of base (prevents path traversal).
func isSubPath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	// Check that it doesn't start with ".."
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".." {
			return false
		}
	}
	return true
}
