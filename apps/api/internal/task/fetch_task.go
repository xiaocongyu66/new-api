package task

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/gateway/port"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// maxTaskDurationSeconds mirrors relaycommon.MaxTaskDurationSeconds to avoid import.
const maxTaskDurationSeconds = 60

// relaycommonTaskInfo mirrors relay/common.TaskInfo for internal use.
type relaycommonTaskInfo struct {
	TaskID           string
	Status           string
	Url              string
	RemoteUrl        string
	Progress         string
	Reason           string
	CompletionTokens int
	TotalTokens      int
}

// fetchRespBuilders maps relay modes to response body builders.
var fetchRespBuilders = map[int]func(c contract.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

// FetchTask dispatches the fetch request to the appropriate builder based on relay mode.
func FetchTask(c contract.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		return &dto.TaskError{
			Code:       "invalid_relay_mode",
			Message:    "invalid relay mode",
			StatusCode: http.StatusBadRequest,
		}
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.SetHeader("Content-Type", "application/json")
	_, err := io.Copy(c.ResponseWriter(), bytes.NewBuffer(respBody))
	if err != nil {
		return &dto.TaskError{
			Code:       "copy_response_body_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	return nil
}

func sunoFetchRespBodyBuilder(c contract.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}
	err := c.BindJSON(&condition)
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "invalid_request",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
		}
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := GetByTaskIds(userId, condition.IDs)
		if err != nil {
			return nil, &dto.TaskError{
				Code:       "get_tasks_failed",
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
		for _, task := range taskModels {
			tasks = append(tasks, relayTaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return respBody, nil
}

func sunoFetchByIDRespBodyBuilder(c contract.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := GetByTaskId(userId, taskId)
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "get_task_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	if !exist {
		return nil, &dto.TaskError{
			Code:       "task_not_exist",
			Message:    "task_not_exist",
			StatusCode: http.StatusBadRequest,
		}
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: relayTaskModel2Dto(originTask),
	})
	return respBody, nil
}

func videoFetchByIDRespBodyBuilder(c contract.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := GetByTaskId(userId, taskId)
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "get_task_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	if !exist {
		return nil, &dto.TaskError{
			Code:       "task_not_exist",
			Message:    "task_not_exist",
			StatusCode: http.StatusBadRequest,
		}
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.RequestURI(), "/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		return realtimeResp, nil
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		platform := string(originTask.Platform)
		provider := port.GetTaskProviderFunc(platform)
		if provider == nil {
			return nil, &dto.TaskError{
				Code:       "not_implemented",
				Message:    fmt.Sprintf("provider not found for platform: %s", platform),
				StatusCode: http.StatusNotImplemented,
			}
		}
		openAIVideoData, err := provider.ConvertToOpenAIVideo(originTask)
		if err != nil {
			return nil, &dto.TaskError{
				Code:       "convert_to_openai_video_failed",
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
		return openAIVideoData, nil
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: relayTaskModel2Dto(originTask),
	})
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "marshal_response_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	return respBody, nil
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy

	// Get provider for the task's platform (e.g., "suno", "kling", "vertex", etc.)
	platform := string(task.Platform)
	provider := port.GetTaskProviderFunc(platform)
	if provider == nil {
		return nil
	}

	resp, err := provider.FetchTask(baseURL, channelModel.Key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	taskResult, err := provider.ParseTaskResult(body)
	if err != nil || taskResult == nil {
		return nil
	}

	ti := &relaycommonTaskInfo{
		TaskID:           taskResult.TaskID,
		Status:           taskResult.Status,
		Url:              taskResult.Url,
		RemoteUrl:        taskResult.RemoteUrl,
		Progress:         taskResult.Progress,
		Reason:           taskResult.Reason,
		CompletionTokens: taskResult.CompletionTokens,
		TotalTokens:      taskResult.TotalTokens,
	}

	snap := task.Snapshot()

	// 将上游最新状态更新到 task
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = BuildProxyURL(task.TaskID)
	}

	if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

// relayTaskModel2Dto converts a task model to DTO (internal version to avoid import cycle).
func relayTaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}
