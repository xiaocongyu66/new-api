package task

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// ============================================================
// System task CAS state machine, lock, and scheduling methods
// Migrated from model/system_task.go — logic owner is now this package.
// Model structs (SystemTask, SystemTaskLock, SystemTaskResponse, SystemTaskStatus)
// and their methods (DecodePayload, DecodeState, ToResponse, BeforeCreate) remain in model.
// ============================================================

var ErrSystemTaskLockLost = errors.New("system task lock lost")

// activeSystemTaskStatuses returns the statuses considered "active" for queries.
// Unexported in model, kept here to avoid cross-package dependency.
func activeSystemTaskStatuses() []string {
	return []string{string(model.SystemTaskStatusPending), string(model.SystemTaskStatusRunning)}
}

// CreateSystemTask creates a new system task with marshaled payload/state.
func CreateSystemTask(taskType string, payload any, state any) (*model.SystemTask, error) {
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, err
	}
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return nil, err
	}

	task := &model.SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		Status:    model.SystemTaskStatusPending,
		ActiveKey: &taskType,
		Payload:   payloadText,
		State:     stateText,
	}

	if err := model.DB.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

// GetSystemTaskByTaskID returns a system task by task_id, or nil if not found.
func GetSystemTaskByTaskID(taskID string) (*model.SystemTask, error) {
	var task model.SystemTask
	if err := model.DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// GetActiveSystemTask returns the most recent active (pending/running) task of the given type.
func GetActiveSystemTask(taskType string) (*model.SystemTask, error) {
	var task model.SystemTask
	err := model.DB.Where("type = ? AND status IN ?", taskType, activeSystemTaskStatuses()).
		Order("id desc").
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// FindEarliestPendingSystemTasks returns the earliest pending task for each type.
func FindEarliestPendingSystemTasks(taskTypes []string) (map[string]*model.SystemTask, error) {
	tasksByType := map[string]*model.SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := model.DB.Model(&model.SystemTask{}).
		Select("MIN(id)").
		Where("type IN ? AND status = ?", taskTypes, model.SystemTaskStatusPending).
		Group("type")
	var tasks []*model.SystemTask
	if err := model.DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

// ListSystemTasks returns the most recent system tasks (default 20, max 100).
func ListSystemTasks(limit int) ([]*model.SystemTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var tasks []*model.SystemTask
	err := model.DB.Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetLatestSystemTask returns the most recent task row of the given type (any status).
func GetLatestSystemTask(taskType string) (*model.SystemTask, error) {
	var task model.SystemTask
	err := model.DB.Where("type = ?", taskType).Order("id desc").First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// GetLatestSystemTasks returns the most recent task row for each type (any status).
func GetLatestSystemTasks(taskTypes []string) (map[string]*model.SystemTask, error) {
	tasksByType := map[string]*model.SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := model.DB.Model(&model.SystemTask{}).
		Select("MAX(id)").
		Where("type IN ?", taskTypes).
		Group("type")
	var tasks []*model.SystemTask
	if err := model.DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

// ClaimSystemTask attempts to claim a pending task for execution with a lock.
// Returns (task, true, nil) if claimed, (nil, false, nil) if not claimed,
// or (nil, false, err) on error.
func ClaimSystemTask(id int64, taskType string, runnerID string, lockUntil int64) (*model.SystemTask, bool, error) {
	now := common.GetTimestamp()
	var task model.SystemTask
	if err := model.DB.Where("id = ? AND type = ? AND status = ?", id, taskType, model.SystemTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	acquired, expiredTaskID, err := acquireSystemTaskLock(taskType, task.TaskID, runnerID, now, lockUntil)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	if expiredTaskID != "" && expiredTaskID != task.TaskID {
		if err := MarkSystemTaskLeaseExpired(expiredTaskID); err != nil {
			_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
			return nil, false, err
		}
	}

	result := model.DB.Model(&model.SystemTask{}).
		Where("id = ? AND type = ? AND status = ?", id, taskType, model.SystemTaskStatusPending).
		Updates(map[string]any{
			"status":     model.SystemTaskStatusRunning,
			"locked_by":  runnerID,
			"updated_at": now,
		})
	if result.Error != nil {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, nil
	}

	if err := model.DB.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

// acquireSystemTaskLock attempts to acquire the per-type lock.
// Returns (acquired, expiredTaskID, error).
func acquireSystemTaskLock(taskType string, taskID string, lockedBy string, now int64, lockUntil int64) (bool, string, error) {
	lock := &model.SystemTaskLock{
		Type:        taskType,
		TaskID:      taskID,
		LockedBy:    lockedBy,
		LockedUntil: lockUntil,
		UpdatedAt:   now,
	}
	if err := model.DB.Create(lock).Error; err == nil {
		return true, "", nil
	}

	var existing model.SystemTaskLock
	err := model.DB.Where("type = ?", taskType).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, "", nil
		}
		return false, "", err
	}
	if existing.LockedUntil >= now {
		return false, "", nil
	}

	result := model.DB.Model(&model.SystemTaskLock{}).
		Where("type = ? AND locked_until < ?", taskType, now).
		Updates(map[string]any{
			"task_id":      taskID,
			"locked_by":    lockedBy,
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, "", result.Error
	}
	if result.RowsAffected == 0 {
		return false, "", nil
	}
	return true, existing.TaskID, nil
}

// UpdateSystemTaskState updates the state JSON of a running task with lock validation.
func UpdateSystemTaskState(taskID string, lockedBy string, state any) error {
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := model.DB.Model(&model.SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, model.SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Updates(map[string]any{
			"state":      stateText,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

// RenewSystemTaskLock extends the lock TTL for a running task.
func RenewSystemTaskLock(taskID string, lockedBy string, lockUntil int64) error {
	now := common.GetTimestamp()
	result := model.DB.Model(&model.SystemTaskLock{}).
		Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
		Updates(map[string]any{
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

// MarkSystemTaskLeaseExpired marks a running task as failed due to lease expiration.
func MarkSystemTaskLeaseExpired(taskID string) error {
	result := model.DB.Model(&model.SystemTask{}).
		Where("task_id = ? AND status = ?", taskID, model.SystemTaskStatusRunning).
		Updates(map[string]any{
			"status":     model.SystemTaskStatusFailed,
			"active_key": nil,
			"error":      "task lease expired",
			"updated_at": common.GetTimestamp(),
		})
	return result.Error
}

// ExpireStaleSystemTaskLocks finds and cleans up all stale locks before now.
func ExpireStaleSystemTaskLocks(now int64) error {
	var locks []*model.SystemTaskLock
	if err := model.DB.Where("locked_until < ?", now).Find(&locks).Error; err != nil {
		return err
	}
	for _, lock := range locks {
		if err := MarkSystemTaskLeaseExpired(lock.TaskID); err != nil {
			return err
		}
		result := model.DB.Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until < ?", lock.Type, lock.TaskID, lock.LockedBy, now).
			Delete(&model.SystemTaskLock{})
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

// ReleaseSystemTaskLock releases the lock for a task.
func ReleaseSystemTaskLock(taskID string, lockedBy string) error {
	result := model.DB.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&model.SystemTaskLock{})
	return result.Error
}

// FinishSystemTask finalizes a running task with result/error and releases the lock.
func FinishSystemTask(taskID string, lockedBy string, status model.SystemTaskStatus, resultPayload any, errorMessage string) error {
	resultText, err := marshalSystemTaskJSON(resultPayload)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := model.DB.Model(&model.SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, model.SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Updates(map[string]any{
			"status":     status,
			"active_key": nil,
			"result":     resultText,
			"error":      errorMessage,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return ReleaseSystemTaskLock(taskID, lockedBy)
}

// JSON marshal/unmarshal helpers (mirrored from model, kept internal).
func marshalSystemTaskJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GenerateSystemTaskID generates a random system task ID.
func GenerateSystemTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "systask_" + key, nil
}

// DecodeSystemTaskJSONString is an exported wrapper for model forwarding.
func DecodeSystemTaskJSONString(data string, v any) error {
	return decodeSystemTaskJSONString(data, v)
}

// DecodeSystemTaskJSONValue is an exported wrapper for model forwarding.
func DecodeSystemTaskJSONValue(data string) any {
	return decodeSystemTaskJSONValue(data)
}

func decodeSystemTaskJSONString(data string, v any) error {
	if data == "" {
		return nil
	}
	return common.UnmarshalJsonStr(data, v)
}

func decodeSystemTaskJSONValue(data string) any {
	if data == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(data, &value); err != nil {
		return data
	}
	return value
}

// SystemTaskStore is a no-op struct for API parity with other capability stores.
type SystemTaskStore struct{}

// NewSystemTaskStore returns a new SystemTaskStore (stateless).
func NewSystemTaskStore() *SystemTaskStore {
	return &SystemTaskStore{}
}
