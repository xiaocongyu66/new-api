package task

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/common/dbx"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// TaskGetAllUserTask returns tasks for a given user with filters
func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams model.SyncTaskQueryParams) []*model.Task {
	var tasks []*model.Task
	query := dbx.DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	err := query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// TaskGetAllTasks returns all tasks with filters (admin usage)
func TaskGetAllTasks(startIdx int, num int, queryParams model.SyncTaskQueryParams) []*model.Task {
	var tasks []*model.Task
	query := dbx.DB

	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// GetTimedOutUnfinishedTasks returns tasks that have timed out but not finished
func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*model.Task {
	var tasks []*model.Task
	err := dbx.DB.Where("progress != ?", "100%").
		Where("status NOT IN ?", []string{model.TaskStatusFailure, model.TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// GetAllUnFinishSyncTasks returns all unfinished sync tasks (Suno/video)
func GetAllUnFinishSyncTasks(limit int) []*model.Task {
	var tasks []*model.Task
	err := dbx.DB.Where("progress != ?", "100%").Where("status != ?", model.TaskStatusFailure).Where("status != ?", model.TaskStatusSuccess).Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedSyncTasks reports whether at least one async (Suno/video) task is still in progress
func HasUnfinishedSyncTasks() bool {
	var id int64
	err := dbx.DB.Model(&model.Task{}).
		Where("progress != ?", "100%").
		Where("status != ?", model.TaskStatusFailure).
		Where("status != ?", model.TaskStatusSuccess).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

// GetByTaskId returns a task by user ID and task ID
func GetByTaskId(userId int, taskId string) (*model.Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task model.Task
	err := dbx.DB.Where("user_id = ? AND task_id = ?", userId, taskId).First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &task, true, nil
}

// GetByTaskIds returns tasks by user ID and task IDs
func GetByTaskIds(userId int, taskIds []any) ([]*model.Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var tasks []*model.Task
	err := dbx.DB.Where("user_id = ? AND task_id IN ?", userId, taskIds).Find(&tasks).Error
	return tasks, err
}

// Insert creates a new task record
func Insert(task *model.Task) error {
	return dbx.DB.Create(task).Error
}

// TaskCountAllTasks returns total tasks matching query params (admin usage)
func TaskCountAllTasks(queryParams model.SyncTaskQueryParams) int64 {
	var total int64
	query := dbx.DB.Model(&model.Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams model.SyncTaskQueryParams) int64 {
	var total int64
	query := dbx.DB.Model(&model.Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// recordExist checks if an error indicates record not found
func recordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}
