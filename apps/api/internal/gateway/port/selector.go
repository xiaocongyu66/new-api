package port

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/model"
)

// SelectParams is the retry-scoped input for the production selection entry
// point below. It accumulates state across retries: Retry advances per
// attempt and ExcludeSet grows with failed channels.
//
// It mirrors the fields of the legacy service.RetryParam so existing call
// sites migrate without semantic change; the type alias in service keeps
// them source-compatible during the migration.
type SelectParams struct {
	Ctx          contract.Context
	TokenGroup   string
	ModelName    string
	RequestPath  string
	ExcludeSet   map[int]bool // request-level exclude set for failed channels
	Retry        *int
	resetNextTry bool
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

// SelectChannel resolves an upstream channel for one attempt of a relay
// request, returning the cached channel record so callers avoid a database
// round-trip on the hot path. It is wired to the channel capability's
// implementation in main.go before the router starts.
//
// ponytail: function seam, not the ChannelSelector interface — the interface
// returns bare IDs and would force a DB refetch of the record today. Once
// selection usecases live fully inside capabilities/channel and records flow
// through ChannelSelection, collapse this into the interface.
var SelectChannel func(p *SelectParams) (*model.Channel, string, error)
