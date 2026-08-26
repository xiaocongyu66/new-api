package task

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

// TaskErrorWrapper wraps an error into a TaskError with standardized formatting.
func TaskErrorWrapper(err error, code string, statusCode int) *dto.TaskError {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		common.SysLog(fmt.Sprintf("error: %s", text))
		text = common.MaskSensitiveInfo(text)
	}
	taskError := &dto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}
	return taskError
}

// TaskErrorWrapperLocal wraps an error into a TaskError and marks it as a local error.
func TaskErrorWrapperLocal(err error, code string, statusCode int) *dto.TaskError {
	taskErr := TaskErrorWrapper(err, code, statusCode)
	taskErr.LocalError = true
	return taskErr
}

// CoverTaskActionToModelName maps a task platform and action to a model name.
func CoverTaskActionToModelName(platform constant.TaskPlatform, action string) string {
	return strings.ToLower(string(platform)) + "_" + strings.ToLower(action)
}
