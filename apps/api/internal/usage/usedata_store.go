package usage

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*model.QuotaData, err error) {
	var quotaDatas []*model.QuotaData
	err = model.DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*model.QuotaData, err error) {
	var quotaDatas []*model.QuotaData
	err = model.DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*model.QuotaData, err error) {
	var quotaDatas []*model.QuotaData
	err = model.DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDatesInternal(startTime int64, endTime int64, username string) (quotaData []*model.QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*model.QuotaData
	err = model.DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

type FlowQuotaData = model.FlowQuotaData

func flowQuotaBaseQuery(startTime int64, endTime int64) *gorm.DB {
	query := model.DB.Table("quota_data").
		Where("use_group <> ''").
		Where("created_at >= ? and created_at <= ?", startTime, endTime)
	return query
}

func getSelfFlowQuotaData(startTime int64, endTime int64, userID int) ([]*FlowQuotaData, error) {
	var rows []*FlowQuotaData
	err := flowQuotaBaseQuery(startTime, endTime).
		Select("token_id, use_group, model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ?", userID).
		Group("token_id, use_group, model_name").
		Order("quota DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, fillFlowTokenNames(rows)
}

func getAdminFlowQuotaData(startTime int64, endTime int64, username string) ([]*FlowQuotaData, error) {
	var rows []*FlowQuotaData
	query := flowQuotaBaseQuery(startTime, endTime)
	if username != "" {
		query = query.Where("username = ?", username)
	}
	err := query.
		Select("user_id, username, use_group, model_name, channel_id, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Group("user_id, username, use_group, model_name, channel_id").
		Order("quota DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, fillFlowChannelNames(rows)
}

func getRootFlowQuotaData(startTime int64, endTime int64, username string) ([]*FlowQuotaData, error) {
	var rows []*FlowQuotaData
	query := flowQuotaBaseQuery(startTime, endTime)
	if username != "" {
		query = query.Where("username = ?", username)
	}
	err := query.
		Select("user_id, username, node_name, token_id, use_group, channel_id, model_name, sum(token_used) as token_used, sum(count) as count, sum(quota) as quota").
		Group("user_id, username, node_name, token_id, use_group, channel_id, model_name").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if err := fillFlowTokenNames(rows); err != nil {
		return nil, err
	}
	if err := fillFlowChannelNames(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func fillFlowTokenNames(rows []*FlowQuotaData) error {
	if len(rows) == 0 {
		return nil
	}
	tokenIds := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.TokenID > 0 {
			tokenIds = append(tokenIds, r.TokenID)
		}
	}
	if len(tokenIds) == 0 {
		return nil
	}
	type tokenNameRow struct {
		Id   int
		Name string
	}
	var tokenNames []tokenNameRow
	if err := model.DB.Table("tokens").Select("id, name").Where("id IN ?", tokenIds).Find(&tokenNames).Error; err != nil {
		return err
	}
	nameMap := make(map[int]string, len(tokenNames))
	for _, t := range tokenNames {
		nameMap[t.Id] = t.Name
	}
	for _, r := range rows {
		if name, ok := nameMap[r.TokenID]; ok {
			r.TokenName = name
		}
	}
	return nil
}

func fillFlowChannelNames(rows []*FlowQuotaData) error {
	if len(rows) == 0 {
		return nil
	}
	channelIds := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.ChannelID > 0 {
			channelIds = append(channelIds, r.ChannelID)
		}
	}
	if len(channelIds) == 0 {
		return nil
	}
	type channelNameRow struct {
		Id   int
		Name string
	}
	var channelNames []channelNameRow
	if common.MemoryCacheEnabled {
		for _, cid := range channelIds {
			if cacheChannel, err := model.CacheGetChannel(cid); err == nil {
				channelNames = append(channelNames, channelNameRow{Id: cid, Name: cacheChannel.Name})
			}
		}
	} else {
		if err := model.DB.Table("channels").Select("id, name").Where("id IN ?", channelIds).Find(&channelNames).Error; err != nil {
			return err
		}
	}
	nameMap := make(map[int]string, len(channelNames))
	for _, c := range channelNames {
		nameMap[c.Id] = c.Name
	}
	for _, r := range rows {
		if name, ok := nameMap[r.ChannelID]; ok {
			r.ChannelName = name
		}
	}
	return nil
}
