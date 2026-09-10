package channel

import (
	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// GetActiveRouteStatsPoolKeys returns the set of PoolKeys (one per alias) that
// currently have at least one enabled route unit. Used by the sweep ticker to
// preserve share pools that are still backed by live routes.
//
// A nil return means "the live set could not be determined", never "no route is
// live": SweepSharePools would read the latter as permission to delete every
// pool. alias2routes is only built by InitChannelCache, which early-returns
// when common.MemoryCacheEnabled is false, so the index being absent is the
// normal state for a deployment with the memory cache off — the table is read
// directly in that case, and only a failed read yields nil.
func GetActiveRouteStatsPoolKeys() map[routestats.PoolKey]struct{} {
	channelSyncLock.RLock()
	indexed := alias2routes
	channelSyncLock.RUnlock()

	if indexed == nil {
		return activeRouteStatsPoolKeysFromDB()
	}
	keep := make(map[routestats.PoolKey]struct{}, len(indexed))
	for alias := range indexed {
		keep[routestats.PoolKey{PublicModelAlias: alias}] = struct{}{}
	}
	return keep
}

// activeRouteStatsPoolKeysFromDB reads the live pool set straight from
// channel_model_routes. Returns nil when the set cannot be read, so neither a
// transient database error nor an uninitialised handle is mistaken for an empty
// pool set — which the sweep would act on by deleting every pool.
func activeRouteStatsPoolKeysFromDB() map[routestats.PoolKey]struct{} {
	if dbx.DB == nil {
		return nil
	}
	var aliases []string
	if err := dbx.DB.Model(&ChannelModelRoute{}).
		Where("enabled = ?", true).
		Distinct().
		Pluck("public_model_alias", &aliases).Error; err != nil {
		common.SysError("failed to load active route stats pool keys: " + err.Error())
		return nil
	}
	keep := make(map[routestats.PoolKey]struct{}, len(aliases))
	for _, alias := range aliases {
		keep[routestats.PoolKey{PublicModelAlias: alias}] = struct{}{}
	}
	return keep
}
