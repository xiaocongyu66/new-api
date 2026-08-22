package channel

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/gateway/port"
)

// Selection failure modes are distinct sentinels so the gateway can tell an
// upstream-availability problem from a defect on its own side: a missing channel
// is a runtime condition to retry or report as unavailable, whereas an
// unconfigured selector is a startup defect and a missing model is a caller bug.
var (
	// ErrNoChannelAvailable reports that no enabled channel satisfies the request.
	ErrNoChannelAvailable = errors.New("no channel available for request")
	// ErrSelectorNotConfigured reports a selector built without a lookup.
	ErrSelectorNotConfigured = errors.New("channel selector is not configured")
	// ErrModelNameRequired reports a selection request with no model.
	ErrModelNameRequired = errors.New("model name is required to select a channel")
)

// LookupFunc resolves a channel id for a group, model, attempt, and exclusion set.
//
// It is injected so the selection rules in this package stay testable without a
// database, while the concrete lookup remains with the existing persistence code
// during the migration.
type LookupFunc func(group, modelName, requestPath string, attempt int, excluded ExclusionSet) (int, string, error)

// Selector implements the gateway's channel selection port.
type Selector struct {
	lookup LookupFunc
}

// NewSelector builds a Selector over a channel lookup.
func NewSelector(lookup LookupFunc) *Selector {
	return &Selector{lookup: lookup}
}

// SelectChannel chooses an upstream channel for a relay request.
//
// It rejects an unconfigured selector and an empty model rather than delegating,
// because both would otherwise surface as a confusing lookup miss. A lookup that
// returns a non-positive id is treated as "nothing available": returning it would
// make the gateway dial a channel that does not exist.
func (s *Selector) SelectChannel(ctx context.Context, request port.ChannelRequest) (port.ChannelSelection, error) {
	if s == nil || s.lookup == nil {
		return port.ChannelSelection{}, ErrSelectorNotConfigured
	}
	if request.ModelName == "" {
		return port.ChannelSelection{}, ErrModelNameRequired
	}
	if err := ctx.Err(); err != nil {
		return port.ChannelSelection{}, fmt.Errorf("channel selection cancelled: %w", err)
	}

	excluded := NewExclusionSet(request.ExcludeChannelIDs)

	channelID, selectedGroup, err := s.lookup(
		request.TokenGroup,
		request.ModelName,
		request.RequestPath,
		request.Attempt,
		excluded,
	)
	if err != nil {
		return port.ChannelSelection{}, err
	}
	if channelID <= 0 {
		return port.ChannelSelection{}, ErrNoChannelAvailable
	}
	if excluded.Excludes(channelID) {
		return port.ChannelSelection{}, fmt.Errorf(
			"%w: lookup returned already-excluded channel %d",
			ErrNoChannelAvailable, channelID,
		)
	}

	if selectedGroup == "" {
		selectedGroup = request.TokenGroup
	}

	return port.ChannelSelection{ChannelID: channelID, SelectedGroup: selectedGroup}, nil
}

// Selector satisfies the gateway port.
var _ port.ChannelSelector = (*Selector)(nil)
