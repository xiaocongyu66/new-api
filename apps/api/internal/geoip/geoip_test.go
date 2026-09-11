package geoip

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizedBlockedCountries pins the case/dedup behavior so a lookup never
// silently stops matching because of how the admin typed the country list.
func TestNormalizedBlockedCountries(t *testing.T) {
	s := &GeoBlockSetting{BlockedCountries: []string{"cn", " CN ", "cn", "", "US"}}
	got := s.NormalizedBlockedCountries()
	assert.Equal(t, []string{"CN", "US"}, got)
}

// TestIsBlockedDisabledWhenOff ensures the off switch passes through even when
// the IP would otherwise match.
func TestIsBlockedDisabledWhenOff(t *testing.T) {
	old := geoBlockSetting
	defer func() { geoBlockSetting = old }()
	geoBlockSetting = GeoBlockSetting{Enabled: false, BlockedCountries: []string{"CN"}}
	assert.False(t, IsBlocked("8.8.8.8"))
}

// TestIsBlockedEmptyCountryList ensures Enabled=true with an empty list blocks
// nothing: the list is the actual gate, the flag alone is not.
func TestIsBlockedEmptyCountryList(t *testing.T) {
	old := geoBlockSetting
	defer func() { geoBlockSetting = old }()
	geoBlockSetting = GeoBlockSetting{Enabled: true, BlockedCountries: nil}
	assert.False(t, IsBlocked("8.8.8.8"))
}

// TestIsBlockedNoDatabase verifies fail-open: with no GEOIP_DB_PATH the lookup
// cannot resolve a country, so a blocked-country config must not 403 anyone.
func TestIsBlockedNoDatabase(t *testing.T) {
	old := geoBlockSetting
	defer func() { geoBlockSetting = old }()
	geoBlockSetting = GeoBlockSetting{Enabled: true, BlockedCountries: []string{"CN"}}
	// No database file is configured in the test environment.
	assert.True(t, DatabasePath() == "")
	assert.False(t, IsBlocked("1.2.3.4"), "missing database must fail open")
}

// TestLookupCountryInvalidIP ensures unparseable input returns empty, not panic.
func TestLookupCountryInvalidIP(t *testing.T) {
	assert.Empty(t, LookupCountry("not-an-ip"))
	require.Empty(t, LookupCountry("300.1.1.1"))
}
