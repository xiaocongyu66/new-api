package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// ChannelModelRoute represents a static weight row for a route unit under a public model alias.
type ChannelModelRoute struct {
	Id               int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Group            string `json:"group" gorm:"type:varchar(64);uniqueIndex:idx_route_unit,priority:1"`
	PublicModelAlias string `json:"public_model_alias" gorm:"type:varchar(255);uniqueIndex:idx_route_unit,priority:2"`
	ChannelId        int    `json:"channel_id" gorm:"uniqueIndex:idx_route_unit,priority:3;index:idx_route_channel"`
	KeyIndex         int    `json:"key_index" gorm:"uniqueIndex:idx_route_unit,priority:4"`
	UpstreamModel    string `json:"upstream_model" gorm:"type:varchar(255);uniqueIndex:idx_route_unit,priority:5"`
	StaticWeight     int    `json:"static_weight"`
	Enabled          bool   `json:"enabled"`
}

// ExpandChannelModelRoutes expands a channel into its route unit rows.
// It is a pure function with no side effects.
func ExpandChannelModelRoutes(channel *Channel) []ChannelModelRoute {
	if channel == nil {
		return nil
	}

	models := channel.GetModels()
	groups := channel.GetGroups()
	if len(models) == 0 || len(groups) == 0 {
		return nil
	}

	// Parse model mapping: origin -> upstream
	var mapping map[string]string
	mappingStr := channel.GetModelMapping()
	if mappingStr != "" {
		if err := common.UnmarshalJsonStr(mappingStr, &mapping); err != nil {
			common.SysError(fmt.Sprintf("failed to unmarshal model_mapping: channel_id=%d, error=%v", channel.Id, err))
			mapping = nil
		}
	}

	// Determine key indices
	var keyIndices []int
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		if len(keys) == 0 {
			return nil
		}
		keyIndices = make([]int, len(keys))
		for i := range keys {
			keyIndices[i] = i
		}
	} else {
		keyIndices = []int{0}
	}

	routes := make([]ChannelModelRoute, 0, len(models)*len(groups)*len(keyIndices))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		upstream := model
		if mapping != nil {
			if v, ok := mapping[model]; ok && v != "" {
				upstream = v
			}
		}
		for _, group := range groups {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			for _, keyIndex := range keyIndices {
				routes = append(routes, ChannelModelRoute{
					Group:            group,
					PublicModelAlias: model,
					ChannelId:        channel.Id,
					KeyIndex:         keyIndex,
					UpstreamModel:    upstream,
					StaticWeight:     100,
					Enabled:          true,
				})
			}
		}
	}
	return routes
}

// SyncChannelModelRoutesWithTx synchronizes the route rows for a single channel inside a transaction.
// If the channel does not exist, all rows for that channelID are removed and nil is returned.
func SyncChannelModelRoutesWithTx(tx *gorm.DB, channelID int) error {
	if tx == nil {
		return gorm.ErrInvalidTransaction
	}

	var channel Channel
	err := tx.First(&channel, channelID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Where("channel_id = ?", channelID).Delete(&ChannelModelRoute{}).Error
		}
		return err
	}

	expected := ExpandChannelModelRoutes(&channel)
	if len(expected) == 0 {
		return tx.Where("channel_id = ?", channelID).Delete(&ChannelModelRoute{}).Error
	}

	// Fetch existing rows for this channel
	var existing []ChannelModelRoute
	if err := tx.Where("channel_id = ?", channelID).Find(&existing).Error; err != nil {
		return err
	}

	// Build sets keyed by composite unique fields for diff
	type routeUnitKey struct {
		Group            string
		PublicModelAlias string
		ChannelId        int
		KeyIndex         int
		UpstreamModel    string
	}

	existingSet := make(map[routeUnitKey]struct{}, len(existing))
	for _, r := range existing {
		existingSet[routeUnitKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}] = struct{}{}
	}

	// Build expected set and collect missing
	expectedSet := make(map[routeUnitKey]struct{}, len(expected))
	missing := make([]ChannelModelRoute, 0, len(expected))
	for _, r := range expected {
		key := routeUnitKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}
		expectedSet[key] = struct{}{}
		if _, ok := existingSet[key]; !ok {
			missing = append(missing, r)
		}
	}

	// Delete stale rows (in existing but not in expected) — batch by primary key
	staleIDs := make([]int, 0, len(existingSet))
	for _, r := range existing {
		key := routeUnitKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}
		if _, ok := expectedSet[key]; !ok {
			staleIDs = append(staleIDs, r.Id)
		}
	}
	if len(staleIDs) > 0 {
		if err := tx.Where("id IN ?", staleIDs).Delete(&ChannelModelRoute{}).Error; err != nil {
			return err
		}
	}

	// Insert missing rows. MutateGatewayRouting serializes all route mutations in a single
	// transaction, so the missing set is computed inside the same tx and conflicts cannot occur.
	if len(missing) > 0 {
		for _, chunk := range lo.Chunk(missing, 50) {
			if err := tx.Create(&chunk).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteChannelModelRoutesByChannelIDsWithTx deletes all route rows for the given channel IDs in a transaction.
func DeleteChannelModelRoutesByChannelIDsWithTx(tx *gorm.DB, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if tx == nil {
		return gorm.ErrInvalidTransaction
	}
	return tx.Where("channel_id IN ?", ids).Delete(&ChannelModelRoute{}).Error
}

// SeedChannelModelRoutes seeds route rows for all existing channels.
// Used at startup to migrate existing channel/ability data into route units.
// Runs atomically inside a transaction so a mid-seed failure rolls back fully
// and the next startup reseeds cleanly.
func SeedChannelModelRoutes() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := tx.Find(&channels).Error; err != nil {
			return err
		}
		for _, ch := range channels {
			if err := SyncChannelModelRoutesWithTx(tx, ch.Id); err != nil {
				return err
			}
		}
		return nil
	})
}
