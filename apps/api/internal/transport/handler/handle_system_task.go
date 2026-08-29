package handler

import (
	ops "github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/internal/common"
)

func CreateLogCleanupSystemTask(c contract.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}

	task, err := ops.StartLogCleanupTask(targetTimestamp)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    task.ToResponse(),
	})
}

func GetCurrentSystemTask(c contract.Context) {
	taskType := c.Query("type")
	if taskType == "" {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "type is required",
		})
		return
	}

	task, err := ops.GetActiveSystemTask(taskType)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if task == nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": true,
			"message": "",
			"data":    nil,
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    task.ToResponse(),
	})
}

func ListSystemTasks(c contract.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))

	tasks, err := ops.ListSystemTasks(limit)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	responses := make([]ops.SystemTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		responses = append(responses, task.ToResponse())
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    responses,
	})
}

func GetSystemTask(c contract.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "task id is required",
		})
		return
	}

	task, err := ops.GetSystemTaskByTaskID(taskID)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if task == nil {
		_ = c.JSON(http.StatusNotFound, common.H{
			"success": false,
			"message": "task not found",
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    task.ToResponse(),
	})
}
