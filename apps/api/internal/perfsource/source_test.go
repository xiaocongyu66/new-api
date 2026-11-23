package perfsource

// 回归：回退链的契约。
//
// 每一级都可能因为上游没有这个接口、凭证没配、窗口内没流量而失败，
// 所以串联逻辑必须做到两件事：只要有一级拿到数据就用它并且不再往下走；
// 全都失败时把每一级各自的原因汇总出来，否则排障时只能看到一句
// 「没数据」，分不清是接口不存在还是没配 token。

import (
	"context"
	"errors"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSource struct {
	name   string
	models map[string][]Point
	err    error
	calls  *int
}

func (s stubSource) Name() string { return s.name }

func (s stubSource) Fetch(context.Context, *channelpkg.Channel) (map[string][]Point, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.models, s.err
}

func TestCollectStopsAtFirstSourceWithData(t *testing.T) {
	channel := &channelpkg.Channel{Id: 1}
	secondCalls := 0
	thirdCalls := 0
	chain := []Source{
		stubSource{name: "newapi", err: errors.New("upstream has no such endpoint: 404")},
		stubSource{name: "sub2api", models: map[string][]Point{"m": {{Ts: 3600, Rate: 75}}}, calls: &secondCalls},
		stubSource{name: "local", models: map[string][]Point{"m": {{Ts: 3600, Rate: 100}}}, calls: &thirdCalls},
	}

	snap, err := collectFrom(t.Context(), channel, chain)
	require.NoError(t, err)
	assert.Equal(t, "sub2api", snap.Source, "the winning source must be recorded")
	assert.Equal(t, 1, secondCalls)
	assert.Zero(t, thirdCalls, "later sources must not be consulted after a hit")
	require.Len(t, snap.Models["m"], 1)
	assert.InDelta(t, 75, snap.Models["m"][0].Rate, 0.001)
}

func TestCollectReportsEverySourceReason(t *testing.T) {
	channel := &channelpkg.Channel{Id: 1}
	chain := []Source{
		stubSource{name: "newapi", err: errors.New("upstream has no such endpoint: 404")},
		stubSource{name: "sub2api", err: errors.New("sub2api access token not configured")},
		stubSource{name: "jk", models: map[string][]Point{}},
		stubSource{name: "local", err: errors.New("no local requests for this channel in window")},
	}

	snap, err := collectFrom(t.Context(), channel, chain)
	require.Error(t, err)
	assert.Nil(t, snap)
	// Each reason must survive: an empty result is not the same as "not configured".
	assert.Contains(t, err.Error(), "newapi=upstream has no such endpoint: 404")
	assert.Contains(t, err.Error(), "sub2api=sub2api access token not configured")
	assert.Contains(t, err.Error(), "jk=no data")
	assert.Contains(t, err.Error(), "local=no local requests for this channel in window")
}

func TestCollectRejectsNilChannel(t *testing.T) {
	_, err := Collect(t.Context(), nil)
	require.Error(t, err)
}
