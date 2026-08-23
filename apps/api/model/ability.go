package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

func deleteAbilitiesWithTx(tx *gorm.DB, channelID int) error {
	if tx == nil {
		return errors.New("ability deletion requires a transaction")
	}
	return tx.Where("channel_id = ?", channelID).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return updateAbilityStatusWithTx(DB, channelId, status)
}

// updateAbilityStatusWithTx is the tx-aware form of UpdateAbilityStatus. It
// writes the enabled column for every ability row of the channel through the
// given transaction so callers can commit it together with the channel status
// and the gateway routing revision bump.
func updateAbilityStatusWithTx(tx *gorm.DB, channelId int, status bool) error {
	return tx.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

// updateAbilityStatusByTagWithTx is the tx-aware form of
// UpdateAbilityStatusByTag. It flips the enabled column for every ability
// row carrying the given tag inside the outer transaction so the channel
// status update and the routing revision bump commit together.
func updateAbilityStatusByTagWithTx(tx *gorm.DB, tag string, status bool) error {
	return tx.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
}

// updateAbilityStatusByModelWithTx is the tx-aware form of
// DisableChannelModel. It flips the enabled column for every ability
// row matching both the given channel_id and model_name inside the outer
// transaction. It deliberately does NOT filter on group, so a single
// channel+model pair across all groups has its enabled status flipped —
// a single dead model on an otherwise healthy channel should not cost the
// channel its other models.
func updateAbilityStatusByModelWithTx(tx *gorm.DB, channelID int, modelName string, status bool) error {
	return tx.Model(&Ability{}).Where("channel_id = ? AND model = ?", channelID, modelName).Select("enabled").Update("enabled", status).Error
}

// DisableChannelModel flips the enabled status of every ability row matching
// the given channel_id and model_name inside one MutateGatewayRouting revision,
// so the ability write and the gateway routing revision bump commit atomically.
// A single disabled model on an otherwise healthy channel should not cost the
// channel its other models; this helper spans ALL groups deliberately.
// Returns an error if modelName is empty, as there is nothing specific to disable.
func DisableChannelModel(channelID int, modelName string) error {
	if modelName == "" {
		return fmt.Errorf("model name must not be empty")
	}
	_, err := MutateGatewayRouting(func(tx *gorm.DB) error {
		if err := updateAbilityStatusByModelWithTx(tx, channelID, modelName, false); err != nil {
			return err
		}
		if err := SyncChannelModelRoutesWithTx(tx, channelID); err != nil {
			return err
		}
		return nil
	})
	return err
}

// UpdateAbilityStatusByTag remains the public convenience wrapper. It
// delegates to the tx-aware form with the shared DB handle so callers that
// are not inside a MutateGatewayRouting transaction keep working.
func UpdateAbilityStatusByTag(tag string, status bool) error {
	return updateAbilityStatusByTagWithTx(DB, tag, status)
}

// updateAbilityByTagWithTx is the tx-aware form of UpdateAbilityByTag. It
// writes the tag/priority/weight columns for every ability row carrying the
// given tag inside the outer transaction.
func updateAbilityByTagWithTx(tx *gorm.DB, tag string, newTag *string, priority *int64, weight *uint) error {
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	return tx.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

// UpdateAbilityByTag remains the public convenience wrapper. It delegates to
// the tx-aware form with the shared DB handle.
func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	return updateAbilityByTagWithTx(DB, tag, newTag, priority, weight)
}

// deleteAbilitiesByChannelIDsWithTx deletes every ability row whose
// channel_id is in ids inside the given transaction, so batch channel
// deletion and the routing revision bump commit atomically.
func deleteAbilitiesByChannelIDsWithTx(tx *gorm.DB, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	return tx.Where("channel_id in (?)", ids).Delete(&Ability{}).Error
}

// deleteAbilitiesByTagWithTx deletes every ability row carrying the given tag
// inside the given transaction. Used when channels sharing a tag are removed.
func deleteAbilitiesByTagWithTx(tx *gorm.DB, tag string) error {
	return tx.Where("tag = ?", tag).Delete(&Ability{}).Error
}

// deleteAbilitiesByStatusWithTx deletes every ability row whose channel has
// the given status, inside the given transaction.
func deleteAbilitiesByStatusWithTx(tx *gorm.DB, status int64) error {
	return tx.Where("channel_id IN (SELECT id FROM channels WHERE status = ?)", status).Delete(&Ability{}).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
