// Package port declares the capabilities the gateway consumes.
//
// Interfaces live here, on the consumer side, rather than in the capability that
// implements them. The gateway executes a relay request and needs a channel
// chosen for it; the channel capability owns how that choice is made. Declaring
// the interface here lets the gateway depend on the operation without importing
// the channel package, which is what keeps the two from forming an import cycle.
package port

import (
	"context"
)

// ChannelSelection is the outcome of choosing an upstream channel for a request.
//
// ChannelID identifies the selected channel; SelectedGroup reports the group the
// choice was actually made from, which can differ from the requested group when
// auto-group resolution walks a group list.
type ChannelSelection struct {
	ChannelID     int
	SelectedGroup string
}

// ChannelRequest describes what the gateway needs a channel for.
//
// ExcludeChannelIDs carries channels already tried and failed for this request,
// so a retry does not reselect a known-bad channel. Attempt is the zero-based
// retry counter; it used to drive descent through priority tiers, and is kept
// for call-site compatibility now that route units compete in one flat pool.
type ChannelRequest struct {
	TokenGroup        string
	ModelName         string
	RequestPath       string
	ExcludeChannelIDs []int
	Attempt           int
}

// ChannelSelector chooses an upstream channel for a relay request.
//
// Implemented by the channel capability. A selection failure must be returned as
// an error rather than a zero-valued selection, because the gateway turns it
// into an upstream-unavailable response instead of dialing channel 0.
type ChannelSelector interface {
	SelectChannel(ctx context.Context, request ChannelRequest) (ChannelSelection, error)
}
