package channel

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// SelectParams is the retry-scoped input for route-unit selection.
type SelectParams struct {
	Ctx           contract.Context
	TokenGroup    string
	ModelName     string
	RequestPath   string
	ExcludeRoutes map[RouteKey]bool
	Retry         *int
	resetNextTry  bool
}

func (p *SelectParams) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *SelectParams) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *SelectParams) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *SelectParams) ResetRetryNextTry() {
	p.resetNextTry = true
}

// SelectChannel resolves one route unit for a relay attempt.
var SelectChannel func(p *SelectParams) (*SelectedRoute, string, error)
