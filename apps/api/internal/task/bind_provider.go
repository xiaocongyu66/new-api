package task

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
)

// TaskPollingAdaptor defines the minimal adaptor interface needed for polling.
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	AdjustBillingOnComplete(taskModel *Task, taskResult *relaycommon.TaskInfo) int
}

// GetTaskAdaptorFunc is injected by main to get a TaskPollingAdaptor for a platform.
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// taskProviderBinding adapts TaskPollingAdaptor to TaskProviderExec.
type taskProviderBinding struct {
	adaptor TaskPollingAdaptor
}

func (b *taskProviderBinding) Init(baseURL, apiKey string) {
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: baseURL,
	}
	info.ApiKey = apiKey
	b.adaptor.Init(info)
}

func (b *taskProviderBinding) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	return b.adaptor.FetchTask(baseURL, key, body, proxy)
}

func (b *taskProviderBinding) ParseTaskResult(body []byte) (*TaskResult, error) {
	relayResult, err := b.adaptor.ParseTaskResult(body)
	if err != nil {
		return nil, err
	}
	if relayResult == nil {
		return nil, nil
	}
	return &TaskResult{
		TaskID:           relayResult.TaskID,
		Status:           relayResult.Status,
		Url:              relayResult.Url,
		RemoteUrl:        relayResult.RemoteUrl,
		Progress:         relayResult.Progress,
		Reason:           relayResult.Reason,
		CompletionTokens: relayResult.CompletionTokens,
		TotalTokens:      relayResult.TotalTokens,
	}, nil
}

func (b *taskProviderBinding) AdjustBillingOnComplete(taskModel *Task, taskResult *TaskResult) int {
	relayResult := &relaycommon.TaskInfo{
		TaskID:           taskResult.TaskID,
		Status:           taskResult.Status,
		Url:              taskResult.Url,
		RemoteUrl:        taskResult.RemoteUrl,
		Progress:         taskResult.Progress,
		Reason:           taskResult.Reason,
		CompletionTokens: taskResult.CompletionTokens,
		TotalTokens:      taskResult.TotalTokens,
	}
	return b.adaptor.AdjustBillingOnComplete(taskModel, relayResult)
}

func (b *taskProviderBinding) ConvertToOpenAIVideo(taskModel *Task) ([]byte, error) {
	type converter interface {
		ConvertToOpenAIVideo(*Task) ([]byte, error)
	}
	if c, ok := b.adaptor.(converter); ok {
		return c.ConvertToOpenAIVideo(taskModel)
	}
	return nil, fmt.Errorf("adaptor does not implement ConvertToOpenAIVideo")
}

// GetTaskProviderFuncBinding wires the gateway port to the channel adaptors.
func GetTaskProviderFuncBinding() func(platform string) TaskProviderExec {
	return func(platform string) TaskProviderExec {
		a := GetTaskAdaptorFunc(constant.TaskPlatform(platform))
		if a == nil {
			return nil
		}
		return &taskProviderBinding{adaptor: a}
	}
}
