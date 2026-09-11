package geoip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oschwald/maxminddb-golang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubbedDBBytes stands in for a downloaded database in tests that stub the
// MaxMind parser; the parser itself is exercised by the rejection tests with
// real HTML garbage, and end-to-end with real databases.
const stubbedDBBytes = "not actually parsed in stubbed tests"

// withUpdaterEnv isolates the cwd-relative managed path in a temp directory
// and points the download source at a per-test server whose behavior the test
// swaps via the returned setter. t.Setenv / t.Chdir restore everything.
func withUpdaterEnv(t *testing.T, initial http.HandlerFunc) (setHandler func(http.HandlerFunc)) {
	t.Helper()

	t.Chdir(t.TempDir()) // the managed path is cwd-relative
	t.Setenv("GEOIP_DB_PATH", "")
	current := initial
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current(w, r)
	}))
	t.Setenv("GEOIP_DB_URL", server.URL+"/db.mmdb")
	t.Cleanup(func() {
		server.Close()
		ResetReader()
	})
	return func(handler http.HandlerFunc) { current = handler }
}

// stubOpenDatabase replaces the MaxMind parser with an accepting stub; the
// zero-value Reader is safe to Close.
func stubOpenDatabase(t *testing.T) {
	t.Helper()
	previous := openDatabase
	openDatabase = func(path string) (*maxminddb.Reader, error) {
		return &maxminddb.Reader{}, nil
	}
	t.Cleanup(func() { openDatabase = previous })
}

func geoSeedManagedFile(t *testing.T, modTime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(ManagedDatabasePath()), 0o755))
	require.NoError(t, os.WriteFile(ManagedDatabasePath(), []byte(stubbedDBBytes), 0o644))
	require.NoError(t, os.Chtimes(ManagedDatabasePath(), modTime, modTime))
}

func TestUpdateDatabaseDownloadsAndInstalls(t *testing.T) {
	var requests int
	setHandler := withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(stubbedDBBytes))
	})
	stubOpenDatabase(t)

	status, err := UpdateDatabase(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, requests)
	assert.True(t, status.Exists)
	assert.True(t, status.SizeBytes > 0)
	assert.WithinDuration(t, time.Now(), time.Unix(status.UpdatedAt, 0), time.Minute)
	assert.False(t, status.Stale)
	assert.Equal(t, DatabasePath(), status.Path)

	// The in-memory reader was reset so the next lookup reopens the new file.
	reader.mu.Lock()
	assert.Nil(t, reader.db)
	reader.mu.Unlock()

	// A second update within the fresh window must not re-download.
	fresh, err := UpdateDatabase(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, requests, "a fresh database must not be re-downloaded")
	assert.Equal(t, status.UpdatedAt, fresh.UpdatedAt)

	// The operator can still force a re-download: stale the file by hand.
	stale := time.Now().Add(-DatabaseFreshWindow - time.Hour)
	require.NoError(t, os.Chtimes(ManagedDatabasePath(), stale, stale))
	setHandler(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(stubbedDBBytes))
	})
	_, err = UpdateDatabase(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, requests, "a stale database is refreshed")
}

func TestUpdateDatabaseRejectsInvalidFile(t *testing.T) {
	withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>404 not found</html>"))
	})
	// No stub: the real MaxMind parser must reject the garbage.

	status, err := UpdateDatabase(context.Background())
	require.Error(t, err)
	assert.False(t, status.Exists, "a rejected download must not be installed")

	entries, err := os.ReadDir(filepath.Dir(ManagedDatabasePath()))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp-", "temp files must be cleaned up")
	}
}

func TestUpdateDatabaseKeepsOldFileOnFailure(t *testing.T) {
	setHandler := withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(stubbedDBBytes))
	})
	stubOpenDatabase(t)
	_, err := UpdateDatabase(context.Background())
	require.NoError(t, err)

	// The source goes down and the file is stale: the previously installed
	// file must survive the failed refresh.
	stale := time.Now().Add(-DatabaseFreshWindow - time.Hour)
	require.NoError(t, os.Chtimes(ManagedDatabasePath(), stale, stale))
	staled, err := os.Stat(ManagedDatabasePath())
	require.NoError(t, err)
	setHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err = UpdateDatabase(context.Background())
	require.Error(t, err)

	survivor, err := os.Stat(ManagedDatabasePath())
	require.NoError(t, err)
	assert.Equal(t, staled.ModTime(), survivor.ModTime(), "failed updates must not touch the old file")
}

func TestEnsureFreshForEnable(t *testing.T) {
	t.Run("missing database and failing source refuses the enable", func(t *testing.T) {
		withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		require.Error(t, EnsureFreshForEnable(context.Background()))
	})

	t.Run("fresh database enables without a download", func(t *testing.T) {
		setHandler := withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("a fresh database must not be re-downloaded")
		})
		_ = setHandler
		geoSeedManagedFile(t, time.Now())
		assert.NoError(t, EnsureFreshForEnable(context.Background()))
	})

	t.Run("stale database is refreshed before enabling", func(t *testing.T) {
		var requests int
		setHandler := withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
			requests++
			_, _ = w.Write([]byte(stubbedDBBytes))
		})
		_ = setHandler
		stubOpenDatabase(t)
		geoSeedManagedFile(t, time.Now().Add(-DatabaseFreshWindow-time.Hour))

		require.NoError(t, EnsureFreshForEnable(context.Background()))
		assert.Equal(t, 1, requests)

		assert.False(t, GetDatabaseStatus().Stale, "the refreshed database is fresh again")
	})
}

func TestExternallyManagedDatabaseIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	managedByOperator := filepath.Join(dir, "operator.mmdb")
	t.Setenv("GEOIP_DB_PATH", managedByOperator)
	t.Setenv("GEOIP_DB_URL", "http://127.0.0.1:1/unreachable") // must never be called

	assert.True(t, ExternallyManaged())
	assert.Equal(t, managedByOperator, DatabasePath())

	_, err := UpdateDatabase(context.Background())
	require.Error(t, err, "the updater must never write to an operator-managed path")

	require.Error(t, EnsureFreshForEnable(context.Background()),
		"enabling with a missing operator-managed file must be refused")
	assert.NoFileExists(t, managedByOperator)

	require.NoError(t, os.WriteFile(managedByOperator, []byte(stubbedDBBytes), 0o644))
	assert.NoError(t, EnsureFreshForEnable(context.Background()),
		"an existing operator-managed file enables without downloads")
}

func TestDatabaseStaleWindow(t *testing.T) {
	assert.False(t, DatabaseStale(time.Now().Add(-6*24*time.Hour)))
	assert.True(t, DatabaseStale(time.Now().Add(-8*24*time.Hour)))
	assert.True(t, DatabaseStale(time.Now().Add(-DatabaseFreshWindow-time.Minute)))
}

func TestGetDatabaseStatusMissingFile(t *testing.T) {
	withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("status must not download")
	})
	status := GetDatabaseStatus()
	assert.False(t, status.Exists)
	assert.False(t, status.Stale, "a missing file is reported as absent, not stale")
	assert.Equal(t, int64(0), status.UpdatedAt)
	assert.Equal(t, ManagedDatabasePath(), status.Path)
	assert.False(t, status.ExternallyManaged)
}

// TestLookupCountryFailsOpenWhenManagedFileMissing guards the request path:
// with the block enabled but no usable database, lookups resolve no country
// (and the gate passes) instead of erroring.
func TestLookupCountryFailsOpenWhenManagedFileMissing(t *testing.T) {
	withUpdaterEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("lookup must not download")
	})
	assert.Empty(t, LookupCountry("114.114.114.114"))
}
