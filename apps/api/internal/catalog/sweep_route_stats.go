package channel

import (
	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// GetActiveRouteStatsPoolKeys returns the set of PoolKey (group, alias) pairs
// that currently have at least one enabled route unit. Used by the sweep ticker
// to preserve share pools that are still backed by live routes.
//
// A nil return means "the live set could not be determined", never "no route is
// live": SweepSharePools would read the latter as permission to delete every
// pool. group2alias2routes is only built by InitChannelCache, which early-returns
// when common.MemoryCacheEnabled is false, so the index being absent is the
// normal state for a deployment with the memory cache off — the table is read
// directly in that case, and only a failed read yields nil.
func GetActiveRouteStatsPoolKeys() map[routestats.PoolKey]struct{} {
	channelSyncLock.RLock()
	indexed := group2alias2routes
	channelSyncLock.RUnlock()

	if indexed == nil {
		return activeRouteStatsPoolKeysFromDB()
	}
	keep := make(map[routestats.PoolKey]struct{}, len(indexed))
	for group, aliasMap := range indexed {
		for alias := range aliasMap {
			keep[routestats.PoolKey{Group: group, PublicModelAlias: alias}] = struct{}{}
		}
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
	var rows []struct {
		Group            string
		PublicModelAlias string
	}
	if err := dbx.DB.Model(&ChannelModelRoute{}).
		Select(dbx.GroupCol()+" as `group`, public_model_alias").
		Where("enabled = ?", true).
		Group(dbx.GroupCol() + ", public_model_alias").
		Scan(&rows).Error; err != nil {
		common.SysError("failed to load active route stats pool keys: " + err.Error())
		return nil
	}
	keep := make(map[routestats.PoolKey]struct{}, len(rows))
	for _, r := range rows {
		keep[routestats.PoolKey{Group: r.Group, PublicModelAlias: r.PublicModelAlias}] = struct{}{}
	}
	return keep
}
