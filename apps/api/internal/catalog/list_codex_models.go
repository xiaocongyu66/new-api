package channel

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/constant"
)

// FetchCodexChannelModels is a stub that returns an empty list.
// The real implementation needs an HTTP client with proxy support
// which lives in internal/egress, but importing egress from catalog
// creates a test cycle with the egress test build.
func FetchCodexChannelModels(channel *Channel) ([]string, error) {
	if channel == nil || channel.Type != constant.ChannelTypeCodex {
		return nil, fmt.Errorf("channel type is not Codex")
	}
	_ = time.Now()
	return nil, nil
}
