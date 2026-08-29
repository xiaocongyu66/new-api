// Package port declares the capabilities the gateway consumes for async task polling and submission.
//
// The task capability implements the polling/submission state machine but delegates upstream
// execution to providers through this interface. This inverts the dependency:
// task -> port (this package) <- service (adapter binding) <- relay/channel.
package task

import (
	channel "github.com/QuantumNous/new-api/internal/catalog"
	"net/http"

	"github.com/QuantumNous/new-api/internal/dto"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
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

// TaskSubmitProvider is the gateway port for upstream task submission.
// Implemented by the service layer (adapter binding); the task capability
// calls this interface instead of importing relay/channel.
type TaskSubmitProvider interface {
	// Init initializes the provider with channel connection info.
	Init(baseURL, apiKey string)

	// GetPlatform determines the task platform and action from the request context.
	GetPlatform(c contract.Context) (platform string, action string)

	// ValidateRequest validates the request and sets the action in info.
	// Returns a TaskError if validation fails.
	ValidateRequest(c contract.Context, info *SubmitInfo) *dto.TaskError

	// ModelMappedHelper applies channel model mapping.
	ModelMappedHelper(c contract.Context, info *SubmitInfo) error

	// ModelPriceHelper calculates the model price.
	ModelPriceHelper(c contract.Context, info *SubmitInfo) (*PriceData, error)

	// EstimateBilling estimates additional billing ratios (duration, resolution, etc).
	EstimateBilling(c contract.Context, info *SubmitInfo) map[string]float64

	// PreConsumeBilling performs pre-consumption billing.
	// Returns a TaskError if pre-consumption fails.
	PreConsumeBilling(c contract.Context, info *SubmitInfo, quota int) *dto.TaskError

	// BuildRequestBody builds the upstream request body.
	BuildRequestBody(c contract.Context, info *SubmitInfo) ([]byte, error)

	// DoRequest sends the request to upstream.
	DoRequest(c contract.Context, info *SubmitInfo, body []byte) (*http.Response, error)

	// DoResponse parses the upstream response.
	DoResponse(c contract.Context, resp *http.Response, info *SubmitInfo) (upstreamTaskID string, taskData []byte, taskErr *dto.TaskError)

	// AdjustBillingOnSubmit adjusts billing based on upstream response.
	// Returns adjusted ratios; empty map means no adjustment.
	AdjustBillingOnSubmit(info *SubmitInfo, taskData []byte) map[string]float64
}

// SubmitInfo carries the minimal submission context needed by the capability.
// It mirrors relay/common.RelayInfo but lives on the consumer side to avoid
// importing relay types into the task capability.
type SubmitInfo struct {
	UserId            int
	TokenId           int
	TokenGroup        string
	SubscriptionId    int
	OriginModelName   string
	UpstreamModelName string
	Action            string
	PublicTaskID      string
	OriginTaskID      string
	LockedChannel     *channel.Channel
	ChannelId         int
	ChannelType       int
	ChannelBaseUrl    string
	ApiKey            string
	PriceData         *PriceData
	Billing           *BillingInfo
	ForcePreConsume   bool
	IsModelMapped     bool
}

// PriceData mirrors the pricing data needed for submission.
type PriceData struct {
	Quota          int
	ModelPrice     float64
	GroupRatio     float64
	ModelRatio     float64
	OtherRatiosMap map[string]float64
	FreeModel      bool
	UsePrice       bool
}

func (p *PriceData) AddOtherRatio(key string, value float64) {
	if p.OtherRatiosMap == nil {
		p.OtherRatiosMap = make(map[string]float64)
	}
	p.OtherRatiosMap[key] = value
}

func (p *PriceData) OtherRatios() map[string]float64 {
	if p.OtherRatiosMap == nil {
		return map[string]float64{}
	}
	return p.OtherRatiosMap
}

func (p *PriceData) ReplaceOtherRatios(ratios map[string]float64) bool {
	p.OtherRatiosMap = ratios
	return true
}

func (p *PriceData) RemoveOtherRatiosFromFloat(base float64) float64 {
	if p.OtherRatiosMap == nil {
		return base
	}
	for _, v := range p.OtherRatiosMap {
		base /= v
	}
	return base
}

func (p *PriceData) ApplyOtherRatiosToFloat(base float64) float64 {
	if p.OtherRatiosMap == nil {
		return base
	}
	for _, v := range p.OtherRatiosMap {
		base *= v
	}
	return base
}

// BillingInfo mirrors the billing context.
type BillingInfo struct {
	Quota           int
	ModelPrice      float64
	GroupRatio      float64
	ModelRatio      float64
	OtherRatios     map[string]float64
	OriginModelName string
	BillingSession  relaycommon.BillingSettler
}

// TaskSubmitResult captures the submission result.
type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       string
	Quota          int
}

// GetTaskProviderFunc returns the provider executor for a given platform (polling).
var GetTaskProviderFunc func(platform string) TaskProviderExec

// GetTaskSubmitProviderFunc returns the submit provider for a given platform.
// Injected by the service layer at startup.
var GetTaskSubmitProviderFunc func(platform string) TaskSubmitProvider

// Task is the task record providers operate on. Like channel.Channel it
// stays unconstrained here: naming the concrete record would make this
// interface package depend on the task domain, which imports this package.
