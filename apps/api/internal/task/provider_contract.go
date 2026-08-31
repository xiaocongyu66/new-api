// Async-task polling port. The task capability owns the polling state machine
// but delegates upstream execution to providers through TaskProviderExec, which
// inverts the dependency: task -> this port <- relay/channel (via the binding in
// bind_provider.go, wired from main).
//
// Submission has no port: the handler calls relay.RelayTaskSubmit directly, the
// same way every other relay path in handle_relay.go does.
package task

import "net/http"

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

// TaskProviderExec is the gateway port for upstream task provider execution (polling).
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
	AdjustBillingOnComplete(task *Task, taskResult *TaskResult) int

	// ConvertToOpenAIVideo converts a task to OpenAI Video API format.
	// Returns the converted video data or an error.
	ConvertToOpenAIVideo(task *Task) ([]byte, error)
}

// GetTaskProviderFunc returns the provider executor for a given platform (polling).
var GetTaskProviderFunc func(platform string) TaskProviderExec

// Task is the task record providers operate on. Like channel.Channel it
// stays unconstrained here: naming the concrete record would make this
// interface package depend on the task domain, which imports this package.
