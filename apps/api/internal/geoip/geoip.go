// Package geoip provides geographic IP blocking backed by a local MaxMind DB
// reader. The gate is a registered setting (geo_block_setting) so the dashboard
// can flip it on/off and edit the blocked-country list without a restart; the
// database file itself is operator-supplied via the GEOIP_DB_PATH env var.
package geoip

import (
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/settings"
)

// GeoBlockSetting controls geographic request blocking.
type GeoBlockSetting struct {
	// Enabled master switch. When off the middleware passes through without
	// touching the GeoIP database at all.
	Enabled bool `json:"enabled"`
	// AllowAdmin keeps administrators (and root) usable from blocked regions:
	// a request carrying an admin dashboard credential passes the gate, so the
	// operator can still sign in and run the site while the region is blocked.
	// Visitors and regular users are unaffected by this switch.
	AllowAdmin bool `json:"allow_admin"`
	// BlockedCountries is the list of ISO 3166-1 country codes (uppercase, e.g.
	// "CN") whose client IPs are rejected. Empty list blocks nothing even when
	// Enabled is true.
	BlockedCountries []string `json:"blocked_countries"`
}

var geoBlockSetting = GeoBlockSetting{
	Enabled:          false,
	AllowAdmin:       true,
	BlockedCountries: []string{"CN"},
}

func init() {
	settings.GlobalConfig.Register("geo_block_setting", &geoBlockSetting)
}

// GetGeoBlockSetting returns the live geo-block configuration.
func GetGeoBlockSetting() *GeoBlockSetting {
	return &geoBlockSetting
}

// NormalizedBlockedCountries returns the configured country list upper-cased and
// de-duplicated, so lookup is case-stable no matter how the admin typed it.
func (s *GeoBlockSetting) NormalizedBlockedCountries() []string {
	seen := make(map[string]struct{}, len(s.BlockedCountries))
	out := make([]string, 0, len(s.BlockedCountries))
	for _, code := range s.BlockedCountries {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

// LookupResult is the country code resolved for an IP. Country is empty when
// the IP is not present in the database (common for IPv6 / private ranges).
type LookupResult struct {
	Country string
}

// lookupCache is a small TTL cache so the hot path does one database read per
// IP per minute instead of per request.
const lookupCacheTTL = time.Minute

type cachedLookup struct {
	country string
	at      time.Time
}

var (
	lookupCacheMu sync.RWMutex
	lookupCache   = make(map[string]cachedLookup)
)

// Reader is the lazily opened MaxMind DB handle. Safe for concurrent use.
var reader = &syncedReader{}

// syncedReader wraps a maxminddb.Reader with a mutex so we can open, close,
// and reopen (on env var change) without data races.
type syncedReader struct {
	mu     sync.Mutex
	db     *maxminddb.Reader
	path   string
	loaded bool // true once a path has been successfully opened
}

// DatabasePath returns the configured MMDB path from the environment.
func DatabasePath() string {
	return strings.TrimSpace(os.Getenv("GEOIP_DB_PATH"))
}

// EnsureReader loads (or reloads) the database at DatabasePath() if needed.
// Returns nil when the path is empty or the file cannot be opened; callers must
// treat nil as "geographic blocking is not available" and pass through.
func EnsureReader() *maxminddb.Reader {
	path := DatabasePath()
	if path == "" {
		return nil
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.db != nil && reader.path == path {
		return reader.db
	}
	if reader.db != nil {
		if cerr := reader.db.Close(); cerr != nil {
			common.SysError("geoip: failed to close old DB " + reader.path + ": " + cerr.Error())
		}
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		common.SysError("geoip: failed to open " + path + ": " + err.Error())
		reader.db = nil
		return nil
	}
	reader.db = db
	reader.path = path
	reader.loaded = true
	return reader.db
}

// LookupCountry resolves the country code for ipStr, using the TTL cache for
// repeated hits. Returns "" when the database is unavailable or the IP is not in
// the database.
func LookupCountry(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}
	db := EnsureReader()
	if db == nil {
		return ""
	}
	now := time.Now()
	key := ip.String()

	lookupCacheMu.RLock()
	if entry, ok := lookupCache[key]; ok && now.Sub(entry.at) < lookupCacheTTL {
		lookupCacheMu.RUnlock()
		return entry.country
	}
	lookupCacheMu.RUnlock()

	country := ""
	var rec struct {
		Country struct {
			IsoCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := db.Lookup(ip, &rec); err != nil {
		// A per-record read error on a healthy database is rare; log once-ish
		// so a corrupt DB is visible without changing the fail-open outcome
		// (an unreadable record must not be treated as "blocked").
		common.SysError("geoip: lookup failed for " + ipStr + ": " + err.Error())
	} else {
		country = strings.ToUpper(rec.Country.IsoCode)
	}

	lookupCacheMu.Lock()
	lookupCache[key] = cachedLookup{country: country, at: now}
	// Overflow is rare (4096-entry cap); when it happens, reset the whole
	// map to the current entry rather than sweeping it. The lost lookups
	// simply re-cost a database read on their next request — acceptable.
	if len(lookupCache) > 4096 {
		lookupCache = make(map[string]cachedLookup)
		lookupCache[key] = cachedLookup{country: country, at: now}
	}
	lookupCacheMu.Unlock()
	return country
}

// BlocksCountry reports whether a resolved country code is on the block list.
// Callers own the Enabled check and the IP-to-country lookup, so the gate can
// keep its own lookup indirection for tests.
func (s *GeoBlockSetting) BlocksCountry(country string) bool {
	if country == "" {
		return false
	}
	for _, code := range s.NormalizedBlockedCountries() {
		if code == country {
			return true
		}
	}
	return false
}
