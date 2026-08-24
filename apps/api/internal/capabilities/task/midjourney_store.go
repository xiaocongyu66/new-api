package task

import (
	"github.com/QuantumNous/new-api/model"
)

// ============================================================
// Midjourney task query & status update methods
// Migrated from model/midjourney.go — logic owner is now this package.
// Model structs (Midjourney, TaskQueryParams) remain in model.
// ============================================================

// GetAllUserTask returns paginated midjourney tasks for a user with filters.
func GetAllUserTask(userId int, startIdx int, num int, queryParams model.TaskQueryParams) []*model.Midjourney {
	var tasks []*model.Midjourney
	query := model.DB.Where("user_id = ?", userId)
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// GetAllTasks returns paginated midjourney tasks with filters (admin).
func GetAllTasks(startIdx int, num int, queryParams model.TaskQueryParams) []*model.Midjourney {
	var tasks []*model.Midjourney
	query := model.DB
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// GetAllUnFinishTasks returns all tasks whose progress != "100%".
func GetAllUnFinishTasks() []*model.Midjourney {
	var tasks []*model.Midjourney
	err := model.DB.Where("progress != ?", "100%").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedMidjourneyTasks reports whether at least one midjourney task
// is still in progress (progress != "100%"). Cheap LIMIT 1 existence check.
func HasUnfinishedMidjourneyTasks() bool {
	var id int
	err := model.DB.Model(&model.Midjourney{}).
		Where("progress != ?", "100%").
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

// GetByOnlyMJId returns a midjourney task by mj_id alone (no user scoping).
func GetByOnlyMJId(mjId string) *model.Midjourney {
	var mj *model.Midjourney
	err := model.DB.Where("mj_id = ?", mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

// GetByMJId returns a midjourney task by user_id + mj_id.
func GetByMJId(userId int, mjId string) *model.Midjourney {
	var mj *model.Midjourney
	err := model.DB.Where("user_id = ? and mj_id = ?", userId, mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

// GetByMJIds returns midjourney tasks by user_id + list of mj_ids.
func GetByMJIds(userId int, mjIds []string) []*model.Midjourney {
	var mj []*model.Midjourney
	err := model.DB.Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

// MjBulkUpdate updates multiple midjourney tasks by mj_id list.
func MjBulkUpdate(mjIds []string, params map[string]any) error {
	return model.DB.Model(&model.Midjourney{}).
		Where("mj_id in (?)", mjIds).
		Updates(params).Error
}

// MjBulkUpdateByTaskIds updates multiple midjourney tasks by internal id list.
func MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	return model.DB.Model(&model.Midjourney{}).
		Where("id in (?)", taskIDs).
		Updates(params).Error
}

// CountAllTasks returns total midjourney tasks for admin query with filters.
func CountAllTasks(queryParams model.TaskQueryParams) int64 {
	var total int64
	query := model.DB.Model(&model.Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for a user with filters.
func CountAllUserTask(userId int, queryParams model.TaskQueryParams) int64 {
	var total int64
	query := model.DB.Model(&model.Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// InsertMidjourneyTask persists a new midjourney task.
func InsertMidjourneyTask(m *model.Midjourney) error {
	return model.DB.Create(m).Error
}

// UpdateMidjourneyTask updates an existing midjourney task.
func UpdateMidjourneyTask(m *model.Midjourney) error {
	return model.DB.Save(m).Error
}

// UpdateMidjourneyBillingState updates quota/token_id/billing_channel_id.
func UpdateMidjourneyBillingState(m *model.Midjourney) error {
	return model.DB.Model(m).
		Select("quota", "token_id", "billing_channel_id").
		Updates(m).Error
}

// GetMidjourneyBillingChannelId returns billing channel id (fallback to channel_id).
func GetMidjourneyBillingChannelId(m *model.Midjourney) int {
	if m.BillingChannelId > 0 {
		return m.BillingChannelId
	}
	return m.ChannelId
}

// UpdateMidjourneyWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
func UpdateMidjourneyWithStatus(m *model.Midjourney, fromStatus string) (bool, error) {
	result := model.DB.Model(m).Where("status = ?", fromStatus).Select("*").Updates(m)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MidjourneyStore is a no-op struct to group midjourney-related operations.
// In Go, we use package-level functions instead, but this exists for API parity
// with other capability stores that use a Store struct.
type MidjourneyStore struct{}

// NewMidjourneyStore returns a new MidjourneyStore (stateless).
func NewMidjourneyStore() *MidjourneyStore {
	return &MidjourneyStore{}
}

// All methods are delegated to package-level functions above.
// This is kept for future extensibility if we need stateful stores.