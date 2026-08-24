package service

import (
	"context"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/capabilities/task"
	"github.com/QuantumNous/new-api/internal/gateway/port"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// TaskPollingAdaptor defines the minimal adaptor interface needed for polling.
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	AdjustBillingOnComplete(taskModel *model.Task, taskResult *relaycommon.TaskInfo) int
}

// GetTaskAdaptorFunc is injected by main to get a TaskPollingAdaptor for a platform.
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// TaskPollSummary mirrors task.TaskPollSummary for backward compatibility.
type TaskPollSummary = task.TaskPollSummary

// RunTaskPollingOnce delegates to the capability layer.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	return task.RunTaskPollingOnce(ctx, report)
}

// taskProviderBinding adapts TaskPollingAdaptor to port.TaskProviderExec.
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

func (b *taskProviderBinding) ParseTaskResult(body []byte) (*port.TaskResult, error) {
	relayResult, err := b.adaptor.ParseTaskResult(body)
	if err != nil {
		return nil, err
	}
	if relayResult == nil {
		return nil, nil
	}
	return &port.TaskResult{
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

func (b *taskProviderBinding) AdjustBillingOnComplete(taskModel *model.Task, taskResult *port.TaskResult) int {
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

// GetTaskProviderFuncBinding wires the gateway port to the channel adaptors.
func GetTaskProviderFuncBinding() func(platform string) port.TaskProviderExec {
	return func(platform string) port.TaskProviderExec {
		a := GetTaskAdaptorFunc(constant.TaskPlatform(platform))
		if a == nil {
			return nil
		}
		return &taskProviderBinding{adaptor: a}
	}
}