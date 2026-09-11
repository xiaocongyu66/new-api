package geoip

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizedBlockedCountries pins the case/dedup behavior so a lookup never
// silently stops matching because of how the admin typed the country list.
func TestNormalizedBlockedCountries(t *testing.T) {
	s := &GeoBlockSetting{BlockedCountries: []string{"cn", " CN ", "cn", "", "US"}}
	got := s.NormalizedBlockedCountries()
	assert.Equal(t, []string{"CN", "US"}, got)
}

// TestBlocksCountry pins the matching contract: case-insensitive on the
// configured list, exact on the resolved code, and empty matches nothing.
func TestBlocksCountry(t *testing.T) {
	s := &GeoBlockSetting{BlockedCountries: []string{"cn", "US"}}
	assert.True(t, s.BlocksCountry("CN"))
	assert.True(t, s.BlocksCountry("US"))
	assert.False(t, s.BlocksCountry("SG"))

	empty := &GeoBlockSetting{BlockedCountries: nil}
	assert.False(t, empty.BlocksCountry("CN"), "the country list is the actual gate")

	assert.False(t, s.BlocksCountry(""), "an unresolved country must never match")
}

// TestAllowAdminDefaultsOn pins the default: administrators keep access to the
// site from a blocked region unless the operator explicitly turns it off.
func TestAllowAdminDefaultsOn(t *testing.T) {
	assert.True(t, geoBlockSetting.AllowAdmin)
}

// TestLookupCountryNoDatabase verifies fail-open at the lookup level: with no
// database on disk (and none managed externally), the resolver cannot name a
// country, so the gate cannot block.
func TestLookupCountryNoDatabase(t *testing.T) {
	assert.False(t, ExternallyManaged())
	// The managed file does not exist in the test cwd.
	assert.Empty(t, LookupCountry("1.2.3.4"), "missing database must fail open")
}

// TestLookupCountryInvalidIP ensures unparseable input returns empty, not panic.
func TestLookupCountryInvalidIP(t *testing.T) {
	assert.Empty(t, LookupCountry("not-an-ip"))
	assert.Empty(t, LookupCountry("300.1.1.1"))
}
