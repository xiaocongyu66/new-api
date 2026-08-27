package model

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// The channel cache implementation lives in internal/catalog
// (cache_store.go); it fills these bridges in its init(). Model code keeps
// calling the package-level wrappers below, so no model file imports the
// capability and the dependency direction stays one-way.
//
// When the capability is absent (package-model tests that never import it),
// the fallbacks reproduce the disabled-cache behavior exactly.
var (
	cacheBridge                 *CacheBridge
	cacheGetChannelFn           func(id int) (*Channel, error)
	cacheGetChannelInfoFn       func(id int) (*ChannelInfo, error)
	cacheUpdateChannelStatusFn  func(id int, status int)
	initChannelCacheFn          func()
	lookupAdvancedCustomConfigs func(channelIDs []int) map[int]*dto.AdvancedCustomConfig
)

// CacheGetChannel resolves a channel by id through the capability cache,
// falling back to the database when the cache is disabled or unregistered.
func CacheGetChannel(id int) (*Channel, error) {
	if cacheGetChannelFn != nil {
		return cacheGetChannelFn(id)
	}
	return GetChannelById(id, true)
}

// CacheGetChannelInfo resolves a channel's ChannelInfo through the cache,
// with the same disabled-cache database fallback as CacheGetChannel.
func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if cacheGetChannelInfoFn != nil {
		return cacheGetChannelInfoFn(id)
	}
	channel, err := GetChannelById(id, true)
	if err != nil {
		return nil, err
	}
	return &channel.ChannelInfo, nil
}

// CacheUpdateChannelStatus refreshes the cached channel status; it is a
// no-op when the cache is disabled or unregistered.
func CacheUpdateChannelStatus(id int, status int) {
	if cacheUpdateChannelStatusFn != nil {
		cacheUpdateChannelStatusFn(id, status)
	}
}

// InitChannelCache rebuilds the channel cache via the capability; without
// the capability it only invalidates the derived pricing cache, matching
// the legacy disabled-cache behavior.
func InitChannelCache() {
	if initChannelCacheFn != nil {
		initChannelCacheFn()
		return
	}
	InvalidatePricingCache()
}

// LookupAdvancedCustomConfigs returns parsed Advanced Custom configs for the
// given channel ids from the cache. ok is false when the capability is not
// registered; callers then use their own database path.
func LookupAdvancedCustomConfigs(channelIDs []int) (map[int]*dto.AdvancedCustomConfig, bool) {
	if lookupAdvancedCustomConfigs == nil || !common.MemoryCacheEnabled {
		return nil, false
	}
	configs := lookupAdvancedCustomConfigs(channelIDs)
	return configs, configs != nil
}

// GroupCol exposes the dialect-correct quoted name of the reserved "group"
// column to packages that build ability queries without touching raw SQL
// themselves.
func GroupCol() string {
	return dbx.GroupCol()
}

// KeyCol is the same accessor for the reserved "key" column. Both names are
// assigned by initCol during database setup, so callers must read them through
// these functions; a copy taken at package-init time would still be empty.
func KeyCol() string {
	return dbx.KeyCol()
}

// CacheBridge carries the capability-side cache entry points. Registered once
// by internal/catalog in its init.
type CacheBridge struct {
	GetChannel            func(id int) (*Channel, error)
	GetChannelInfo        func(id int) (*ChannelInfo, error)
	UpdateChannelStatus   func(id int, status int)
	InitCache             func()
	LookupAdvancedConfigs func(channelIDs []int) map[int]*dto.AdvancedCustomConfig
	UpdateChannel         func(channel *Channel)
}

// RegisterCacheBridge installs the capability cache implementation behind the
// package-level wrappers above.
func RegisterCacheBridge(b CacheBridge) {
	cacheBridge = &b
	cacheGetChannelFn = b.GetChannel
	cacheGetChannelInfoFn = b.GetChannelInfo
	cacheUpdateChannelStatusFn = b.UpdateChannelStatus
	initChannelCacheFn = b.InitCache
	lookupAdvancedCustomConfigs = b.LookupAdvancedConfigs
}

// CacheUpdateChannel refreshes the cached copy of one channel through the
// capability implementation; no-op when the cache is disabled or unregistered.
func CacheUpdateChannel(channel *Channel) {
	if cacheBridge != nil && cacheBridge.UpdateChannel != nil {
		cacheBridge.UpdateChannel(channel)
	}
}
