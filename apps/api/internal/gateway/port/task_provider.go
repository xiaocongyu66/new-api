// Package port declares the capabilities the gateway consumes for async task polling.
//
// The task capability implements the polling state machine but delegates upstream
// execution to providers through this interface. This inverts the dependency:
// task -> port (this package) <- service (adapter binding) <- relay/channel.
package port

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
)

// TaskResult captures the minimal upstream task status needed by the polling loop.
// It mirrors relay/common.TaskInfo but lives on the consumer side to avoid
// importing relay types into the task capability.
type TaskResult struct {
	TaskID           string
	Status           string
	Url              string
	RemoteUrl        string
	Progress         string
	Reason           string
	CompletionTokens int
	TotalTokens      int
}

// TaskProviderExec is the gateway port for upstream task provider execution.
// Implemented by the service layer (adapter binding); the task capability
// calls this interface instead of importing relay/channel.
type TaskProviderExec interface {
	// Init initializes the provider with channel connection info.
	// Called once per channel before polling tasks on that channel.
	Init(baseURL, apiKey string)

	// FetchTask submits a status query to the upstream provider.
	// baseURL: upstream base URL; key: API key; body: request payload; proxy: optional proxy URL.
	FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error)

	// ParseTaskResult parses the upstream response body into a TaskResult.
	ParseTaskResult(body []byte) (*TaskResult, error)

	// AdjustBillingOnComplete is called when a task reaches a terminal state.
	// Return a positive quota value to trigger delta settlement (top-up/refund);
	// return 0 to keep the pre-charged amount unchanged.
	AdjustBillingOnComplete(task *model.Task, taskResult *TaskResult) int
}

// GetTaskProviderFunc returns the provider executor for a given platform.
// Injected by the service layer at startup.
var GetTaskProviderFunc func(platform string) TaskProviderExec