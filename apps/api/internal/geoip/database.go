package geoip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"

	"github.com/QuantumNous/new-api/internal/common"
)

// DefaultDatabaseURL serves a mainland-China-focused MaxMind-format database
// (MetaCubeX/meta-rules-dat "country-lite", rebuilt daily from delegated
// stats). It only contains CN entries, which matches the default
// blocked_countries list; dropping a full GeoLite2-Country file at the same
// path generalizes the block to other countries without code changes.
const DefaultDatabaseURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/latest/download/country-lite.mmdb"

// DatabaseFreshWindow bounds how old a downloaded database may be. Enabling
// the block with an older file triggers a re-download, and the dashboard
// flags the database as stale past this point.
const DatabaseFreshWindow = 7 * 24 * time.Hour

// managedRelPath is cwd-relative. The official image runs with WORKDIR /data
// (the persistent volume), so the managed database lands on durable storage
// without extra configuration.
const managedRelPath = "geoip/Country.mmdb"

const databaseDownloadTimeout = 2 * time.Minute

// openDatabase is a package variable so tests can install a downloaded file
// without a real MaxMind fixture; production always uses the real parser.
var openDatabase = maxminddb.Open

// DatabaseSourceURL returns the configured download source. GEOIP_DB_URL
// replaces the default for deployments that mirror the database internally.
func DatabaseSourceURL() string {
	if url := strings.TrimSpace(os.Getenv("GEOIP_DB_URL")); url != "" {
		return url
	}
	return DefaultDatabaseURL
}

// ExternallyManaged reports whether the operator pinned a database file via
// GEOIP_DB_PATH. The updater never writes to an operator-managed path.
func ExternallyManaged() bool {
	return strings.TrimSpace(os.Getenv("GEOIP_DB_PATH")) != ""
}

// ManagedDatabasePath is where the updater stores its database: relative to
// the working directory, so the official image (WORKDIR /data) persists it on
// the mounted data volume.
func ManagedDatabasePath() string {
	return filepath.Join(managedRelPath)
}

// DatabasePath resolves the file the lookup engine reads: the operator's
// GEOIP_DB_PATH when set, the managed path otherwise.
func DatabasePath() string {
	if ExternallyManaged() {
		return strings.TrimSpace(os.Getenv("GEOIP_DB_PATH"))
	}
	return ManagedDatabasePath()
}

// DatabaseStatus describes the on-disk database for the dashboard.
type DatabaseStatus struct {
	Path              string `json:"path"`
	Exists            bool   `json:"exists"`
	SizeBytes         int64  `json:"size_bytes"`
	UpdatedAt         int64  `json:"updated_at"`
	Stale             bool   `json:"stale"`
	FreshWindowDays   int    `json:"fresh_window_days"`
	SourceURL         string `json:"source_url"`
	ExternallyManaged bool   `json:"externally_managed"`
}

// GetDatabaseStatus inspects the database file. updated_at is the file's
// modification time: a fresh download and a manually replaced file both
// update it, no extra state storage needed.
func GetDatabaseStatus() DatabaseStatus {
	path := DatabasePath()
	status := DatabaseStatus{
		Path:              path,
		SourceURL:         DatabaseSourceURL(),
		ExternallyManaged: ExternallyManaged(),
		FreshWindowDays:   int(DatabaseFreshWindow.Hours() / 24),
	}
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Not-missing is normal, but permission or I/O errors would
			// silently disable the gate — make them visible.
			common.SysError("geoip: failed to stat database " + path + ": " + err.Error())
		}
		return status
	}
	status.Exists = true
	status.SizeBytes = info.Size()
	status.UpdatedAt = info.ModTime().Unix()
	status.Stale = DatabaseStale(info.ModTime())
	return status
}

// DatabaseStale reports whether a modification time falls outside the fresh
// window. A missing file counts as stale.
func DatabaseStale(modTime time.Time) bool {
	return time.Since(modTime) > DatabaseFreshWindow
}

var (
	databaseMu      sync.Mutex
	databaseClient  = &http.Client{Timeout: databaseDownloadTimeout}
	ErrNoDatabase   = errors.New("geoip database file not found")
	ErrInvalidDBURL = errors.New("geoip database url is not configured")
)

// UpdateDatabase downloads a fresh database, validates it, and installs it
// atomically. Concurrent callers serialize; a caller that finds the database
// freshly updated while waiting skips the download. The in-memory reader is
// reset afterwards so the next lookup observes the new file.
func UpdateDatabase(ctx context.Context) (DatabaseStatus, error) {
	if ExternallyManaged() {
		return GetDatabaseStatus(), fmt.Errorf(
			"database is managed externally via GEOIP_DB_PATH (%s); replace the file yourself instead",
			DatabasePath())
	}

	databaseMu.Lock()
	defer databaseMu.Unlock()

	if status := GetDatabaseStatus(); status.Exists && !status.Stale {
		// Another caller updated it while we waited for the lock.
		return status, nil
	}

	source := DatabaseSourceURL()
	if source == "" {
		return GetDatabaseStatus(), ErrInvalidDBURL
	}
	if err := downloadDatabase(ctx, source, DatabasePath()); err != nil {
		return GetDatabaseStatus(), err
	}

	ResetReader()
	common.SysLog("geoip: database updated from " + source)
	return GetDatabaseStatus(), nil
}

// downloadDatabase streams the source into a sibling temp file, validates it
// with the real MaxMind parser, then renames it into place. The previous
// file survives every failure: bad download, validation error, crash.
func downloadDatabase(ctx context.Context, source, target string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, databaseDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := databaseClient.Do(req)
	if err != nil {
		return fmt.Errorf("download from %s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download from %s: unexpected status %d", source, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(dir, "Country.mmdb.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Cleanup is safe after a successful rename too: the temp path no longer
	// exists then, so os.Remove just returns ENOENT.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save download: %w", err)
	}

	if err := validateDatabaseFile(tmpPath); err != nil {
		return fmt.Errorf("downloaded file is not a usable MaxMind database: %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("install database: %w", err)
	}
	return nil
}

// validateDatabaseFile opens the file with the real MaxMind parser, which
// parses the metadata and rejects HTML error pages, truncations, and wrong
// formats.
func validateDatabaseFile(path string) error {
	db, err := openDatabase(path)
	if err != nil {
		return err
	}
	return db.Close()
}

// EnsureFreshForEnable gates the enable switch: turning the block on with a
// missing or week-old database first tries to download a fresh one, so the
// operator never enables a block that would silently fail open. An
// operator-managed file (GEOIP_DB_PATH) is only checked for existence — the
// updater never writes to it, and its freshness is the operator's business.
func EnsureFreshForEnable(ctx context.Context) error {
	if ExternallyManaged() {
		if _, err := os.Stat(DatabasePath()); err != nil {
			return fmt.Errorf("%w: %s", ErrNoDatabase, DatabasePath())
		}
		return nil
	}
	status := GetDatabaseStatus()
	if status.Exists && !status.Stale {
		return nil
	}
	if status.Exists {
		common.SysLog("geoip: database is stale, refreshing before enabling the block")
	}
	updated, err := UpdateDatabase(ctx)
	if err != nil {
		return err
	}
	if !updated.Exists {
		return ErrNoDatabase
	}
	return nil
}

// RefreshStaleDatabaseOnStartup runs in the background at boot: when the
// block is enabled and the database is missing or stale, download a fresh one
// and log the outcome. Failures never block startup — the request path
// fail-opens until the database becomes usable.
func RefreshStaleDatabaseOnStartup() {
	if !geoBlockSetting.Enabled || ExternallyManaged() {
		return
	}
	status := GetDatabaseStatus()
	if status.Exists && !status.Stale {
		return
	}
	if status.Exists {
		common.SysLog("geoip: database is stale, refreshing in background")
	} else {
		common.SysLog("geoip: database is missing while the block is enabled, downloading")
	}
	if _, err := UpdateDatabase(context.Background()); err != nil {
		common.SysError("geoip: background database refresh failed: " + err.Error())
	}
}

// ResetReader drops the cached MaxMind handle so the next lookup reopens the
// file. Called after a successful download; a one-minute country cache may
// briefly serve pre-update answers until its TTL expires.
func ResetReader() {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.db != nil {
		if cerr := reader.db.Close(); cerr != nil {
			common.SysError("geoip: failed to close replaced database: " + cerr.Error())
		}
		reader.db = nil
	}
}
