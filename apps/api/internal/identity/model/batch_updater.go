package model

import (
	channelmodel "github.com/QuantumNous/new-api/internal/catalog/model"
	rootmodel "github.com/QuantumNous/new-api/model"
)

type batchUpdater struct{}

func (batchUpdater) IncreaseTokenQuota(id int, delta int) error {
	return increaseTokenQuota(id, delta)
}

func (batchUpdater) UpdateChannelUsedQuota(id int, delta int) {
	channelmodel.UpdateChannelUsedQuota(id, delta)
}

func (batchUpdater) UpdateUserQuotaUsedQuotaAndRequestCount(userId int, quota int, usedQuota int, requestCount int) error {
	updateUserQuotaUsedQuotaAndRequestCount(userId, quota, usedQuota, requestCount)
	return nil
}

func init() {
	rootmodel.RegisterBatchUpdater(batchUpdater{})
}
