package middleware

import (
	"io"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/internal/usage/insight"
)

// insightScanLimit is how much of the request body participates in profiling.
// The prefix covers system prompts and tool definitions (every agent client puts
// injected content first) without allocating for a multi-hundred-KB context.
const insightScanLimit = 64 * 1024

// UserInsight extracts client fingerprint and usage traits as a relay request
// enters, publishing the result on the request context for the consume log and
// the profile aggregator. Analysis failure never blocks the request.
func UserInsight() contract.Middleware {
	return func(c contract.Context) {
		if !usage.GetUserInsightSetting().Enabled {
			c.Next()
			return
		}
		if result := analyzeRequestInsight(c); result != nil {
			c.Set(string(constant.ContextKeyUserInsight), result)
		}
		c.Next()
	}
}

func analyzeRequestInsight(c contract.Context) *insight.Result {
	body := readInsightBodyPrefix(c)
	setting := usage.GetUserInsightSetting()
	result := insight.Analyze(c.Headers(), body, c.Path(), insight.Options{
		GenderInference: setting.GenderInferenceEnabled,
	})
	// Only keep a reference to the raw body when full retention is enabled, so
	// the original text does not travel further than the operator allowed.
	if result != nil && setting.SampleEnabled && setting.SampleKeepBody {
		result.SetRawBody(body)
	}
	return result
}

// readInsightBodyPrefix reads the leading bytes of the body through the
// replayable storage, so the relay pipeline still sees the full request.
func readInsightBodyPrefix(c contract.Context) []byte {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		logger.LogWarn(c.Context(), "insight: failed to access body storage: "+err.Error())
		return nil
	}
	reader, err := storage.NewReader()
	if err != nil {
		logger.LogWarn(c.Context(), "insight: failed to open body reader: "+err.Error())
		return nil
	}
	defer func() { _ = reader.Close() }()

	prefix := make([]byte, insightScanLimit)
	n, err := io.ReadFull(reader, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		logger.LogWarn(c.Context(), "insight: failed to read body prefix: "+err.Error())
	}
	return prefix[:n]
}
