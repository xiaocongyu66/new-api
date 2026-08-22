package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/gateway/port"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectorSatisfiesGatewayPort asserts the channel capability is usable
// through the gateway's interface. This is the wiring the split depends on: the
// gateway consumes ChannelSelector and never imports this package.
func TestSelectorSatisfiesGatewayPort(t *testing.T) {
	var selector port.ChannelSelector = NewSelector(
		func(group, modelName, requestPath string, attempt int, excluded ExclusionSet) (int, string, error) {
			return 5, group, nil
		},
	)

	selection, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
		TokenGroup: "default",
		ModelName:  "gpt-4",
	})

	require.NoError(t, err)
	assert.Equal(t, 5, selection.ChannelID)
	assert.Equal(t, "default", selection.SelectedGroup)
}

// TestSelectorForwardsRequestToLookup asserts the request fields the retry loop
// depends on reach the lookup unchanged, including the exclusion set.
func TestSelectorForwardsRequestToLookup(t *testing.T) {
	var (
		gotGroup    string
		gotModel    string
		gotPath     string
		gotAttempt  int
		gotExcluded ExclusionSet
	)

	selector := NewSelector(func(group, modelName, requestPath string, attempt int, excluded ExclusionSet) (int, string, error) {
		gotGroup, gotModel, gotPath, gotAttempt, gotExcluded = group, modelName, requestPath, attempt, excluded
		return 9, "resolved-group", nil
	})

	selection, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
		TokenGroup:        "auto",
		ModelName:         "claude-3",
		RequestPath:       "/v1/messages",
		ExcludeChannelIDs: []int{2, 3},
		Attempt:           2,
	})

	require.NoError(t, err)
	assert.Equal(t, "auto", gotGroup)
	assert.Equal(t, "claude-3", gotModel)
	assert.Equal(t, "/v1/messages", gotPath)
	assert.Equal(t, 2, gotAttempt)
	assert.True(t, gotExcluded.Excludes(2))
	assert.True(t, gotExcluded.Excludes(3))
	assert.Equal(t, "resolved-group", selection.SelectedGroup, "lookup may resolve a different group than requested")
}

// TestSelectorRejectsNonPositiveChannelID asserts a lookup miss becomes an error.
// Returning channel 0 would make the gateway dial a channel that does not exist.
func TestSelectorRejectsNonPositiveChannelID(t *testing.T) {
	for _, channelID := range []int{0, -1} {
		selector := NewSelector(func(string, string, string, int, ExclusionSet) (int, string, error) {
			return channelID, "default", nil
		})

		_, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
			TokenGroup: "default",
			ModelName:  "gpt-4",
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoChannelAvailable)
	}
}

// TestSelectorRejectsExcludedChannel asserts a lookup that hands back a
// already-failed channel is refused, so one broken upstream cannot consume every
// retry while healthy channels stay untried.
func TestSelectorRejectsExcludedChannel(t *testing.T) {
	selector := NewSelector(func(string, string, string, int, ExclusionSet) (int, string, error) {
		return 4, "default", nil
	})

	_, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
		TokenGroup:        "default",
		ModelName:         "gpt-4",
		ExcludeChannelIDs: []int{4},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoChannelAvailable)
}

// TestSelectorPropagatesLookupError asserts a lookup failure is surfaced rather
// than converted into a silent no-channel result.
func TestSelectorPropagatesLookupError(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	selector := NewSelector(func(string, string, string, int, ExclusionSet) (int, string, error) {
		return 0, "", lookupErr
	})

	_, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
		TokenGroup: "default",
		ModelName:  "gpt-4",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, lookupErr)
}

// TestSelectorRequiresModelName asserts a missing model fails before the lookup,
// since every channel query is model-scoped, and reports a caller-side sentinel
// distinct from an upstream-availability failure.
func TestSelectorRequiresModelName(t *testing.T) {
	lookupCalled := false
	selector := NewSelector(func(string, string, string, int, ExclusionSet) (int, string, error) {
		lookupCalled = true
		return 1, "default", nil
	})

	_, err := selector.SelectChannel(context.Background(), port.ChannelRequest{TokenGroup: "default"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrModelNameRequired)
	assert.NotErrorIs(t, err, ErrNoChannelAvailable, "a caller bug must not look like an upstream outage")
	assert.False(t, lookupCalled, "lookup must not run without a model")
}

// TestSelectorRejectsCancelledContext asserts an abandoned request stops before
// issuing a channel query.
func TestSelectorRejectsCancelledContext(t *testing.T) {
	lookupCalled := false
	selector := NewSelector(func(string, string, string, int, ExclusionSet) (int, string, error) {
		lookupCalled = true
		return 1, "default", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := selector.SelectChannel(ctx, port.ChannelRequest{TokenGroup: "default", ModelName: "gpt-4"})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, lookupCalled)
}

// TestSelectorRejectsUnconfiguredLookup asserts a selector without a lookup fails
// loudly instead of panicking inside the relay path.
func TestSelectorRejectsUnconfiguredLookup(t *testing.T) {
	selector := NewSelector(nil)

	_, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
		TokenGroup: "default",
		ModelName:  "gpt-4",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSelectorNotConfigured)
	assert.NotErrorIs(t, err, ErrNoChannelAvailable, "a startup defect must not look like an upstream outage")
}

// TestSelectorFallsBackToRequestedGroup asserts the requested group is reported
// when the lookup does not resolve one, so callers always get a usable value.
func TestSelectorFallsBackToRequestedGroup(t *testing.T) {
	selector := NewSelector(func(string, string, string, int, ExclusionSet) (int, string, error) {
		return 8, "", nil
	})

	selection, err := selector.SelectChannel(context.Background(), port.ChannelRequest{
		TokenGroup: "vip",
		ModelName:  "gpt-4",
	})

	require.NoError(t, err)
	assert.Equal(t, "vip", selection.SelectedGroup)
}
