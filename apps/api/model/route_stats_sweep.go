package model

import (
	"github.com/QuantumNous/new-api/pkg/routestats"
)

// GetActiveRouteStatsPoolKeys returns the set of PoolKey (group, alias) pairs
// that currently have at least one enabled route unit. Used by the sweep ticker
// to preserve share pools that are still backed by live routes.
func GetActiveRouteStatsPoolKeys() map[routestats.PoolKey]struct{} {
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2alias2routes == nil {
		return nil
	}
	keep := make(map[routestats.PoolKey]struct{}, len(group2alias2routes))
	for group, aliasMap := range group2alias2routes {
		for alias := range aliasMap {
			keep[routestats.PoolKey{Group: group, PublicModelAlias: alias}] = struct{}{}
		}
	}
	return keep
}